//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real private HTTP composition asks the generic classifier first even
// for retained legacy rows. Prove that it yields to the legacy decoder without
// weakening that decoder's exact physical/account/generation/consumer fences.
func nativeLegacyDispatchAfterClassification(t *testing.T, parent context.Context, native *service.GatewayNativeConnections, custody *service.GatewayNativeCredentialCustody, gateway *service.OpenAIGatewayService,
	in service.GatewayNativeConnectionScope, route service.GatewayNativeRoute, bind func(context.Context, service.GatewayNativeRoute, string) (context.Context, error), calls *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	ctx, err := service.WithGatewayNativeConsumer(ctx, in.Scope.Consumer)
	require.NoError(t, err)
	life, err := service.NewGatewayNativeLifetime(ctx, cancel, func() {}, 1<<20)
	require.NoError(t, err)
	life.BindAccount(in.Scope.Consumer, in.Scope.Account, in.Scope.Generation)
	require.True(t, life.Admit(time.Now().Add(5*time.Second)))
	ctx = service.WithGatewayNativeLifetime(service.WithGatewayNativeCustody(ctx, custody), life)
	bound, handled, err := native.BindRoute(ctx, route, in.Scope.Account, 1, 1<<20, 16384)
	require.NoError(t, err, "legacy generations use their own stored keys")
	require.False(t, handled)
	bound, err = bind(bound, route, in.Scope.Account)
	require.NoError(t, err)
	before := calls.Load()
	wrong := route
	wrong.Generation = uuid.NewString()
	_, err = bind(ctx, wrong, in.Scope.Account)
	require.Error(t, err)
	_, err = bind(ctx, route, uuid.NewString())
	require.Error(t, err)
	foreign, err := service.WithGatewayNativeConsumer(ctx, "foreign-consumer")
	require.NoError(t, err)
	_, err = bind(foreign, route, in.Scope.Account)
	require.Error(t, err)
	wrong = route
	wrong.CreatedAt = wrong.CreatedAt.Add(time.Microsecond)
	_, _, err = native.BindRoute(ctx, wrong, in.Scope.Account, 1, 1<<20, 16384)
	require.Error(t, err)
	require.Equal(t, before, calls.Load(), "rejected tuples must not enter source HTTP")
	reasoning := ""
	if route.Profile == service.GatewayClaudeOAuthMessagesProfile {
		reasoning = `,"reasoning":{"effort":"medium"}`
	}
	body := []byte(fmt.Sprintf(`{"model":%q,"input":"Review source","stream":true,"store":false,"max_output_tokens":8192,"tools":[{"type":"function","name":"read_source","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}}]%s}`, route.Model, reasoning))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	_, entered, err := gateway.ForwardGatewayRoute(bound, c, route, body)
	require.NoError(t, err)
	require.True(t, entered)
	require.Equal(t, before+1, calls.Load())
	require.True(t, life.Snapshot().Completed)
	require.NotEmpty(t, nativeConnectionCompletedOutput(t, recorder.Body.String()))
}

func nativeLegacyRecoveryAssertions(t *testing.T, ctx context.Context, adapter service.GatewayNativeConnectionAdapter, in service.GatewayNativeConnectionScope, expected *service.GatewayNativeRoute, requests *atomic.Int32) {
	t.Helper()
	before := requests.Load()
	require.Empty(t, adapter.MethodIDs, "removed mounts advertise no new methods")
	require.Contains(t, adapter.RecoveryMethodIDs, in.Connection.MethodID)
	read, err := adapter.Read(ctx, in)
	require.NoError(t, err)
	require.Equal(t, &in.Connection, read.Connection)
	require.Empty(t, read.AuthorizeURL)
	require.Nil(t, read.DeviceAuthorization)
	wire, err := json.Marshal(read)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(wire, &fields))
	require.NotContains(t, fields, "method", "retained V2 readback must not reintroduce a legacy top-level method")
	descriptor, err := adapter.Describe(ctx, in)
	require.NoError(t, err)
	if expected == nil {
		require.Nil(t, descriptor.Native)
		require.Equal(t, "pending", descriptor.Qualification)
	} else {
		require.Equal(t, "completed", read.State)
		require.NotNil(t, descriptor.Native)
		require.Equal(t, *expected, descriptor.Native.GatewayNativeRoute)
		custody, err := adapter.Custody(ctx, in, *descriptor.Native)
		require.NoError(t, err)
		require.True(t, custody.Proof.Authenticated)
		require.Equal(t, []string{"oauth"}, custody.CredentialKinds)
	}
	_, err = adapter.Begin(ctx, in, map[string]string{"refresh_token": "untrusted-new-secret"})
	require.Error(t, err)
	_, err = adapter.Submit(ctx, in, map[string]string{"completion": "untrusted-new-code"})
	require.Error(t, err)
	_, err = adapter.Poll(ctx, in)
	require.Error(t, err)
	_, err = adapter.Refresh(ctx, in)
	require.Error(t, err)
	foreign := in
	foreign.Scope.Owner = "foreign-owner"
	_, err = adapter.Read(ctx, foreign)
	require.Error(t, err)
	foreign = in
	foreign.Connection.ConfigurationGeneration = "retargeted-config"
	_, err = adapter.Read(ctx, foreign)
	require.Error(t, err)
	_, err = adapter.Cleanup(ctx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "recovery-cleanup", CleanupToken: "recovery-cleanup-token"})
	require.NoError(t, err)
	if descriptor.Native != nil {
		_, err = adapter.Custody(ctx, in, *descriptor.Native)
		require.Error(t, err)
	}
	require.Equal(t, before, requests.Load(), "retained read/descriptor/erase and denied effects make zero provider HTTP calls")
}

