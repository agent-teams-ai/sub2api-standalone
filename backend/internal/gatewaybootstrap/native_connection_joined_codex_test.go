//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

// This peer implements the fixed provider authentication protocols. All native
// conversion, journal, custody, discovery and review code remains production
// code, reached through the same private HTTP composition as the other rows.
type nativeConnectionJoinedCodex struct {
	t                         *testing.T
	method, variant, clientID string
	account, access, refresh  string
	reconnectAccess           string
	reconnectRefresh          string
	key                       *rsa.PrivateKey
	agent, reconnectAgent     ed25519.PrivateKey
	mu                        sync.Mutex
	challenge, currentBearer  string
	code                      string
	codeUsed                  bool
	reconnectRefreshUsed      bool
	currentRefresh            string
	activeAgent               ed25519.PrivateKey
	deviceVerifier            string
	devicePoll                int
	tokens, checks, tasks     atomic.Int64
	models, inference, quotas atomic.Int64
	jwks, authorizations      atomic.Int64
	devices, polls            atomic.Int64
}

func newNativeConnectionJoinedCodexFamily(t *testing.T, cfg *Config) nativeConnectionJoinedFamily {
	t.Helper()
	profile := nativeConnectionJoinedProfile(service.GatewayCodexOAuthResponsesProfile, service.GatewayCodexOAuthBaseURL, service.GatewayCodexOAuthModel)
	cfg.OAuth = &OAuthConfig{IntentKey: cfg.Custody.Keys[0].Key, Profile: ProfileConfig{
		BaseURL: profile.BaseURL, Model: profile.Model, QualificationRef: profile.QualificationRef,
		RequestBytes: profile.RequestBytes, OutputBytes: profile.OutputBytes, Tokens: profile.Tokens, ProviderTokenUpperBound: profile.ProviderTokenUpperBound}}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	family := nativeConnectionJoinedFamily{Profiles: []gatewaytransport.QualifiedProfile{profile}, Hosts: []string{"auth.openai.com:443", "chatgpt.com:443"}}
	rows := []struct{ method, variant string }{
		{"browser", "refreshable_oauth"}, {"device_code", "refreshable_oauth"},
		{"refresh_token", "codex_refresh_token"}, {"mobile_refresh_token", "mobile_client_refresh_token"},
		{"auth_json", "refreshable_auth_json"}, {"auth_json", "refreshable_auth_json_with_supplied_at_and_claims"},
		{"codex_session", "raw_access_token"}, {"codex_session", "json_accessToken"}, {"codex_session", "json_access_token"},
		{"codex_session", "at_only_auth_json"}, {"codex_session", "at_plus_rt_auth_json"},
		{"codex_session", "agent_identity_auth_json"}, {"agent_identity", "agent_identity_auth_json"},
		{"codex_pat", "personal_access_token_at_prefix"},
	}
	for _, entry := range rows {
		f := &nativeConnectionJoinedCodex{t: t, method: entry.method, variant: entry.variant, clientID: openai.ClientID,
			account: uuid.NewString(), access: "joined-codex-access-" + uuid.NewString(), refresh: "joined-codex-refresh-" + uuid.NewString(),
			reconnectAccess: "joined-codex-access-" + uuid.NewString(), reconnectRefresh: "joined-codex-refresh-" + uuid.NewString(), key: key,
			deviceVerifier: strings.Repeat("device-verifier-", 5)}
		row := nativeConnectionJoinedRow{ID: nativeConnectionJoinedRowID(profile.Profile, entry.method, entry.variant),
			ProfileID: profile.Profile, MethodID: entry.method, Variant: entry.variant,
			CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native",
			Provider: "openai", Model: "future-native-model", ExecutionFamily: "codex_oauth", Execute: true}
		if entry.method == "mobile_refresh_token" {
			f.clientID = "app_LlGpXReQgckcGGUo2JrYvtJK"
		}
		if entry.method == "codex_pat" {
			f.access, f.reconnectAccess = "at-"+uuid.NewString(), "at-"+uuid.NewString()
			row.CredentialKind, row.RefreshPolicy, row.ExecutionFamily = "native", "none", "codex_pat"
		} else if entry.variant == "agent_identity_auth_json" {
			_, f.agent, err = ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			_, f.reconnectAgent, err = ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			row.CredentialKind, row.CredentialPolicy, row.ExecutionFamily = "native", "delegated_identity", "codex_agent_identity"
		} else if entry.method == "codex_session" && entry.variant != "at_plus_rt_auth_json" {
			row.RefreshPolicy, row.ExecutionFamily = "none", "codex_access_token"
		}
		f.currentBearer, f.currentRefresh, f.activeAgent = f.access, f.refresh, f.agent
		row.Fields, row.ReconnectFields = f.fields(false), f.fields(true)
		if entry.method == "browser" {
			row.Completion = []nativeConnectionJoinedCompletion{{Action: "submit", FromAuthorizationURL: true}}
		} else if entry.method == "device_code" {
			row.Completion = []nativeConnectionJoinedCompletion{{Action: "poll"}, {Action: "poll"}}
		}
		row.ReconnectCompletion = row.Completion
		peer := &nativeConnectionJoinedPeer{Row: row, Protocol: "responses", Handle: f.handle, InferenceAuth: f.inferenceAuth, Verify: f.verify, Counters: f.counters}
		if entry.method == "browser" {
			peer.ObserveAuthorizeURL = f.observeAuthorization
		}
		family.Peers = append(family.Peers, peer)
	}
	family.Compose = func(b nativeConnectionJoinedBuild) ([]service.GatewayNativeConnectionMethodAdapter, map[string]service.GatewayNativeConnectionDispatchAdapter, error) {
		credentials, ok := b.Repository.(service.GatewayNativeOAuthRepository)
		if !ok {
			return nil, nil, ErrDenied
		}
		dispatchRepository, ok := b.Repository.(service.GatewayNativeOAuthDispatchRepository)
		if !ok {
			return nil, nil, ErrDenied
		}
		refreshRepository, ok := b.Repository.(service.GatewayNativeOAuthRefreshRepository)
		if !ok {
			return nil, nil, ErrDenied
		}
		verifier := service.NewGatewayNativeOAuthVerifier(b.Transport)
		enrollment, err := service.NewGatewayNativeOAuthEnrollment(verifier, b.Custody, credentials, b.MaterialKey)
		if err != nil {
			return nil, nil, err
		}
		connect, err := service.NewGatewayNativeOAuthConnect(b.MaterialKey, b.Transport, b.Journal, enrollment, credentials)
		if err != nil {
			return nil, nil, err
		}
		dispatch, err := service.NewGatewayNativeOAuthDispatch(dispatchRepository, b.Custody, verifier, b.Transport)
		if err != nil {
			return nil, nil, err
		}
		refresh, err := service.NewGatewayNativeOAuthRefresh(refreshRepository, b.Custody, verifier, b.Transport)
		if err != nil {
			return nil, nil, err
		}
		connectCheck := func(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string) error {
			owner, err := b.Owners.AuthorizeNativeOAuthOwner(ctx, scope.Consumer, operation, scope.Account, scope.Generation)
			if err != nil || ctx.Err() != nil || owner != scope.Owner {
				return ErrDenied
			}
			return nil
		}
		refreshCheck := func(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string) error {
			owner, err := b.Owners.AuthorizeNativeOAuthRefreshOwner(ctx, scope.Consumer, operation, scope.Account, scope.Generation)
			if err != nil || ctx.Err() != nil || owner != scope.Owner {
				return ErrDenied
			}
			return nil
		}
		if connect.SetEntryAuthorizer(connectCheck) != nil || refresh.SetEntryAuthorizer(refreshCheck) != nil {
			return nil, nil, ErrDenied
		}
		generation, err := connectionConfigurationGeneration(*b.Config, profile)
		if err != nil {
			return nil, nil, err
		}
		methods, err := service.NewGatewayNativeCodexImportMethods(generation, service.GatewayConnectionMount{BaseURL: profile.BaseURL, Model: profile.Model, QualificationRef: profile.QualificationRef}, b.Transport)
		if err != nil {
			return nil, nil, err
		}
		imports, err := service.NewGatewayNativeCodexImportDispatcher(b.Gateway)
		if err != nil {
			return nil, nil, err
		}
		b.OAuth.connect, b.OAuth.dispatch, b.OAuth.refresh = connect, dispatch, refresh
		return methods, map[string]service.GatewayNativeConnectionDispatchAdapter{"codex_responses": imports}, nil
	}
	return family
}

