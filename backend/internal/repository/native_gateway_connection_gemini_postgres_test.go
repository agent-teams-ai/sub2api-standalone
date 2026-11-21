//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nativeGeminiWireFixture struct {
	t                                                                      *testing.T
	variant, profile, method, base, model, endpoint                        string
	secret, initialRefresh, refresh, access, challenge, clientID, redirect string
	key                                                                    *rsa.PrivateKey
	wrapped                                                                bool
	phase, tokens, inference, projects                                     atomic.Int32
	loseToken                                                              atomic.Bool
}

func (f *nativeGeminiWireFixture) serve(w http.ResponseWriter, r *http.Request) {
	assert.NotNil(f.t, r.TLS)
	assert.Empty(f.t, r.Header.Get("Cookie"))
	assert.Empty(f.t, r.Header.Get("X-Codex-Turn-State"))
	if r.Host == "oauth2.googleapis.com" {
		n := f.tokens.Add(1)
		assert.Equal(f.t, "/token", r.URL.Path)
		require.NoError(f.t, r.ParseForm())
		if f.method == "vertex_service_account" {
			assert.Equal(f.t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.PostForm.Get("grant_type"))
			token, err := jwt.Parse(r.PostForm.Get("assertion"), func(*jwt.Token) (any, error) { return &f.key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}),
				jwt.WithAudience("https://oauth2.googleapis.com/token"), jwt.WithIssuer("fixture@gemini-project.iam.gserviceaccount.com"), jwt.WithExpirationRequired())
			require.NoError(f.t, err)
			require.True(f.t, token.Valid)
		} else {
			assert.Equal(f.t, f.clientID, r.PostForm.Get("client_id"))
			assert.NotEmpty(f.t, r.PostForm.Get("client_secret"))
			if f.method == "browser" && n == 1 {
				assert.Equal(f.t, "authorization_code", r.PostForm.Get("grant_type"))
				assert.Equal(f.t, "fixture-user-code", r.PostForm.Get("code"))
				assert.Equal(f.t, f.redirect, r.PostForm.Get("redirect_uri"))
				assert.Equal(f.t, f.challenge, geminicli.GenerateCodeChallenge(r.PostForm.Get("code_verifier")))
			} else {
				assert.Equal(f.t, "refresh_token", r.PostForm.Get("grant_type"))
				expected := f.refresh
				if n == 1 {
					expected = f.initialRefresh
				}
				assert.Equal(f.t, expected, r.PostForm.Get("refresh_token"))
			}
		}
		f.access, f.refresh = fmt.Sprintf("google-fixture-access-%s-%d", f.variant, n), fmt.Sprintf("google-fixture-refresh-%s-%d", f.variant, n)
		if f.loseToken.Load() {
			connection, _, err := w.(http.Hijacker).Hijack()
			require.NoError(f.t, err)
			_ = connection.Close()
			return
		}
		expiry := 3600
		if f.method != "vertex_service_account" && n == 1 {
			expiry = 45
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(f.t, json.NewEncoder(w).Encode(map[string]any{"access_token": f.access, "refresh_token": f.refresh, "token_type": "Bearer", "expires_in": expiry, "scope": "fixture-provider-scope"}))
		return
	}
	if r.URL.Path == "/v1internal:loadCodeAssist" {
		f.projects.Add(1)
		assert.Equal(f.t, "Bearer "+f.access, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cloudaicompanionProject":"auto-assigned-project","currentTier":{"id":"free-tier"}}`)
		return
	}
	f.inference.Add(1)
	assert.Equal(f.t, http.MethodPost, r.Method)
	assert.Equal(f.t, f.endpoint, "https://"+r.Host+r.URL.RequestURI())
	assert.Empty(f.t, r.Header.Get("x-api-key"))
	if f.method == "vertex_api_key" || f.method == "api_key" {
		assert.Equal(f.t, f.secret, r.Header.Get("x-goog-api-key"))
		assert.Empty(f.t, r.Header.Get("Authorization"))
	} else {
		assert.Equal(f.t, "Bearer "+f.access, r.Header.Get("Authorization"))
		assert.Empty(f.t, r.Header.Get("x-goog-api-key"))
	}
	body, err := io.ReadAll(r.Body)
	require.NoError(f.t, err)
	assert.NotContains(f.t, string(body), "foreign-consumer-token")
	assert.NotContains(f.t, string(body), "gemini-thoughts-v1.")
	assert.NotContains(f.t, string(body), "skip_thought_signature_validator")
	var request map[string]json.RawMessage
	require.NoError(f.t, json.Unmarshal(body, &request))
	if f.wrapped {
		assert.JSONEq(f.t, fmt.Sprintf("%q", f.model), string(request["model"]))
		assert.NotEmpty(f.t, request["project"])
		if strings.HasPrefix(f.profile, "antigravity-") {
			assert.Equal(f.t, `"antigravity"`, string(request["userAgent"]))
			assert.Equal(f.t, `"agent"`, string(request["requestType"]))
		}
		var inner map[string]json.RawMessage
		require.NoError(f.t, json.Unmarshal(request["request"], &inner))
		request = inner
	}
	assert.NotEmpty(f.t, request["contents"])
	assert.Contains(f.t, string(request["tools"]), `"parametersJsonSchema"`)
	if f.phase.Load() == 2 {
		assert.Contains(f.t, string(request["contents"]), "real-native-function-signature")
		assert.Contains(f.t, string(request["contents"]), "synthetic file contents")
		assert.Contains(f.t, string(request["contents"]), `"id":"native_review"`)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(raw string) {
		if f.wrapped {
			raw = `{"response":` + raw + `}`
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	}
	if f.phase.Load() == 3 {
		write(`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial only"}]}}]}`)
		return // A clean network EOF without provider finish must fail.
	}
	if f.phase.Load() == 2 {
		write(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"{\"findings\":[]}"},{"text":"","thoughtSignature":"real-native-text-signature"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`)
	} else {
		write(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Checking the file."},{"functionCall":{"id":"native_review","name":"read_source","args":{"path":"file.go"}},"thoughtSignature":"real-native-function-signature"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`)
	}
}

// This matrix exercises the real native journal, custody, refresh and final
// inference fence against migrated PostgreSQL and controlled TLS peers.
func TestGatewayNativeConnectionPostgresGeminiAndAntigravity(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_GEMINI_CONNECTION_TEST_DSN", "native_gemini_connection_test_")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("google-wire", map[string][]byte{"google-wire": bytes.Repeat([]byte{0x31}, 32)})
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var current atomic.Pointer[nativeGeminiWireFixture]
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().serve(w, r) }))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		switch address {
		case "oauth2.googleapis.com:443", "us-central1-aiplatform.googleapis.com:443", "aiplatform.googleapis.com:443", "cloudcode-pa.googleapis.com:443", "generativelanguage.googleapis.com:443", "qualified-native.example.test:443":
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
		}
		return nil, service.ErrGatewayNativeIdentity
	}
	defer transport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionTLSUpstream{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	dispatchers, err := service.NewGatewayNativeGeminiDispatchers(gateway, transport)
	require.NoError(t, err)
	antiDispatchers, err := service.NewGatewayNativeAntigravityDispatchers(gateway)
	require.NoError(t, err)
	for family, adapter := range antiDispatchers {
		dispatchers[family] = adapter
	}
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "google-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "google-owner")
	require.NoError(t, err)
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if ctx.Err() != nil || !service.GatewayConnectionScopeAuthorized(ctx, scope) || scope.Owner != "google-owner" {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	for _, variant := range []string{"vertex_service_account", "vertex_api_key_project", "vertex_api_key_express", "google_one", "code_assist", "ai_studio", "antigravity_browser", "antigravity_refresh_token", "antigravity_target"} {
		t.Run(variant, func(t *testing.T) {
			f := &nativeGeminiWireFixture{t: t, variant: variant, key: key, model: "gemini-3.1-pro-preview", secret: "google-key-" + uuid.NewString(), initialRefresh: "google-import-refresh-" + uuid.NewString(),
				profile: "gemini-vertex-chat-v1", method: "vertex_api_key", base: "https://us-central1-aiplatform.googleapis.com"}
			input := map[string]string{"api_key": f.secret, "project_id": "selected-project", "location": "us-central1"}
			f.endpoint = f.base + "/v1/projects/selected-project/locations/us-central1/publishers/google/models/" + f.model + ":streamGenerateContent?alt=sse"
			if variant == "vertex_service_account" {
				f.method = variant
				raw, err := json.Marshal(map[string]string{"type": "service_account", "project_id": "gemini-project", "private_key_id": "google-fixture", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "client_email": "fixture@gemini-project.iam.gserviceaccount.com", "token_uri": "https://foreign.invalid/do-not-send-assertion"})
				require.NoError(t, err)
				input = map[string]string{"service_account_json": string(raw), "project_id": "selected-project", "location": "us-central1"}
			} else if variant == "vertex_api_key_express" {
				f.base = "https://aiplatform.googleapis.com"
				f.endpoint = f.base + "/v1/publishers/google/models/" + f.model + ":streamGenerateContent?alt=sse"
				input = map[string]string{"api_key": f.secret}
			} else if !strings.HasPrefix(variant, "vertex_") {
				f.method, f.base, f.wrapped, f.clientID, f.redirect = "browser", geminicli.GeminiCliBaseURL, true, geminicli.GeminiCLIOAuthClientID, geminicli.GeminiCLIRedirectURI
				f.profile = map[string]string{"google_one": "gemini-google-one-responses-oauth-v1", "code_assist": "gemini-code-assist-responses-oauth-v1", "ai_studio": "gemini-ai-studio-responses-oauth-v1"}[variant]
				input = map[string]string{}
				if variant == "code_assist" {
					input["project_id"] = "selected-project"
				}
				if variant == "ai_studio" {
					f.base, f.wrapped, f.clientID, f.redirect = geminicli.AIStudioBaseURL, false, "fixture-custom.apps.googleusercontent.com", geminicli.AIStudioOAuthRedirectURI
				}
				if strings.HasPrefix(variant, "antigravity_") {
					f.profile, f.clientID, f.redirect = "antigravity-responses-oauth-v1", antigravity.ClientID, antigravity.RedirectURI
					if variant == "antigravity_refresh_token" {
						f.method = "refresh_token"
						input["refresh_token"] = f.initialRefresh
					}
					if variant == "antigravity_target" {
						f.profile, f.method, f.base, f.wrapped = "antigravity-target-responses-apikey-v1", "api_key", "https://qualified-native.example.test", false
						input = map[string]string{"api_key": f.secret, "target_id": "google-fixture", "project_id": "declared-project"}
					}
				}
				f.endpoint = f.base + "/v1internal:streamGenerateContent?alt=sse"
				if !f.wrapped {
					prefix := ""
					if variant == "antigravity_target" {
						prefix = "/antigravity"
					}
					f.endpoint = f.base + prefix + "/v1beta/models/" + f.model + ":streamGenerateContent?alt=sse"
				}
			}
			current.Store(f)
			mount := service.GatewayConnectionMount{BaseURL: f.base, Model: f.model, QualificationRef: "google-fixture"}
			var method service.GatewayNativeConnectionMethodAdapter
			if strings.HasPrefix(variant, "vertex_") {
				method, err = service.NewGatewayNativeCloudConnectionMethod(f.profile, f.method, "google-"+uuid.NewString(), mount)
			} else if strings.HasPrefix(variant, "antigravity_") {
				method, err = service.NewGatewayNativeAntigravityConnectionMethod(f.profile, f.method, "google-"+uuid.NewString(), mount, transport)
			} else {
				cfg := &config.Config{}
				cfg.Gemini.OAuth.ClientID, cfg.Gemini.OAuth.ClientSecret = "fixture-custom.apps.googleusercontent.com", "fixture-custom-secret"
				oauth := service.NewGeminiOAuthService(nil, NewGatewayNativeGeminiOAuthClient(cfg, transport), nil, nil, cfg)
				t.Cleanup(oauth.Stop)
				method, err = service.NewGatewayNativeGeminiConnectionMethod(f.profile, "google-"+uuid.NewString(), mount, oauth)
			}
			require.NoError(t, err)
			var route *service.GatewayNativeRoute
			cleanup := func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
				return service.GatewayNativeOAuthCleanupAuthority{Owner: "google-owner", LeaseExpiresAt: time.Now().Add(time.Minute), Native: route}, nil
			}
			native, err := service.NewGatewayNativeConnections(service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: bytes.Repeat([]byte{0x32}, 32), Methods: []service.GatewayNativeConnectionMethodAdapter{method}, Connect: owner, Refresh: owner, Cleanup: cleanup, Dispatchers: dispatchers, Gateway: gateway})
			require.NoError(t, err)
			in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "google-consumer", Owner: "google-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "google-" + uuid.NewString(), Connection: method.Selection()}
			created, err := native.Begin(ownerCtx, in, input)
			require.NoError(t, err)
			if f.method == "browser" {
				require.Equal(t, "prepared", created.State)
				authorize, err := url.Parse(created.AuthorizeURL)
				require.NoError(t, err)
				f.challenge = authorize.Query().Get("code_challenge")
				callback := f.redirect + "?" + url.Values{"state": {authorize.Query().Get("state")}, "code": {"fixture-user-code"}}.Encode()
				created, err = native.Submit(ownerCtx, in, map[string]string{"completion": callback})
				require.NoError(t, err)
			}
			require.Equal(t, "completed", created.State, "token exchanges=%d project reads=%d", f.tokens.Load(), f.projects.Load())
			published, err := native.Describe(ownerCtx, in)
			require.NoError(t, err)
			require.NotNil(t, published.Native)
			route = &published.Native.GatewayNativeRoute
			request := map[string]any{"model": f.model, "input": "Review file.go", "store": false, "stream": true, "max_output_tokens": 512, "tools": []any{map[string]any{"type": "function", "name": "read_source", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}}}}}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			f.phase.Store(1)
			first, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.NoError(t, err, "first turn entered=%t inference requests=%d", entered, f.inference.Load())
			require.True(t, entered)
			output := nativeConnectionCompletedOutput(t, first.Body.String())
			require.NotContains(t, first.Body.String(), "real-native-function-signature")
			toolResult := json.RawMessage(`{"type":"function_call_output","call_id":"native_review","output":"synthetic file contents"}`)
			request["input"] = append(output, toolResult)
			continued, err := json.Marshal(request)
			require.NoError(t, err)
			require.NoError(t, service.ValidateGatewayNativeProviderResponsesBounded(f.profile, continued, 1<<20), "continuation admission")
			for _, unsupported := range []string{`"logprobs":[{}]`, `"logprobs":null`} {
				changed := bytes.Replace(continued, []byte(`"logprobs":[]`), []byte(unsupported), 1)
				require.NotEqual(t, continued, changed, "native Responses output carries its empty probability marker")
				require.Error(t, service.ValidateGatewayNativeProviderResponsesBounded(f.profile, changed, 1<<20), "unsupported probability data must not be silently discarded")
			}
			f.phase.Store(2)
			second, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, continued)
			require.NoError(t, err, "second turn entered=%t inference requests=%d", entered, f.inference.Load())
			require.True(t, entered)
			require.NotEmpty(t, nativeConnectionCompletedOutput(t, second.Body.String()))
			require.EqualValues(t, 2, f.inference.Load())
			if created.Credential.RefreshPolicy == "native" {
				if variant == "antigravity_refresh_token" {
					f.loseToken.Store(true)
				}
				maintained, err := native.Maintain(ownerCtx, in)
				require.NoError(t, err)
				require.EqualValues(t, 2, f.tokens.Load())
				if f.loseToken.Load() {
					require.Equal(t, "unknown", maintained.State)
					replayed, err := native.Maintain(ownerCtx, in)
					require.NoError(t, err)
					require.Equal(t, "unknown", replayed.State)
					require.EqualValues(t, 2, f.tokens.Load(), "lost token ACK cannot trigger another rotation")
				} else {
					require.Equal(t, "completed", maintained.State)
					_, entered, err = nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, continued)
					require.Error(t, err)
					require.False(t, entered, "old credential revision must not authenticate the old thought carrier")
					require.EqualValues(t, 2, f.inference.Load())
				}
			}
			if !f.loseToken.Load() {
				f.phase.Store(3)
				failed, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
				require.ErrorIs(t, err, service.ErrGatewayNativeEffectUnknown)
				require.True(t, entered)
				require.NotContains(t, failed.Body.String(), `"type":"response.completed"`)
				require.EqualValues(t, 3, f.inference.Load(), "no retry or protocol fallback after a truncated native stream")
			}
			_, err = native.Cleanup(ownerCtx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "google-cleanup", CleanupToken: "google-cleanup-token"})
			require.NoError(t, err)
			_, entered, err = nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.Error(t, err)
			require.False(t, entered)
			var durable string
			require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_build_object('account',(SELECT to_jsonb(a) FROM accounts a WHERE id=$1),'intent',(SELECT to_jsonb(i) FROM gateway_oauth_connect_intents i WHERE operation_ref=$2))::text`, route.AccountID, in.Operation).Scan(&durable))
			require.NotContains(t, durable, f.secret)
			if f.access != "" {
				require.NotContains(t, durable, f.access)
			}
			require.NotContains(t, durable, "fixture-custom-secret")
		})
	}
}
