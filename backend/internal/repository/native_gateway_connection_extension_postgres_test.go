//go:build (integration || gatewaycustody) && gatewayextension

package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The extension's own passive quota projection must not turn a successful
// source response into an unknown terminal effect. Exercise the actual browser
// grant, protected dispatcher, source TLS headers and exact PG cache writer.
func TestGatewayNativeExtensionPostgresStreamAndQuota(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_EXTENSION_TEST_DSN", "native_extension_test_")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	material := bytes.Repeat([]byte{0x5b}, 32)
	custody, err := service.NewGatewayNativeCredentialCustody("extension-quota", map[string][]byte{"extension-quota": material})
	require.NoError(t, err)
	secret := "extension-source-key-" + uuid.NewString()
	var challenge string
	var grants, calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "native-extension.example", r.Host)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.Header.Get("Cookie"))
		if r.URL.Path == "/token" {
			require.EqualValues(t, 1, grants.Add(1), "one browser grant has one effect entry")
			require.NoError(t, r.ParseForm())
			require.Equal(t, "source-one-use-code", r.PostForm.Get("code"))
			digest := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			require.Equal(t, challenge, base64.RawURLEncoding.EncodeToString(digest[:]))
			require.Empty(t, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"api_key": secret}))
			return
		}
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer "+secret, r.Header.Get("Authorization"))
		turn := calls.Add(1)
		require.LessOrEqual(t, turn, int32(2), "no hidden retry or metadata HTTP")
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NotContains(t, string(raw), secret)
		if turn == 2 {
			require.Contains(t, string(raw), "synthetic file contents")
		}
		w.Header().Set("X-Fixture-Rate-Limit", "100")
		w.Header().Set("X-Fixture-Rate-Remaining", fmt.Sprint(100-turn))
		w.Header().Set("X-Fixture-Rate-Reset", time.Now().UTC().Add(time.Minute).Format(time.RFC3339))
		nativeConnectionWriteProtocol(t, w, &nativeConnectionWireCase{model: service.GatewayNativeExtensionFixtureModel, protocol: service.APIProtocolResponses}, turn == 2)
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "native-extension.example:443" {
			return nil, service.ErrGatewayNativeIdentity
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionTLSUpstream{client: &http.Client{Transport: service.GatewayNativeExtensionFixtureTransport(transport), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	mount := service.GatewayConnectionMount{BaseURL: service.GatewayNativeExtensionFixtureBaseURL, Model: service.GatewayNativeExtensionFixtureModel, QualificationRef: "extension-quota"}
	method, err := service.NewGatewayNativeExtensionFixtureMethod("extension-quota-generation", mount, transport)
	require.NoError(t, err)
	dispatcher, err := service.NewGatewayNativeExtensionFixtureDispatcher(gateway)
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "extension-quota-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "extension-quota-owner")
	require.NoError(t, err)
	in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "extension-quota-consumer", Owner: "extension-quota-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "extension-" + uuid.NewString(), Connection: method.Selection()}
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if scope != in.Scope || !service.GatewayConnectionScopeAuthorized(ctx, scope) {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	var route *service.GatewayNativeRoute
	cleanup := func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
		return service.GatewayNativeOAuthCleanupAuthority{Owner: in.Scope.Owner, LeaseExpiresAt: time.Now().Add(time.Minute), Native: route}, nil
	}
	native, err := service.NewGatewayNativeConnections(service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: material,
		Methods: []service.GatewayNativeConnectionMethodAdapter{method}, Connect: owner, Refresh: owner, Cleanup: cleanup,
		Dispatchers: map[string]service.GatewayNativeConnectionDispatchAdapter{"openai_responses": dispatcher}, Gateway: gateway})
	require.NoError(t, err)
	begin, err := native.Begin(ownerCtx, in, map[string]string{})
	require.NoError(t, err)
	authorize, err := url.Parse(begin.AuthorizeURL)
	require.NoError(t, err)
	challenge = authorize.Query().Get("code_challenge")
	callback := service.GatewayNativeExtensionFixtureRedirect + "?" + (url.Values{"state": {authorize.Query().Get("state")}, "code": {"source-one-use-code"}}).Encode()
	completed, err := native.Submit(ownerCtx, in, map[string]string{"completion": callback})
	require.NoError(t, err)
	require.Equal(t, "completed", completed.State)
	published, err := native.Describe(ownerCtx, in)
	require.NoError(t, err)
	require.NotNil(t, published.Native)
	route = &published.Native.GatewayNativeRoute
	wrongGeneration := *route
	wrongGeneration.Generation = uuid.NewString()
	_, marked, err := native.BindRoute(ownerCtx, wrongGeneration, in.Scope.Account, 1, 1<<20, 512)
	require.True(t, marked, "an invalid generic generation cannot fall through to a legacy decoder")
	require.Error(t, err)
	_, marked, err = native.BindRoute(ownerCtx, *route, uuid.NewString(), 1, 1<<20, 512)
	require.True(t, marked)
	require.Error(t, err)
	require.Zero(t, calls.Load(), "canonical identity rejection occurs before provider entry")
	limits, err := service.NewGatewayAccountLimitsReaderV3(native, nil, nil, func(_ context.Context, input service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
		if input.OwnerRef != in.Scope.Owner || input.AccountRef != in.Scope.Account {
			return service.GatewayNativeRoute{}, service.ErrGatewayNativeIdentity
		}
		return *route, nil
	})
	require.NoError(t, err)
	read := service.GatewayAccountLimitsInput{OwnerRef: in.Scope.Owner, AccountRef: in.Scope.Account, ExpectedAuthorizationEpoch: 1, ProfileID: in.Connection.ProfileID}
	require.Equal(t, "unknown", limits.Read(ownerCtx, read).Status)
	input := json.RawMessage(`"Review source"`)
	for turn := 1; turn <= 2; turn++ {
		body := []byte(fmt.Sprintf(`{"model":%q,"input":%s,"stream":true,"store":false,"max_output_tokens":100,"tools":[{"type":"function","name":"read_source","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}}]}`, route.Model, input))
		recorder, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
		require.NoError(t, err, "a passive source quota observation must allow terminal closure")
		require.True(t, entered)
		output := nativeConnectionCompletedOutput(t, recorder.Body.String())
		require.NotEmpty(t, output)
		observed := limits.Read(ownerCtx, read)
		require.Equal(t, "snapshot", observed.Status)
		require.Len(t, observed.Windows, 1)
		window := observed.Windows[0]
		require.Equal(t, service.GatewayNativeExtensionFixtureProvider, window.Provider)
		require.Equal(t, "rate_limit", window.Dimension)
		require.Equal(t, "requests", window.Unit)
		require.Nil(t, window.Label, "rate windows cannot acquire a subscription-period label")
		require.NotNil(t, window.UsedPercent)
		require.Equal(t, float64(turn), *window.UsedPercent)
		require.EqualValues(t, turn, calls.Load(), "cached reads do not perform provider HTTP")
		if turn == 1 {
			history := []json.RawMessage{json.RawMessage(`{"role":"user","content":"Review source"}`)}
			history = append(history, output...)
			history = append(history, json.RawMessage(`{"type":"function_call_output","call_id":"call_review","output":"synthetic file contents"}`))
			input, err = json.Marshal(history)
			require.NoError(t, err)
		}
	}
	stored, err := accounts.GetByID(ownerCtx, route.AccountID)
	require.NoError(t, err)
	encrypted, err := json.Marshal(stored.Credentials)
	require.NoError(t, err)
	assert.NotContains(t, string(encrypted), secret)
	_, err = native.Cleanup(ownerCtx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "extension-erase", CleanupToken: "extension-cleanup-token"})
	require.NoError(t, err)
	require.Equal(t, "unavailable", limits.Read(ownerCtx, read).Status)
	require.EqualValues(t, 1, grants.Load())
	require.EqualValues(t, 2, calls.Load())
}