func (f *nativeConnectionJoinedCodex) fields(reconnect bool) map[string]string {
	access, refresh, agent := f.access, f.refresh, f.agent
	if reconnect {
		access, refresh, agent = f.reconnectAccess, f.reconnectRefresh, f.reconnectAgent
	}
	switch f.method {
	case "browser", "device_code":
		return map[string]string{}
	case "refresh_token", "mobile_refresh_token":
		return map[string]string{"refresh_token": refresh}
	case "codex_pat":
		return map[string]string{"access_token": access}
	}
	var document any
	if f.method == "auth_json" {
		tokens := map[string]any{"refresh_token": refresh}
		if f.variant == "refreshable_auth_json_with_supplied_at_and_claims" {
			tokens["access_token"], tokens["id_token"], tokens["account_id"] = "untrusted-supplied-access", "untrusted-supplied-claims", "untrusted-account-hint"
			tokens["oauth_provenance"] = map[string]string{"issuer": service.GatewayOAuthIssuer, "client_id": openai.ClientID}
		}
		document = map[string]any{"auth_mode": "chatgpt", "tokens": tokens}
	} else {
		switch f.variant {
		case "raw_access_token":
			return map[string]string{"content": access}
		case "json_accessToken":
			document = map[string]any{"accessToken": access}
		case "json_access_token":
			document = map[string]any{"access_token": access}
		case "at_only_auth_json":
			document = map[string]any{"tokens": map[string]any{"access_token": access}}
		case "at_plus_rt_auth_json":
			document = map[string]any{"tokens": map[string]any{"access_token": access, "refresh_token": refresh, "id_token": "untrusted-supplied-id"}, "email": "untrusted@example.test"}
		case "agent_identity_auth_json":
			der, err := x509.MarshalPKCS8PrivateKey(agent)
			require.NoError(f.t, err)
			document = map[string]any{"agentIdentity": map[string]any{"agentRuntimeId": "runtime-" + f.account,
				"agentPrivateKey": base64.StdEncoding.EncodeToString(der), "accountId": f.account, "chatgptUserId": "user-" + f.account,
				"taskId": "untrusted-imported-task", "email": "untrusted-agent@example.test", "planType": "enterprise"}}
		default:
			f.t.Fatal("missing Codex inventory variant")
		}
	}
	raw, err := json.Marshal(document)
	require.NoError(f.t, err)
	return map[string]string{"content": string(raw)}
}

