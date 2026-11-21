//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nativeSubscriptionWire struct {
	t                                             *testing.T
	method, provider, input, subject, team, state string
	clientID, nonce                               string
	key                                           *ecdsa.PrivateKey
	grant, calls, quotas                          atomic.Int32
	wrongPrincipal, loseReply, omitExpiry         atomic.Bool
	second                                        atomic.Bool
}

func (f *nativeSubscriptionWire) access() string {
	return fmt.Sprintf("fixture-access-%s-%d", f.method, f.grant.Load())
}
func (f *nativeSubscriptionWire) refresh() string {
	return fmt.Sprintf("fixture-refresh-%s-%d", f.method, f.grant.Load())
}

func (f *nativeSubscriptionWire) oauthClient() string {
	if f.clientID != "" {
		return f.clientID
	}
	return xai.DefaultClientID
}

func (f *nativeSubscriptionWire) token(w http.ResponseWriter, r *http.Request) {
	f.grant.Add(1)
	if f.loseReply.Load() {
		connection, _, err := w.(http.Hijacker).Hijack()
		assert.NoError(f.t, err)
		if err == nil {
			_ = connection.Close()
		}
		return
	}
	subject, team := f.subject, f.team
	if f.wrongPrincipal.Load() {
		subject, team = uuid.NewString(), uuid.NewString()
	}
	payload := map[string]any{"access_token": f.access(), "refresh_token": f.refresh(), "token_type": "Bearer", "expires_in": 45}
	if f.provider == "claude" {
		payload["scope"] = "user:inference"
		payload["account"] = map[string]string{"uuid": subject, "email_address": "claude-fixture@example.test"}
		payload["organization"] = map[string]string{"uuid": team}
		if strings.HasPrefix(f.method, "setup_token_") {
			delete(payload, "refresh_token")
			payload["expires_in"] = 365 * 24 * 60 * 60
		}
	} else {
		payload["scope"] = "openid profile grok-cli:access"
		if f.grant.Load() == 1 {
			nonce := f.nonce
			if nonce == "" {
				nonce = "original-import-nonce"
			}
			token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"iss": xai.OAuthIssuer, "aud": f.oauthClient(), "sub": subject, "team_id": team, "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(), "nonce": nonce})
			token.Header["kid"] = "subscription-fixture"
			signed, err := token.SignedString(f.key)
			require.NoError(f.t, err)
			payload["id_token"] = signed
		}
	}
	if f.omitExpiry.Load() {
		delete(payload, "expires_in")
	}
	w.Header().Set("Content-Type", "application/json")
	require.NoError(f.t, json.NewEncoder(w).Encode(payload))
}

