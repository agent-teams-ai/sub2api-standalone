//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeCodexImportFields(t *testing.T, f *nativeCodexImportFixture) map[string]string {
	t.Helper()
	switch f.method {
	case "mobile_refresh_token":
		return map[string]string{"refresh_token": f.refresh}
	case "codex_pat":
		return map[string]string{"access_token": f.access}
	}
	var document any
	switch f.variant {
	case "raw":
		return map[string]string{"content": f.access}
	case "json_string":
		document = f.access
	case "expired":
		document = map[string]any{"access_token": f.access, "expires_at": time.Now().Add(-time.Hour).UnixMilli()}
	case "opaque_expiry":
		document = map[string]any{"tokens": map[string]any{"accessToken": f.access, "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}}
	case "camel":
		document = map[string]any{"accessToken": f.access}
	case "snake":
		document = map[string]any{"access_token": f.access}
	case "auth_json":
		document = map[string]any{"tokens": map[string]any{"access_token": f.access}}
	case "single_array":
		document = []any{map[string]any{"tokens": map[string]any{"access_token": f.access}}}
	case "auth_json_refresh":
		document = map[string]any{"tokens": map[string]any{"access_token": f.access, "refresh_token": f.refresh, "id_token": "untrusted-input-id-token"}, "email": "untrusted@example.test"}
	default:
		der, err := x509.MarshalPKCS8PrivateKey(f.agent)
		require.NoError(t, err)
		document = map[string]any{"agentIdentity": map[string]any{"agentRuntimeId": "runtime-" + f.account,
			"agentPrivateKey": base64.StdEncoding.EncodeToString(der), "accountId": f.account, "chatgptUserId": "user-" + f.account,
			"taskId": "untrusted-imported-task", "email": "untrusted-agent@example.test", "planType": "enterprise",
			"chatgpt_account_is_fedramp": false, "chatgptAccountIsFedramp": true}}
	}
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	return map[string]string{"content": string(raw)}
}

// The provider side is controlled TLS. Everything between the operation,
// migrated PG custody, model discovery, signed native HTTP and tool continuation
// is the actual implementation; an unavailable local prerequisite is a skip.
func TestGatewayNativeConnectionPostgresCodexVariants(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_CODEX_CONNECTION_TEST_DSN", "native_codex_connection_test_")
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stop()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	legacy, ok := accounts.(service.GatewayNativeOAuthDispatchRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("codex-wire", map[string][]byte{"codex-wire": bytes.Repeat([]byte{0x64}, 32)})
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var current atomic.Pointer[nativeCodexImportFixture]
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().serve(w, r) }))
	defer server.Close()
	baseTransport, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	transport := baseTransport.Clone()
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DisableKeepAlives = true
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "auth.openai.com:443" && address != "chatgpt.com:443" {
			return nil, service.ErrGatewayNativeIdentity
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionTLSUpstream{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	dispatch, err := service.NewGatewayNativeCodexImportDispatcher(gateway)
	require.NoError(t, err)
	codex, err := service.NewGatewayNativeOAuthDispatch(legacy, custody, service.NewGatewayNativeOAuthVerifier(transport), transport)
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "codex-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "codex-owner")
	require.NoError(t, err)
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if ctx.Err() != nil || !service.GatewayConnectionScopeAuthorized(ctx, scope) || scope.Owner != "codex-owner" {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	cases := []struct{ method, variant string }{
		{"codex_session", "raw"}, {"codex_session", "camel"}, {"codex_session", "snake"}, {"codex_session", "auth_json"},
		{"codex_session", "json_string"}, {"codex_session", "expired"}, {"codex_session", "opaque_expiry"},
		{"codex_session", "single_array"}, {"codex_session", "auth_json_refresh"}, {"mobile_refresh_token", "mobile"},
		{"codex_pat", "pat"}, {"agent_identity", "agent"}, {"codex_session", "agent_session"},
	}
	for _, row := range cases {
		t.Run(row.method+"/"+row.variant, func(t *testing.T) {
			f := &nativeCodexImportFixture{t: t, method: row.method, variant: row.variant, key: key,
				access: "opaque-codex-" + uuid.NewString(), refresh: "refresh-codex-" + uuid.NewString(), account: uuid.NewString()}
			if row.variant == "auth_json_refresh" {
				f.clientID = openai.ClientID
			} else if row.method == "mobile_refresh_token" {
				f.clientID = "app_LlGpXReQgckcGGUo2JrYvtJK"
			} else if row.method == "codex_pat" {
				f.access = "at-" + uuid.NewString()
			} else if row.variant == "agent" || row.variant == "agent_session" {
				_, f.agent, err = ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
			}
			current.Store(f)
			methods, err := service.NewGatewayNativeCodexImportMethods("codex-"+uuid.NewString(), service.GatewayConnectionMount{BaseURL: service.GatewayCodexOAuthBaseURL, Model: service.GatewayCodexOAuthModel, QualificationRef: "codex-fixture"}, transport)
			require.NoError(t, err)
			var selected service.GatewayNativeConnectionMethodAdapter
			for _, method := range methods {
				if method.MethodID == row.method {
					selected = method
				}
			}
			require.NotEmpty(t, selected.MethodID)
			var route *service.GatewayNativeRoute
			cleanup := func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
				return service.GatewayNativeOAuthCleanupAuthority{Owner: "codex-owner", LeaseExpiresAt: time.Now().Add(time.Minute), Native: route}, nil
			}
			deps := service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: bytes.Repeat([]byte{0x65}, 32), Methods: methods,
				Connect: owner, Refresh: owner, Cleanup: cleanup, Dispatchers: map[string]service.GatewayNativeConnectionDispatchAdapter{"codex_responses": dispatch}, Gateway: gateway, Codex: codex}
			native, err := service.NewGatewayNativeConnections(deps)
			require.NoError(t, err)
			in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "codex-consumer", Owner: "codex-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "codex-" + uuid.NewString(), Connection: selected.Selection()}
			selectedCtx, err := service.WithGatewayConnectionSelection(ownerCtx, in.Connection)
			require.NoError(t, err)
			fields := nativeCodexImportFields(t, f)
			if len(f.agent) != 0 {
				var aliases map[string]map[string]any
				require.NoError(t, json.Unmarshal([]byte(fields["content"]), &aliases))
				encoded, valid := aliases["agentIdentity"]["agentPrivateKey"].(string)
				require.True(t, valid)
				aliases["agentIdentity"]["agentPrivateKey"] = encoded[:32] + "\r\n" + encoded[32:]
				alternate, err := json.Marshal(aliases)
				require.NoError(t, err)
				original, err := selected.Prepare(fields)
				require.NoError(t, err)
				alias, err := selected.Prepare(map[string]string{"content": string(alternate)})
				require.NoError(t, err)
				require.Equal(t, original.EntryClaim, alias.EntryClaim, "equivalent private-key spellings cannot bypass unknown-entry protection")
				clear(original.Material)
				clear(original.EntryClaim)
				clear(alias.Material)
				clear(alias.EntryClaim)
			}
			created, err := native.Begin(ownerCtx, in, fields)
			if row.variant == "expired" {
				require.Error(t, err)
				require.Zero(t, f.checks.Load())
				require.Zero(t, f.inference.Load())
				var rows int
				require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM gateway_oauth_connect_intents WHERE operation_ref=$1`, in.Operation).Scan(&rows))
				require.Zero(t, rows, "expired opaque AT is rejected before durable effect entry")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "completed", created.State)
			require.NotNil(t, created.Credential)
			if len(f.agent) != 0 {
				require.Equal(t, service.GatewayCredentialAttestation{"native", "delegated_identity", "native"}, *created.Credential)
				require.EqualValues(t, 1, f.tasks.Load(), "imported task is replaced by one native registration")
			} else if row.method == "codex_pat" {
				require.Equal(t, service.GatewayCredentialAttestation{"native", "subscription_session", "none"}, *created.Credential)
			} else if f.clientID != "" {
				require.Equal(t, service.GatewayCredentialAttestation{"oauth", "subscription_session", "native"}, *created.Credential)
			} else {
				require.Equal(t, service.GatewayCredentialAttestation{"oauth", "subscription_session", "none"}, *created.Credential)
			}
			readback, err := native.Begin(ownerCtx, in, fields)
			require.NoError(t, err)
			require.Equal(t, created, readback, "same operation is durable readback")
			published, err := native.Describe(ownerCtx, in)
			require.NoError(t, err)
			require.NotNil(t, published.Native)
			route = &published.Native.GatewayNativeRoute
			infoReader, err := service.NewGatewayAccountInfoReader(NewGatewayAccountInfoRepository(db), func(context.Context, service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
				return *route, nil
			}, native)
			require.NoError(t, err)
			infoInput := service.GatewayAccountLimitsInput{OwnerRef: in.Scope.Owner, AccountRef: in.Scope.Account, ProfileID: in.Connection.ProfileID, ExpectedAuthorizationEpoch: 1}
			checksBefore, tokensBefore, tasksBefore := f.checks.Load(), f.tokens.Load(), f.tasks.Load()
			info := infoReader.Read(ownerCtx, infoInput)
			require.Equal(t, "available", info.Info.Status)
			require.Equal(t, in.Scope.Generation, info.PhysicalGeneration)
			require.NotEmpty(t, info.PrincipalVersion)
			require.NotNil(t, info.Info.Fields)
			if f.clientID != "" {
				require.Equal(t, "known", info.Info.Fields.CredentialExpiresAt.Status, "committed provider expiry reaches the cached reader")
			}
			require.Equal(t, "unsupported", service.ProjectGatewayAccountInfoV1(info).Info.Fields.PrivacyMode.Status)
			infoInput.OwnerRef = "foreign-owner"
			require.Equal(t, "unavailable", infoReader.Read(ownerCtx, infoInput).Info.Status)
			require.Equal(t, checksBefore, f.checks.Load(), "metadata reads do not call account check")
			require.Equal(t, tokensBefore, f.tokens.Load(), "metadata reads do not refresh credentials")
			require.Equal(t, tasksBefore, f.tasks.Load(), "metadata reads do not register tasks")
			reader, err := service.NewGatewayAccountModelsReaderV2(native, nil, func(context.Context, service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
				return *route, nil
			})
			require.NoError(t, err)
			modelsInput := service.GatewayAccountModelsInput{GatewayAccountLimitsInput: service.GatewayAccountLimitsInput{OwnerRef: in.Scope.Owner, AccountRef: in.Scope.Account, ProfileID: in.Connection.ProfileID, ExpectedAuthorizationEpoch: 1}}
			limitsReader, err := service.NewGatewayAccountLimitsReaderV3(native, nil, nil, func(context.Context, service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
				return *route, nil
			})
			require.NoError(t, err)
			require.Equal(t, "unknown", limitsReader.Read(ownerCtx, modelsInput.GatewayAccountLimitsInput).Status)
			candidates, err := native.ListQuotaAccounts(ownerCtx)
			require.NoError(t, err)
			require.Len(t, candidates, 1)
			require.NoError(t, native.CollectQuota(ownerCtx, candidates[0], service.NewGatewayProviderQuotaCollector(transport)))
			collected := limitsReader.Read(ownerCtx, modelsInput.GatewayAccountLimitsInput)
			require.Equal(t, "snapshot", collected.Status)
			require.Len(t, collected.Windows, 1)
			require.Equal(t, 75.0, *collected.Windows[0].UsedPercent)
			require.EqualValues(t, 1, f.quotas.Load(), "cached read does not repeat the quota-only GET")
			models := reader.Read(ownerCtx, modelsInput)
			require.Equal(t, "snapshot", models.Status)
			require.Len(t, *models.Models, 1)
			require.Equal(t, "future-native-model", (*models.Models)[0].ModelID)
			encoded, err := json.Marshal(models)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "PRIVATE-NATIVE-INSTRUCTIONS")
			if row.variant == "opaque_expiry" {
				resolution, err := physical.ResolveGatewayNativeConnectionRefresh(selectedCtx, in.Scope, in.Operation)
				require.NoError(t, err)
				raw, err := service.AuthenticateGatewayNativeConnectionPhysical(selectedCtx, custody, resolution.Physical)
				require.NoError(t, err)
				require.True(t, strings.Contains(string(raw), `"expires_at":`), "explicit opaque-token expiry survives sealing")
				clear(raw)
			}
			request := map[string]any{"model": "future-native-model", "input": "Review file.go", "store": false, "stream": true, "max_output_tokens": 512, "tools": []any{map[string]any{"type": "function", "name": "read_source", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}}}}}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			f.phase.Store(1)
			var firstUsage []service.GatewayNativeUsage
			usageCtx := service.WithGatewayNativeUsageObserver(ownerCtx, func(usage service.GatewayNativeUsage) { firstUsage = append(firstUsage, usage) })
			first, entered, err := nativeConnectionForwardFixture(t, usageCtx, custody, native, gateway, in, *route, body)
			require.NoError(t, err)
			require.True(t, entered)
			require.Len(t, firstUsage, 1)
			require.Equal(t, &service.GatewayNativeUsageContext{ProviderID: "openai", ProductID: "subscription", ProfileID: service.GatewayCodexOAuthResponsesProfile,
				ModelID: "future-native-model", Billing: service.GatewayNativeUsageBilling{Status: "unknown"}}, firstUsage[0].NativeContext)
			require.NotEqual(t, route.Model, firstUsage[0].NativeContext.ModelID, "usage records the admitted live model, not the qualified anchor")
			firstUsageJSON, err := json.Marshal(firstUsage[0])
			require.NoError(t, err)
			require.NotContains(t, string(firstUsageJSON), f.access)
			passive := limitsReader.Read(ownerCtx, modelsInput.GatewayAccountLimitsInput)
			require.Equal(t, "snapshot", passive.Status)
			require.Len(t, passive.Windows, 1)
			require.Equal(t, 25.0, *passive.Windows[0].UsedPercent, "actual response headers replace the earlier quota source")
			require.EqualValues(t, 1, f.quotas.Load())
			previousPhysical, releaseQuota, err := physical.LockGatewayNativeConnection(selectedCtx, in.Scope, in.Operation, published.Native)
			require.NoError(t, err)
			releaseQuota()
			output := nativeConnectionCompletedOutput(t, first.Body.String())
			toolResult := json.RawMessage(`{"type":"function_call_output","call_id":"call_review","output":"synthetic file contents"}`)
			request["input"] = append(output, toolResult)
			body, err = json.Marshal(request)
			require.NoError(t, err)
			f.phase.Store(2)
			second, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.NoError(t, err)
			require.True(t, entered)
			require.NotEmpty(t, nativeConnectionCompletedOutput(t, second.Body.String()))
			require.EqualValues(t, 2, f.inference.Load())
			if len(f.agent) != 0 {
				f.phase.Store(3)
				failed, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
				require.ErrorIs(t, err, service.ErrGatewayNativeEffectUnknown)
				require.True(t, entered)
				require.NotContains(t, failed.Body.String(), "private provider task rejection")
				require.EqualValues(t, 1, f.tasks.Load(), "rejected inference never registers or retries inline")
				resolution, err := physical.ResolveGatewayNativeConnectionRefresh(selectedCtx, in.Scope, in.Operation)
				require.NoError(t, err)
				require.Equal(t, "prepared", resolution.State)
				// Let the existing 15-second refresh claim expire normally.
				timer := time.NewTimer(16 * time.Second)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					t.Fatal(ctx.Err())
				}
			}
			maintained, err := native.Maintain(ownerCtx, in)
			require.NoError(t, err)
			if created.Credential.RefreshPolicy == "native" {
				require.Equal(t, "completed", maintained.State)
				resolution, err := physical.ResolveGatewayNativeConnectionRefresh(selectedCtx, in.Scope, in.Operation)
				require.NoError(t, err)
				require.EqualValues(t, 2, resolution.Physical.CredentialVersion)
				require.Equal(t, "unknown", limitsReader.Read(ownerCtx, modelsInput.GatewayAccountLimitsInput).Status, "old quota principal cannot survive a credential revision")
				writer, ok := physical.(service.GatewayNativeConnectionQuotaRepository)
				require.True(t, ok)
				observed, err := time.Parse(time.RFC3339Nano, *passive.Windows[0].ObservedAt)
				require.NoError(t, err)
				require.Error(t, writer.StoreGatewayNativeConnectionQuota(selectedCtx, previousPhysical, service.GatewayProviderQuotaObservation{Limits: service.GatewayAccountLimitsSnapshotV2{Scope: passive.Scope, Windows: passive.Windows}, ObservedAt: observed}, 0), "a late response from the old credential cannot publish")
				modelsInput.ForceRefresh = true
				require.Equal(t, "snapshot", reader.Read(ownerCtx, modelsInput).Status)
				f.phase.Store(2)
				_, entered, err = nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
				require.NoError(t, err)
				require.True(t, entered)
				currentQuota := limitsReader.Read(ownerCtx, modelsInput.GatewayAccountLimitsInput)
				require.Equal(t, "snapshot", currentQuota.Status)
				require.NotEqual(t, passive.PrincipalVersion, currentQuota.PrincipalVersion)
			} else {
				require.Equal(t, "idle", maintained.State, "AT-only and PAT never invent refresh")
			}
			var durable string
			require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_build_object('account',(SELECT to_jsonb(a) FROM accounts a WHERE id=$1),'intent',(SELECT to_jsonb(i) FROM gateway_oauth_connect_intents i WHERE operation_ref=$2))::text`, route.AccountID, in.Operation).Scan(&durable))
			for _, secret := range []string{f.access, f.refresh, "untrusted-input-id-token", "untrusted-agent@example.test", "untrusted-imported-task"} {
				require.NotContains(t, durable, secret)
			}
			if len(f.agent) != 0 {
				der, err := x509.MarshalPKCS8PrivateKey(f.agent)
				require.NoError(t, err)
				require.NotContains(t, durable, base64.StdEncoding.EncodeToString(der))
			}
			_, err = native.Cleanup(ownerCtx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "codex-cleanup", CleanupToken: "codex-cleanup-token"})
			require.NoError(t, err)
			before := f.inference.Load()
			_, entered, err = nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.Error(t, err)
			require.False(t, entered)
			require.Equal(t, before, f.inference.Load())
			afterEraseUsage, err := json.Marshal(firstUsage[0])
			require.NoError(t, err)
			require.JSONEq(t, string(firstUsageJSON), string(afterEraseUsage), "captured usage survives physical retirement without a fresh lookup")
		})
	}
}