func (f *nativeConnectionJoinedCodex) observeAuthorization(raw string) (map[string]string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "auth.openai.com" || u.Path != "/oauth/authorize" || u.User != nil || u.Fragment != "" {
		return nil, ErrDenied
	}
	q := u.Query()
	if q.Get("client_id") != f.clientID || q.Get("redirect_uri") != service.GatewayOAuthConnectRedirect || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) < 16 || len(q.Get("code_challenge")) != 43 {
		return nil, ErrDenied
	}
	f.mu.Lock()
	f.challenge = q.Get("code_challenge")
	f.code, f.codeUsed = "joined-code-"+uuid.NewString(), false
	code := f.code
	f.mu.Unlock()
	f.authorizations.Add(1)
	completion := service.GatewayOAuthConnectRedirect + "?" + url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	return map[string]string{"completion": completion}, nil
}

func (f *nativeConnectionJoinedCodex) signedID() string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": service.GatewayOAuthIssuer, "sub": "subject-" + f.account,
		"aud": f.clientID, "iat": time.Now().Add(-time.Second).Unix(), "exp": time.Now().Add(time.Hour).Unix(), "email": "verified-codex@example.test"})
	token.Header["kid"] = "joined-codex"
	signed, err := token.SignedString(f.key)
	require.NoError(f.t, err)
	return signed
}