func (f *nativeSubscriptionWire) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := f.t
	assert.NotNil(t, r.TLS)
	w.Header().Set("Content-Type", "application/json")
	switch "https://" + r.Host + r.URL.Path {
	case "https://claude.ai/api/organizations":
		cookie, err := r.Cookie("sessionKey")
		assert.NoError(t, err)
		if cookie != nil {
			assert.Equal(t, f.input, cookie.Value)
		}
		_, _ = fmt.Fprintf(w, `[{"uuid":%q,"raven_type":"team"}]`, f.team)
	case "https://claude.ai/v1/oauth/" + f.team + "/authorize":
		cookie, err := r.Cookie("sessionKey")
		assert.NoError(t, err)
		if cookie != nil {
			assert.Equal(t, f.input, cookie.Value)
		}
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, oauth.ClientID, body["client_id"])
		assert.Equal(t, oauth.RedirectURI, body["redirect_uri"])
		assert.Equal(t, "S256", body["code_challenge_method"])
		if f.method == "setup_token_cookie" {
			assert.Equal(t, oauth.ScopeInference, body["scope"])
		} else {
			assert.Equal(t, oauth.ScopeAPI, body["scope"])
		}
		f.state = body["state"]
		callback := oauth.RedirectURI + "?" + url.Values{"code": {"fixture-code"}, "state": {f.state}}.Encode()
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"redirect_uri": callback}))
	case oauth.TokenURL:
		assert.Empty(t, r.Header.Get("Cookie"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, oauth.ClientID, body["client_id"])
		if body["grant_type"] == "authorization_code" {
			assert.Equal(t, f.state, body["state"])
			assert.Equal(t, "fixture-code", body["code"])
			assert.Len(t, body["code_verifier"], 43)
		} else {
			assert.Equal(t, "refresh_token", body["grant_type"])
			assert.Equal(t, f.refresh(), body["refresh_token"])
		}
		f.token(w, r)
	case xai.SSOAccountsURL:
		cookie, err := r.Cookie("sso")
		assert.NoError(t, err)
		if cookie != nil {
			assert.Equal(t, f.input, cookie.Value)
		}
		_, _ = io.WriteString(w, `{"authenticated":true}`)
	case xai.SSODeviceURL:
		require.NoError(t, r.ParseForm())
		assert.Equal(t, xai.DefaultClientID, r.Form.Get("client_id"))
		_, _ = io.WriteString(w, `{"device_code":"fixture-device","user_code":"fixture-user","verification_uri_complete":"https://auth.x.ai/device","interval":1,"expires_in":30}`)
	case "https://auth.x.ai/device", "https://auth.x.ai/done":
		_, _ = io.WriteString(w, `{"ready":true}`)
	case xai.SSOVerifyURL:
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "fixture-user", r.Form.Get("user_code"))
		w.Header().Set("Location", "/consent")
		w.WriteHeader(http.StatusSeeOther)
	case "https://auth.x.ai/consent":
		_, _ = io.WriteString(w, `<input type="hidden" name="consent_token" value="fixture-consent">`)
	case xai.SSOApproveURL:
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "fixture-consent", r.Form.Get("consent_token"))
		assert.Equal(t, "allow", r.Form.Get("action"))
		w.Header().Set("Location", "/done")
		w.WriteHeader(http.StatusSeeOther)
	case xai.DefaultTokenURL:
		require.NoError(t, r.ParseForm())
		assert.Equal(t, f.oauthClient(), r.Form.Get("client_id"))
		if r.Form.Get("grant_type") == "refresh_token" {
			assert.Empty(t, r.Header.Get("Cookie"))
			expected := f.refresh()
			if f.grant.Load() == 0 {
				expected = f.input
			}
			assert.Equal(t, expected, r.Form.Get("refresh_token"))
		} else if r.Form.Get("grant_type") == "authorization_code" {
			assert.Equal(t, "fixture-code", r.Form.Get("code"))
			assert.Equal(t, xai.DefaultRedirectURI, r.Form.Get("redirect_uri"))
			assert.Len(t, r.Form.Get("code_verifier"), 43)
		} else {
			assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.Form.Get("grant_type"))
			assert.Equal(t, "fixture-device", r.Form.Get("device_code"))
		}
		f.token(w, r)
	case xai.ProtectedJWKSURL:
		assert.Empty(t, r.Header.Get("Authorization"))
		key := f.key.PublicKey
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kid": "subscription-fixture", "kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))), "y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32)))}}}))
	case xai.ProtectedUserinfoURL:
		assert.Equal(t, "Bearer "+f.access(), r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Cookie"))
		subject := f.subject
		if f.wrongPrincipal.Load() {
			subject = "foreign-fixture-subject"
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"sub": subject, "team_id": f.team, "email": "grok-fixture@example.test", "name": "Fixture account"}))
	case "https://cli-chat-proxy.grok.com/v1/billing":
		f.quotas.Add(1)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer "+f.access(), r.Header.Get("Authorization"))
		assert.Equal(t, xai.ProtectedClientIdentifier, r.Header.Get("x-grok-client-identifier"))
		assert.Empty(t, r.Header.Get("Cookie"))
		_, _ = io.WriteString(w, `{"config":{"currentPeriod":{"type":"weekly"},"creditUsagePercent":35,"monthlyLimit":{"val":15000},"used":{"val":7500}}}`)
	default:
		f.calls.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer "+f.access(), r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Cookie"))
		assert.Empty(t, r.Header.Get("X-Api-Key"))
		assert.Empty(t, r.Header.Get("X-Codex-Turn-State"))
		row := &nativeConnectionWireCase{model: service.GatewayGrokOAuthModel, protocol: service.APIProtocolResponses}
		if f.provider == "claude" {
			assert.Equal(t, "https://api.anthropic.com/v1/messages", "https://"+r.Host+r.URL.Path)
			assert.Contains(t, r.Header.Get("anthropic-beta"), "oauth-")
			row.model, row.protocol = service.GatewayClaudeOAuthModel, service.APIProtocolAnthropic
			w.Header().Set("anthropic-ratelimit-unified-5h-utilization", "0.25")
		} else {
			assert.Equal(t, service.GatewayGrokOAuthBaseURL+"/v1/responses", "https://"+r.Host+r.URL.Path)
			assert.Equal(t, xai.CLITokenAuth, r.Header.Get("X-XAI-Token-Auth"))
			assert.Equal(t, xai.ProtectedClientIdentifier, r.Header.Get("x-grok-client-identifier"))
			w.Header().Set("x-ratelimit-limit-requests", "100")
			w.Header().Set("x-ratelimit-remaining-requests", "75")
		}
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), f.input)
		if f.second.Load() {
			assert.Contains(t, string(raw), "synthetic file contents")
		}
		nativeConnectionWriteProtocol(t, w, row, f.second.Load())
	}
}