// The legacy methods are created through actual TLS OAuth flows. The recovery
// adapters then receive only persisted journal/repository/custody ports, with
// no current mount, OAuth client, provider transport or entry authorization.
func TestGatewayNativeConnectionPostgresLegacyRecoveryWithoutMount(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_LEGACY_RECOVERY_TEST_DSN", "native_legacy_recovery_test_")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	codexRepo, ok := accounts.(service.GatewayNativeCodexRecoveryRepository)
	require.True(t, ok)
	stage, ok := accounts.(service.GatewayNativeOAuthRepository)
	require.True(t, ok)
	claudeRepo, err := NewGatewayNativeClaudeRepository(db)
	require.NoError(t, err)
	grokRepo, err := NewGatewayNativeGrokRepository(db)
	require.NoError(t, err)
	materialKey := bytes.Repeat([]byte{0x7a}, 32)
	custody, err := service.NewGatewayNativeCredentialCustody("legacy-recovery-fixture", map[string][]byte{"legacy-recovery-fixture": materialKey})
	require.NoError(t, err)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	var codex atomic.Pointer[nativeCodexImportFixture]
	var subscription atomic.Pointer[nativeSubscriptionWire]
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if fixture := subscription.Load(); fixture != nil {
			fixture.ServeHTTP(w, request)
			return
		}
		fixture := codex.Load()
		if fixture == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if request.Host == "auth.openai.com" && request.URL.Path == "/api/accounts/deviceauth/usercode" {
			var body map[string]string
			require.NoError(fixture.t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(fixture.t, openai.ClientID, body["client_id"])
			require.NoError(fixture.t, json.NewEncoder(w).Encode(map[string]any{"device_auth_id": "fixture-device-id", "user_code": "CODE-1234", "interval": 3, "expires_in": 300}))
			return
		}
		if fixture.method == "browser" && request.Host == "auth.openai.com" && request.URL.Path == "/oauth/token" {
			require.NoError(fixture.t, request.ParseForm())
			assert.Equal(fixture.t, "authorization_code", request.Form.Get("grant_type"))
			assert.Equal(fixture.t, openai.ClientID, request.Form.Get("client_id"))
			assert.Equal(fixture.t, service.GatewayOAuthConnectRedirect, request.Form.Get("redirect_uri"))
			fixture.tokens.Add(1)
			require.NoError(fixture.t, json.NewEncoder(w).Encode(map[string]any{"access_token": fixture.bearer(), "refresh_token": fixture.refresh, "id_token": fixture.signedID(), "token_type": "Bearer", "expires_in": 3600}))
			return
		}
		fixture.serve(w, request)
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		switch address {
		case "auth.openai.com:443", "chatgpt.com:443", "platform.claude.com:443", "auth.x.ai:443", "api.anthropic.com:443", "cli-chat-proxy.grok.com:443":
		default:
			return nil, service.ErrGatewayNativeIdentity
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionTLSUpstream{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	verifier := service.NewGatewayNativeOAuthVerifier(transport)
	enrollment, err := service.NewGatewayNativeOAuthEnrollment(verifier, custody, stage, materialKey)
	require.NoError(t, err)
	connect, err := service.NewGatewayNativeOAuthConnect(materialKey, transport, journal, enrollment, codexRepo)
	require.NoError(t, err)
	dispatch, err := service.NewGatewayNativeOAuthDispatch(codexRepo, custody, verifier, transport)
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "legacy-recovery-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "legacy-recovery-owner")
	require.NoError(t, err)
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if !service.GatewayConnectionScopeAuthorized(ctx, scope) || scope.Owner != "legacy-recovery-owner" {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	require.NoError(t, connect.SetEntryAuthorizer(owner))
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	native, err := service.NewGatewayNativeConnections(service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: materialKey,
		Connect: owner, Refresh: owner, Cleanup: func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
			return service.GatewayNativeOAuthCleanupAuthority{}, service.ErrGatewayNativeIdentity
		}})
	require.NoError(t, err)
	for _, name := range []string{"codex/browser", "codex/device_code", "codex/refresh_token", "codex/auth_json", "claude/browser", "grok/browser"} {
		t.Run(name, func(t *testing.T) {
			provider, method, _ := strings.Cut(name, "/")
			profile := service.GatewayCodexOAuthResponsesProfile
			if provider == "claude" {
				profile = service.GatewayClaudeOAuthMessagesProfile
			}
			if provider == "grok" {
				profile = service.GatewayGrokOAuthResponsesProfile
			}
			selection := service.GatewayConnectionSelection{ProfileID: profile, MethodID: method, MethodRevision: 1, ConfigurationGeneration: "old-qualified-config"}
			in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "legacy-recovery-consumer", Owner: "legacy-recovery-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "legacy-" + uuid.NewString(), Connection: selection}
			selectedCtx, err := service.WithGatewayConnectionSelection(ownerCtx, selection)
			require.NoError(t, err)
			var route *service.GatewayNativeRoute
			cleanup := func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
				return service.GatewayNativeOAuthCleanupAuthority{Owner: in.Scope.Owner, LeaseExpiresAt: time.Now().Add(time.Minute), Native: route}, nil
			}
			var recovery service.GatewayNativeConnectionAdapter
			if provider == "codex" {
				subscription.Store(nil)
				fixture := &nativeCodexImportFixture{t: t, method: method, clientID: openai.ClientID, access: "fixture-old-access-" + uuid.NewString(), refresh: "fixture-old-refresh-" + uuid.NewString(), account: uuid.NewString(), key: rsaKey}
				codex.Store(fixture)
				var completed service.GatewayNativeOAuthConnectResult
				if method == "refresh_token" || method == "auth_json" {
					input := fixture.refresh
					if method == "auth_json" {
						raw, e := json.Marshal(map[string]any{"tokens": map[string]string{"refresh_token": fixture.refresh}})
						require.NoError(t, e)
						input = string(raw)
					}
					completed, err = connect.ImportConnect(selectedCtx, in.Scope, in.Operation, method, input)
				} else {
					completed, err = connect.BeginConnectMethod(selectedCtx, in.Scope, in.Operation, method)
					require.NoError(t, err)
					if method == "browser" {
						authorize, e := url.Parse(completed.AuthorizeURL)
						require.NoError(t, e)
						completed, err = connect.CompleteOwnerCallback(selectedCtx, in.Scope, in.Operation, authorize.Query().Get("state"), "fixture-code")
					}
				}
				require.NoError(t, err)
				recovery, err = service.NewGatewayNativeCodexRecoveryConnectionAdapter(journal, codexRepo, custody, cleanup)
				require.NoError(t, err)
				before := requests.Load()
				pending, err := recovery.Describe(ownerCtx, in)
				require.NoError(t, err)
				require.Nil(t, pending.Native, "retained Descriptor must not qualify a pending source")
				require.Equal(t, before, requests.Load())
				if method != "device_code" {
					require.Equal(t, "completed", completed.State)
					qualified, e := dispatch.Descriptor(selectedCtx, in.Scope, in.Operation)
					require.NoError(t, e)
					require.NotNil(t, qualified.Native)
					route = qualified.Native
				}
			} else {
				fixture := &nativeSubscriptionWire{t: t, provider: provider, method: "browser", input: "unused-browser-material-" + uuid.NewString(), subject: uuid.NewString(), team: uuid.NewString(), key: ecKey}
				subscription.Store(fixture)
				if provider == "claude" {
					current, err := service.NewGatewayNativeClaudeOAuth(claudeRepo, custody, transport)
					require.NoError(t, err)
					require.NoError(t, current.SetEntryAuthorizers(owner, owner))
					begin, err := current.BeginConnectMethod(selectedCtx, in.Scope, in.Operation, "browser")
					require.NoError(t, err)
					authorize, err := url.Parse(begin.AuthorizeURL)
					require.NoError(t, err)
					fixture.state = authorize.Query().Get("state")
					fixture.completionCode, fixture.codeChallenge = "fresh-code-"+uuid.NewString(), authorize.Query().Get("code_challenge")
					completed, err := current.CompleteOwnerCallback(selectedCtx, in.Scope, in.Operation, fixture.state, fixture.completionCode)
					require.NoError(t, err)
					require.Equal(t, "completed", completed.State)
					descriptor, err := current.Descriptor(selectedCtx, in.Scope, in.Operation)
					require.NoError(t, err)
					route = descriptor.Native
					nativeLegacyDispatchAfterClassification(t, ctx, native, custody, gateway, in, *route, func(ctx context.Context, route service.GatewayNativeRoute, account string) (context.Context, error) {
						return current.BindApproved(ctx, route, account, 1, 1<<20, 16384)
					}, &fixture.calls)
					recovery, err = service.NewGatewayNativeClaudeRecoveryConnectionAdapter(claudeRepo, custody, cleanup)
					require.NoError(t, err)
					// Actual facade reconnect must stage a fresh native generation
					// before switching its canonical binding and erasing the old birth.
					// Both grants use fresh source codes/PKCE over the real TLS peer.
					active, err := service.NewGatewayNativeClaudeConnectionAdapter(current, cleanup)
					require.NoError(t, err)
					candidate := in
					candidate.Scope.Generation, candidate.Operation = uuid.NewString(), "reconnect-"+uuid.NewString()
					candidateBegin, err := active.Begin(ownerCtx, candidate, map[string]string{})
					require.NoError(t, err)
					require.Empty(t, candidateBegin.Method)
					authorize, err = url.Parse(candidateBegin.AuthorizeURL)
					require.NoError(t, err)
					fixture.state = authorize.Query().Get("state")
					fixture.completionCode, fixture.codeChallenge = "fresh-code-"+uuid.NewString(), authorize.Query().Get("code_challenge")
					connected, err := active.Submit(ownerCtx, candidate, map[string]string{"completion": fixture.completionCode})
					require.NoError(t, err)
					require.Equal(t, "completed", connected.State)
					require.EqualValues(t, 2, fixture.grant.Load())
					_, err = active.Submit(ownerCtx, candidate, map[string]string{"completion": fixture.completionCode})
					require.NoError(t, err)
					require.EqualValues(t, 2, fixture.grant.Load(), "committed reconnect readback cannot replay its one-use source grant")
					candidateDescriptor, err := current.Descriptor(selectedCtx, candidate.Scope, candidate.Operation)
					require.NoError(t, err)
					require.NotNil(t, candidateDescriptor.Native)
					require.NotEqual(t, route.AccountID, candidateDescriptor.Native.AccountID)
					require.False(t, route.CreatedAt.Equal(candidateDescriptor.Native.CreatedAt))
					_, err = current.RetainedCustody(selectedCtx, in.Scope, in.Operation, *route)
					require.NoError(t, err, "old custody stays retained until the facade commits replacement")
					// A new logical account still cannot take the permanently owned
					// provider principal, even with a successful fresh source exchange.
					foreign := candidate
					foreign.Scope.Account, foreign.Scope.Generation, foreign.Operation = uuid.NewString(), uuid.NewString(), "foreign-"+uuid.NewString()
					foreignBegin, err := active.Begin(ownerCtx, foreign, map[string]string{})
					require.NoError(t, err)
					authorize, err = url.Parse(foreignBegin.AuthorizeURL)
					require.NoError(t, err)
					fixture.state = authorize.Query().Get("state")
					fixture.completionCode, fixture.codeChallenge = "fresh-code-"+uuid.NewString(), authorize.Query().Get("code_challenge")
					_, err = active.Submit(ownerCtx, foreign, map[string]string{"completion": fixture.completionCode})
					require.Error(t, err)
					unknown, err := active.Read(ownerCtx, foreign)
					require.NoError(t, err)
					require.Equal(t, "unknown", unknown.State)
					require.Nil(t, unknown.Credential)
					require.EqualValues(t, 3, fixture.grant.Load())
					// A separately authenticated owner also cannot take the original
					// logical account's provider principal. Its source grant is valid;
					// the permanent native SQL claim rejects the ownership change.
					foreign.Scope = candidate.Scope
					foreign.Scope.Owner, foreign.Scope.Generation, foreign.Operation = "another-owner", uuid.NewString(), "owner-"+uuid.NewString()
					foreignCtx, err := service.WithGatewayNativeOAuthOwner(selectedCtx, foreign.Scope.Owner)
					require.NoError(t, err)
					foreignCurrent, err := service.NewGatewayNativeClaudeOAuth(claudeRepo, custody, transport)
					require.NoError(t, err)
					authenticatedOwner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
						if !service.GatewayConnectionScopeAuthorized(ctx, scope) {
							return service.ErrGatewayNativeIdentity
						}
						return nil
					}
					require.NoError(t, foreignCurrent.SetEntryAuthorizers(authenticatedOwner, authenticatedOwner))
					foreignStart, err := foreignCurrent.BeginConnectMethod(foreignCtx, foreign.Scope, foreign.Operation, "browser")
					require.NoError(t, err)
					authorize, err = url.Parse(foreignStart.AuthorizeURL)
					require.NoError(t, err)
					fixture.state = authorize.Query().Get("state")
					fixture.completionCode, fixture.codeChallenge = "fresh-code-"+uuid.NewString(), authorize.Query().Get("code_challenge")
					_, err = foreignCurrent.CompleteOwnerCallback(foreignCtx, foreign.Scope, foreign.Operation, fixture.state, fixture.completionCode)
					require.Error(t, err)
					require.EqualValues(t, 4, fixture.grant.Load())
					nativeLegacyRecoveryAssertions(t, ownerCtx, recovery, in, route, &requests)
					_, err = current.RetainedCustody(selectedCtx, candidate.Scope, candidate.Operation, *candidateDescriptor.Native)
					require.NoError(t, err, "erasing the old birth cannot erase or retarget fresh custody")
					route = candidateDescriptor.Native
					nativeLegacyRecoveryAssertions(t, ownerCtx, recovery, candidate, route, &requests)
					return
				} else {
					fixture.clientID = "old-qualified-grok-client"
					current, err := service.NewGatewayNativeGrokOAuth(grokRepo, custody, transport, service.GatewayNativeGrokOAuthQualification{ClientID: fixture.clientID, RedirectURI: xai.DefaultRedirectURI, DeploymentQualified: true})
					require.NoError(t, err)
					require.NoError(t, current.SetEntryAuthorizers(owner, owner))
					begin, err := current.BeginConnectMethod(selectedCtx, in.Scope, in.Operation, "browser")
					require.NoError(t, err)
					authorize, err := url.Parse(begin.AuthorizeURL)
					require.NoError(t, err)
					fixture.state, fixture.nonce = authorize.Query().Get("state"), authorize.Query().Get("nonce")
					completed, err := current.CompleteOwnerCallback(selectedCtx, in.Scope, in.Operation, fixture.state, "fixture-code")
					require.NoError(t, err)
					require.Equal(t, "completed", completed.State)
					descriptor, err := current.Descriptor(selectedCtx, in.Scope, in.Operation)
					require.NoError(t, err)
					route = descriptor.Native
					nativeLegacyDispatchAfterClassification(t, ctx, native, custody, gateway, in, *route, func(ctx context.Context, route service.GatewayNativeRoute, account string) (context.Context, error) {
						scope, operation, err := current.ResolveApproved(ctx, route, account)
						if err != nil {
							return nil, err
						}
						return service.WithGatewayNativeGrokDispatch(ctx, current, scope, operation, 1<<20, 16384)
					}, &fixture.calls)
					// No current qualification/client is passed to retained recovery.
					recovery, err = service.NewGatewayNativeGrokRecoveryConnectionAdapter(grokRepo, custody, cleanup)
					require.NoError(t, err)
				}
				require.NotNil(t, route)
			}
			nativeLegacyRecoveryAssertions(t, ownerCtx, recovery, in, route, &requests)
		})
	}
}