func (f *nativeConnectionJoinedCodex) authorized(r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("ChatGPT-Account-Id") != f.account || r.Header.Get("User-Agent") != service.CodexCanonicalUserAgent() ||
		r.Header.Get("Version") != service.CodexCanonicalClientVersion() || r.Header.Get("Cookie") != "" || r.Header.Get("x-api-key") != "" || r.Header.Get("X-Codex-Turn-State") != "" {
		return false
	}
	if len(f.agent) == 0 {
		return r.Header.Get("Authorization") == "Bearer "+f.currentBearer
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "AgentAssertion ") {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "AgentAssertion "))
	var assertion map[string]string
	if err != nil || json.Unmarshal(raw, &assertion) != nil || len(assertion) != 4 || assertion["agent_runtime_id"] != "runtime-"+f.account || assertion["task_id"] != fmt.Sprintf("task-%d", f.tasks.Load()) {
		return false
	}
	timestamp, err := time.Parse(time.RFC3339, assertion["timestamp"])
	if err != nil || timestamp.Before(time.Now().Add(-time.Minute)) || timestamp.After(time.Now().Add(time.Minute)) {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(assertion["signature"])
	message := []byte(assertion["agent_runtime_id"] + ":" + assertion["task_id"] + ":" + assertion["timestamp"])
	return err == nil && ed25519.Verify(f.activeAgent.Public().(ed25519.PublicKey), message, signature)
}

func (f *nativeConnectionJoinedCodex) inferenceAuth(r *http.Request) bool {
	ok := r.Method == http.MethodPost && r.Host == "chatgpt.com" && r.URL.Path == "/backend-api/codex/responses" && r.URL.RawQuery == "" && f.authorized(r)
	if ok {
		f.inference.Add(1)
	}
	return ok
}