// Every method uses its actual native token/cookie/device code, an actual TLS
// fixture peer, and the migrated PG journal/custody/dispatch/refresh/erase path.
// The fixture never opens a connection to a public provider or bills inference.
func TestGatewayNativeConnectionPostgresSubscriptionMethods(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_SUBSCRIPTION_TEST_DSN", "native_subscription_test_")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("subscription-fixture", map[string][]byte{"subscription-fixture": bytes.Repeat([]byte{0x67}, 32)})
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	var current atomic.Pointer[nativeSubscriptionWire]
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fixture := current.Load(); fixture != nil {
			fixture.ServeHTTP(w, r)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		switch address {
		case "claude.ai:443", "platform.claude.com:443", "api.anthropic.com:443", "accounts.x.ai:443", "auth.x.ai:443", "api.x.ai:443", "cli-chat-proxy.grok.com:443":
		default:
			return nil, service.ErrGatewayNativeIdentity
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionTLSUpstream{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	dispatchers, err := service.NewGatewayNativeAPIKeyDispatchers(gateway)
	require.NoError(t, err)
	dispatchers["claude_messages"], err = service.NewGatewayNativeClaudeImportDispatcher(gateway)
	require.NoError(t, err)
	dispatchers["grok_responses"], err = service.NewGatewayNativeGrokImportDispatcher(gateway, dispatchers["grok_responses"])
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "subscription-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "subscription-owner")
	require.NoError(t, err)
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if ctx.Err() != nil || !service.GatewayConnectionScopeAuthorized(ctx, scope) || scope.Owner != "subscription-owner" {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	for _, name := range []string{"cookie_oauth", "setup_token_code", "setup_token_cookie", "refresh_token", "sso", "refresh_token_lost_ack", "sso_missing_expiry"} {
		t.Run(name, func(t *testing.T) {
			fixture := &nativeSubscriptionWire{t: t, method: strings.TrimSuffix(strings.TrimSuffix(name, "_lost_ack"), "_missing_expiry"), provider: "claude", input: "fixture-secret-" + uuid.NewString(), subject: uuid.NewString(), team: uuid.NewString(), key: key}
			if strings.HasPrefix(name, "refresh_token") || strings.HasPrefix(name, "sso") {
				fixture.provider = "grok"
			}
			fixture.loseReply.Store(strings.HasSuffix(name, "_lost_ack"))
			fixture.omitExpiry.Store(strings.HasSuffix(name, "_missing_expiry"))
			current.Store(fixture)
			base, model := service.GatewayClaudeOAuthBaseURL, service.GatewayClaudeOAuthModel
			var methods []service.GatewayNativeConnectionMethodAdapter
			generation := "subscription-" + uuid.NewString()
			if fixture.provider == "claude" {
				methods, err = service.NewGatewayNativeClaudeImportMethods(generation, service.GatewayConnectionMount{BaseURL: base, Model: model, QualificationRef: "subscription-fixture"}, transport)
			} else {
				base, model = service.GatewayGrokOAuthBaseURL, service.GatewayGrokOAuthModel
				methods, err = service.NewGatewayNativeGrokImportMethods(generation, service.GatewayConnectionMount{BaseURL: base, Model: model, QualificationRef: "subscription-fixture"}, service.GatewayNativeGrokOAuthQualification{ClientID: xai.DefaultClientID, RedirectURI: xai.DefaultRedirectURI, DeploymentQualified: true}, transport)
			}
			require.NoError(t, err)
			var method service.GatewayNativeConnectionMethodAdapter
			for _, candidate := range methods {
				if candidate.MethodID == fixture.method {
					method = candidate
				}
			}
			require.NotEmpty(t, method.MethodID)
			fields := map[string]string{"session_key": fixture.input}
			switch fixture.method {
			case "setup_token_code":
				fields = map[string]string{}
			case "refresh_token":
				fields = map[string]string{"refresh_token": fixture.input}
			case "sso":
				fields = map[string]string{"sso_token": "sso=" + fixture.input + "; sso-rw=" + fixture.input}
			}
			prepared, err := method.Prepare(fields)
			require.NoError(t, err)
			clear(prepared.Material)
			clear(prepared.EntryClaim)
			require.Zero(t, fixture.grant.Load(), "Prepare never contacts the provider")
			var route *service.GatewayNativeRoute
			cleanup := func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
				return service.GatewayNativeOAuthCleanupAuthority{Owner: "subscription-owner", LeaseExpiresAt: time.Now().Add(time.Minute), Native: route}, nil
			}
			dependencies := service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: bytes.Repeat([]byte{0x68}, 32), Methods: []service.GatewayNativeConnectionMethodAdapter{method}, Connect: owner, Refresh: owner, Cleanup: cleanup, Dispatchers: dispatchers, Gateway: gateway}
			native, err := service.NewGatewayNativeConnections(dependencies)
			require.NoError(t, err)
			in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "subscription-consumer", Owner: "subscription-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "subscription-" + uuid.NewString(), Connection: method.Selection()}
			created, err := native.Begin(ownerCtx, in, fields)
			require.NoError(t, err)
			if fixture.method == "setup_token_code" {
				require.Equal(t, "prepared", created.State)
				authorize, err := url.Parse(created.AuthorizeURL)
				require.NoError(t, err)
				fixture.state = authorize.Query().Get("state")
				require.Equal(t, oauth.ScopeInference, authorize.Query().Get("scope"))
				require.Zero(t, fixture.grant.Load())
				// A new reader uses the sealed original PKCE and client.
				native, err = service.NewGatewayNativeConnections(dependencies)
				require.NoError(t, err)
				created, err = native.Submit(ownerCtx, in, map[string]string{"completion": "fixture-code#" + fixture.state})
				require.NoError(t, err)
			}
			if fixture.loseReply.Load() || fixture.omitExpiry.Load() {
				require.Equal(t, "unknown", created.State)
				require.Nil(t, created.Credential)
				require.EqualValues(t, 1, fixture.grant.Load())
				native, err = service.NewGatewayNativeConnections(dependencies)
				require.NoError(t, err)
				again, err := native.Begin(ownerCtx, in, fields)
				require.NoError(t, err)
				require.Equal(t, "unknown", again.State)
				require.EqualValues(t, 1, fixture.grant.Load(), "unknown token effect is never replayed")
				return
			}
			require.Equal(t, "completed", created.State)
			require.NotNil(t, created.Credential)
			require.Equal(t, "oauth", created.Credential.CredentialKind)
			refreshPolicy := "native"
			if strings.HasPrefix(fixture.method, "setup_token_") {
				refreshPolicy = "none"
			}
			require.Equal(t, refreshPolicy, created.Credential.RefreshPolicy)
			require.EqualValues(t, 1, fixture.grant.Load())
			published, err := native.Describe(ownerCtx, in)
			require.NoError(t, err)
			require.NotNil(t, published.Native)
			route = &published.Native.GatewayNativeRoute
			require.Equal(t, method.Selection(), published.Native.Publication.Connection)
			var durable string
			require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_build_object('account',(SELECT to_jsonb(a) FROM accounts a WHERE id=$1),'intent',(SELECT to_jsonb(i) FROM gateway_oauth_connect_intents i WHERE operation_ref=$2))::text`, route.AccountID, in.Operation).Scan(&durable))
			for _, secret := range []string{fixture.input, fixture.access(), fixture.refresh()} {
				require.NotContains(t, durable, secret)
			}
			// Reconnect/recovery reads do not need current creation methods.
			recoveryDependencies := dependencies
			recoveryDependencies.Methods = nil
			recovered, err := service.NewGatewayNativeConnections(recoveryDependencies)
			require.NoError(t, err)
			receipt, err := recovered.Read(ownerCtx, in)
			require.NoError(t, err)
			require.Equal(t, created, receipt)
			require.EqualValues(t, 1, fixture.grant.Load())
			foreign := in
			foreign.Connection.ConfigurationGeneration = "changed-client-generation"
			_, err = recovered.Read(ownerCtx, foreign)
			require.Error(t, err)
			maintained, err := native.Maintain(ownerCtx, in)
			require.NoError(t, err)
			if refreshPolicy == "native" {
				require.Equal(t, "completed", maintained.State)
				require.EqualValues(t, 2, fixture.grant.Load())
			} else {
				require.Equal(t, "idle", maintained.State)
				require.EqualValues(t, 1, fixture.grant.Load())
			}
			// Refresh preserves immutable method, kind and actual physical birth.
			after, err := native.Describe(ownerCtx, in)
			require.NoError(t, err)
			require.Equal(t, published.Native.Publication, after.Native.Publication)
			route = &after.Native.GatewayNativeRoute
			limitsReader, err := service.NewGatewayAccountLimitsReaderV3(native, nil, nil, func(context.Context, service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
				return *route, nil
			})
			require.NoError(t, err)
			limitsInput := service.GatewayAccountLimitsInput{OwnerRef: in.Scope.Owner, AccountRef: in.Scope.Account, ProfileID: in.Connection.ProfileID, ExpectedAuthorizationEpoch: 1}
			require.Equal(t, "unknown", limitsReader.Read(ownerCtx, limitsInput).Status)
			if fixture.provider == "grok" {
				candidates, err := native.ListQuotaAccounts(ownerCtx)
				require.NoError(t, err)
				require.Len(t, candidates, 1)
				require.NoError(t, native.CollectQuota(ownerCtx, candidates[0], service.NewGatewayProviderQuotaCollector(transport)))
				quota := limitsReader.Read(ownerCtx, limitsInput)
				require.Equal(t, "snapshot", quota.Status)
				require.Len(t, quota.Windows, 2)
				require.EqualValues(t, 2, fixture.quotas.Load(), "one bounded weekly/monthly provider batch")
			}
			call := func(body []byte) (*httptest.ResponseRecorder, bool, error) {
				callCtx, stop := context.WithTimeout(ownerCtx, 10*time.Second)
				defer stop()
				life, err := service.NewGatewayNativeLifetime(callCtx, stop, func() {}, 1<<20)
				require.NoError(t, err)
				life.BindAccount(in.Scope.Consumer, in.Scope.Account, in.Scope.Generation)
				require.True(t, life.Admit(time.Now().Add(5*time.Second)))
				callCtx = service.WithGatewayNativeLifetime(service.WithGatewayNativeCustody(callCtx, custody), life)
				bound, handled, err := native.BindRoute(callCtx, *route, in.Scope.Account, 1, 1<<20, 512)
				if err != nil {
					life.NoForward()
					return httptest.NewRecorder(), false, err
				}
				require.True(t, handled)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("Authorization", "Bearer inbound-secret-must-not-leak")
				c.Request.Header.Set("X-Codex-Turn-State", "foreign")
				_, entered, callErr := gateway.ForwardGatewayRoute(bound, c, *route, body)
				return recorder, entered, callErr
			}
			request := map[string]any{"model": model, "input": "Review file.go", "store": false, "stream": true, "service_tier": "default", "max_output_tokens": 512, "tools": []any{map[string]any{"type": "function", "name": "read_source", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}}}}}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			first, entered, err := call(body)
			require.NoError(t, err)
			require.True(t, entered)
			passiveQuota := limitsReader.Read(ownerCtx, limitsInput)
			require.Equal(t, "snapshot", passiveQuota.Status)
			observedPassive := false
			for _, window := range passiveQuota.Windows {
				if fixture.provider == "claude" && window.ID == "five_hour" || fixture.provider == "grok" && window.ID == "request_rate" {
					require.Equal(t, 25.0, *window.UsedPercent)
					observedPassive = true
				}
			}
			require.True(t, observedPassive, "native source response headers reach the cached V3 reader")
			if fixture.provider == "grok" {
				require.Len(t, passiveQuota.Windows, 3, "rate observation preserves independent weekly/monthly quota")
				require.EqualValues(t, 2, fixture.quotas.Load(), "metadata read never repeats provider collection")
			}
			output := nativeConnectionCompletedOutput(t, first.Body.String())
			toolResult, err := json.Marshal(map[string]string{"type": "function_call_output", "call_id": "call_review", "output": "synthetic file contents"})
			require.NoError(t, err)
			request["input"] = append(output, toolResult)
			body, err = json.Marshal(request)
			require.NoError(t, err)
			fixture.second.Store(true)
			second, entered, err := call(body)
			require.NoError(t, err)
			require.True(t, entered)
			message := nativeConnectionCompletedOutput(t, second.Body.String())
			require.NotEmpty(t, message)
			// Replay the native output unchanged, including id/status and the
			// encoder's empty annotations/logprobs, through another actual TLS call.
			history := append(append(append([]json.RawMessage{}, output...), toolResult), message...)
			history = append(history, json.RawMessage(`{"role":"user","content":"Confirm the result"}`))
			request["input"] = history
			body, err = json.Marshal(request)
			require.NoError(t, err)
			if fixture.provider == "claude" {
				incomplete := bytes.Replace(body, []byte(`"status":"completed"`), []byte(`"status":"incomplete"`), 1)
				_, entered, err = call(incomplete)
				require.Error(t, err)
				require.False(t, entered, "incomplete output cannot enter provider dispatch")
			}
			third, entered, err := call(body)
			require.NoError(t, err)
			require.True(t, entered)
			require.NotEmpty(t, nativeConnectionCompletedOutput(t, third.Body.String()))
			require.EqualValues(t, 3, fixture.calls.Load())
			if refreshPolicy == "native" {
				fixture.wrongPrincipal.Store(true)
				unknown, err := native.Maintain(ownerCtx, in)
				require.NoError(t, err)
				require.Equal(t, "unknown", unknown.State)
				require.Equal(t, "unavailable", limitsReader.Read(ownerCtx, limitsInput).Status, "uncertain credential refresh cannot reuse cached principal")
				count := fixture.grant.Load()
				again, err := native.Maintain(ownerCtx, in)
				require.NoError(t, err)
				require.Equal(t, "unknown", again.State)
				require.Equal(t, count, fixture.grant.Load(), "identity-changing refresh cannot be repeated")
			}
			_, err = recovered.Cleanup(ownerCtx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "subscription-cleanup", CleanupToken: "fixture-cleanup"})
			require.NoError(t, err)
			_, entered, err = call(body)
			require.Error(t, err)
			require.False(t, entered)
			require.EqualValues(t, 3, fixture.calls.Load(), "erased credential never reaches TLS")
		})
	}
}