func (f *nativeConnectionJoinedCodex) handle(w http.ResponseWriter, r *http.Request) bool {
	endpoint := r.Host + r.URL.Path
	switch endpoint {
	case "auth.openai.com/.well-known/jwks.json":
		require.True(f.t, r.Method == http.MethodGet && r.URL.RawQuery == "", "fixed Codex JWKS endpoint")
		f.jwks.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kid": "joined-codex", "kty": "RSA", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}})
	case "auth.openai.com/api/accounts/deviceauth/usercode":
		var body map[string]string
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		require.True(f.t, r.Method == http.MethodPost && f.method == "device_code" && len(body) == 1 && body["client_id"] == f.clientID, "fixed native device client")
		f.mu.Lock()
		f.devicePoll = 0
		f.code, f.codeUsed = "joined-code-"+uuid.NewString(), false
		f.mu.Unlock()
		f.devices.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"device_auth_id": "joined-device-" + f.account, "user_code": "JOINED-CODE", "interval": 1, "expires_in": 120})
	case "auth.openai.com/api/accounts/deviceauth/token":
		var body map[string]string
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		require.True(f.t, r.Method == http.MethodPost && f.method == "device_code" && len(body) == 2 && body["device_auth_id"] == "joined-device-"+f.account && body["user_code"] == "JOINED-CODE", "bound native device poll")
		f.polls.Add(1)
		f.mu.Lock()
		f.devicePoll++
		pending := f.devicePoll == 1
		code := f.code
		f.mu.Unlock()
		if pending {
			w.WriteHeader(http.StatusForbidden)
			return true
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"authorization_code": code, "code_verifier": f.deviceVerifier, "code_challenge": openai.GenerateCodeChallenge(f.deviceVerifier)})
	case "auth.openai.com/oauth/token":
		require.True(f.t, r.Method == http.MethodPost && r.URL.RawQuery == "", "fixed native token endpoint")
		require.NoError(f.t, r.ParseForm())
		require.True(f.t, r.PostForm.Get("client_id") == f.clientID, "native OAuth audience")
		f.mu.Lock()
		valid := false
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if f.method == "browser" {
				digest := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
				valid = f.challenge != "" && base64.RawURLEncoding.EncodeToString(digest[:]) == f.challenge && r.PostForm.Get("redirect_uri") == service.GatewayOAuthConnectRedirect
			} else if f.method == "device_code" {
				valid = r.PostForm.Get("code_verifier") == f.deviceVerifier && r.PostForm.Get("redirect_uri") == service.GatewayOAuthDeviceRedirect
			}
			valid = valid && !f.codeUsed && r.PostForm.Get("code") == f.code
			if valid {
				f.codeUsed = true
			}
		case "refresh_token":
			input := r.PostForm.Get("refresh_token")
			valid = input == f.currentRefresh
			if f.tokens.Load() > 0 && input == f.reconnectRefresh && !f.reconnectRefreshUsed {
				valid, f.reconnectRefreshUsed = true, true
			}
		}
		if valid {
			n := f.tokens.Add(1)
			f.currentBearer, f.currentRefresh = fmt.Sprintf("%s-rotated-%d", f.access, n), fmt.Sprintf("%s-rotated-%d", f.refresh, n)
		}
		access, refresh := f.currentBearer, f.currentRefresh
		f.mu.Unlock()
		require.True(f.t, valid, "bound native OAuth grant and PKCE")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": refresh, "id_token": f.signedID(), "token_type": "Bearer", "expires_in": 3600})
	case "chatgpt.com/backend-api/accounts/check/v4-2023-04-27":
		f.mu.Lock()
		if f.tokens.Load() == 0 && r.Header.Get("Authorization") == "Bearer "+f.reconnectAccess {
			f.currentBearer = f.reconnectAccess
		}
		valid := r.Header.Get("Authorization") == "Bearer "+f.currentBearer
		f.mu.Unlock()
		require.True(f.t, r.Method == http.MethodGet && r.URL.RawQuery == "" && valid && r.Header.Get("ChatGPT-Account-Id") == "", "authenticated native account check")
		f.checks.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{"workspace-map-key-is-not-account-identity": map[string]any{
			"account": map[string]any{"account_id": f.account, "is_default": true, "plan_type": "plus"}, "entitlement": map[string]any{"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}}})
	case "auth.openai.com/api/accounts/v1/user-auth-credential/whoami":
		f.mu.Lock()
		if r.Header.Get("Authorization") == "Bearer "+f.reconnectAccess {
			f.currentBearer = f.reconnectAccess
		}
		valid := r.Header.Get("Authorization") == "Bearer "+f.currentBearer
		f.mu.Unlock()
		require.True(f.t, r.Method == http.MethodGet && f.method == "codex_pat" && r.URL.RawQuery == "" && valid, "native PAT whoami")
		f.checks.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "pat@example.test", "chatgpt_user_id": "user-" + f.account, "chatgpt_account_id": f.account, "chatgpt_plan_type": "plus", "chatgpt_account_is_fedramp": false})
	case "chatgpt.com/backend-api/codex/models":
		require.True(f.t, r.Method == http.MethodGet && r.URL.Query().Get("client_version") == service.CodexCanonicalClientVersion() && len(r.URL.Query()) == 1 && f.authorized(r), "authenticated native model discovery")
		f.models.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"slug": "future-native-model", "visibility": "list", "supported_endpoints": []string{"responses"}, "input_modalities": []string{"text"}, "instructions": "PRIVATE-NATIVE-INSTRUCTIONS"}}})
	case "chatgpt.com/backend-api/wham/usage":
		require.True(f.t, r.Method == http.MethodGet && r.URL.RawQuery == "" && f.authorized(r), "authenticated native quota read")
		f.quotas.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"account_id": f.account, "rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": 25, "limit_window_seconds": 18000}}})
	default:
		if endpoint != "auth.openai.com/api/accounts/v1/agent/runtime-"+f.account+"/task/register" {
			return false
		}
		f.registerTask(w, r)
	}
	return true
}

func (f *nativeConnectionJoinedCodex) registerTask(w http.ResponseWriter, r *http.Request) {
	require.True(f.t, r.Method == http.MethodPost && r.URL.RawQuery == "" && r.Header.Get("Authorization") == "" && len(f.agent) == ed25519.PrivateKeySize, "fixed native Agent Identity task registration")
	var body map[string]string
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	signature, err := base64.StdEncoding.DecodeString(body["signature"])
	require.NoError(f.t, err)
	message := []byte("runtime-" + f.account + ":" + body["timestamp"])
	f.mu.Lock()
	if ed25519.Verify(f.reconnectAgent.Public().(ed25519.PublicKey), message, signature) {
		f.activeAgent = f.reconnectAgent
	}
	valid := len(body) == 2 && ed25519.Verify(f.activeAgent.Public().(ed25519.PublicKey), message, signature)
	agent := f.activeAgent
	f.mu.Unlock()
	require.True(f.t, valid, "signed native Agent Identity registration")
	timestamp, err := time.Parse(time.RFC3339, body["timestamp"])
	require.NoError(f.t, err)
	require.True(f.t, !timestamp.Before(time.Now().Add(-time.Minute)) && !timestamp.After(time.Now().Add(time.Minute)), "fresh Agent Identity registration")
	digest := sha512.Sum512(agent.Seed())
	digest[0] &= 248
	digest[31] &= 127
	digest[31] |= 64
	curve, err := curve25519.X25519(digest[:32], curve25519.Basepoint)
	require.NoError(f.t, err)
	var public [32]byte
	copy(public[:], curve)
	encrypted, err := box.SealAnonymous(nil, []byte(fmt.Sprintf("task-%d", f.tasks.Add(1))), &public, rand.Reader)
	require.NoError(f.t, err)
	_ = json.NewEncoder(w).Encode(map[string]string{"encrypted_task_id": base64.StdEncoding.EncodeToString(encrypted)})
}

func (f *nativeConnectionJoinedCodex) counters() map[string]int64 {
	return map[string]int64{"token_grants": f.tokens.Load(), "identity_checks": f.checks.Load(), "agent_tasks": f.tasks.Load(),
		"models": f.models.Load(), "inference": f.inference.Load(), "quotas": f.quotas.Load(), "jwks": f.jwks.Load(),
		"authorizations": f.authorizations.Load(), "device_starts": f.devices.Load(), "device_polls": f.polls.Load()}
}

func (f *nativeConnectionJoinedCodex) verify() {
	f.t.Helper()
	require.Positive(f.t, f.models.Load(), "Codex row must use real native model discovery")
	require.Positive(f.t, f.inference.Load(), "Codex row must execute through its native authentication family")
	if len(f.agent) != 0 {
		require.Positive(f.t, f.tasks.Load(), "Agent Identity must register and decrypt a native task")
		require.Zero(f.t, f.tokens.Load(), "Agent Identity does not use OAuth token exchange")
	} else {
		require.Positive(f.t, f.checks.Load(), "Codex account identity must come from the fixed authenticated provider endpoint")
	}
	if f.method == "browser" {
		require.Positive(f.t, f.authorizations.Load(), "browser completion must bind the native authorization URL")
		require.Positive(f.t, f.jwks.Load(), "the first Codex row must fetch the native signing key")
	}
	if f.method == "device_code" {
		require.Positive(f.t, f.devices.Load())
		require.GreaterOrEqual(f.t, f.polls.Load(), int64(2), "device pending and successful exchange must both execute")
	}
	if f.method == "browser" || f.method == "device_code" || f.method == "refresh_token" || f.method == "mobile_refresh_token" || f.method == "auth_json" || f.variant == "at_plus_rt_auth_json" {
		require.Positive(f.t, f.tokens.Load(), "refreshable Codex variants must exchange at the fixed native audience")
	}
}
