//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type nativeJoinedSubscriptionCode struct {
	state, challenge, nonce string
}

type nativeJoinedSubscriptionRefresh struct {
	access, nonce string
	initial       bool
}

type nativeJoinedSubscriptionDevice struct {
	code, user, consent string
	approved, used      bool
	pending             int
}

// Only the controlled upstream keeps this material. Reconnection receives a
// fresh cookie/refresh token, and neither consumed grants nor counters reset.
// The real native journal must prevent duplicate effects after a lost reply.
type nativeJoinedSubscriptionSource struct {
	t                            *testing.T
	mu                           sync.Mutex
	provider, method             string
	subject, team, lostRefresh   string
	key                          *ecdsa.PrivateKey
	codes                        map[string]nativeJoinedSubscriptionCode
	refreshes                    map[string]nativeJoinedSubscriptionRefresh
	access                       map[string]bool
	sessions                     map[string]bool
	devices                      map[string]*nativeJoinedSubscriptionDevice
	sessionDevice                map[string]string
	grants, rotations, codesUsed int64
	devicePolls, deviceGrants    int64
	cookieGrants, authorizations int64
	userinfo, quotas, inferences int64
	lostEntries, rejectedReplays int64
}

func newNativeConnectionJoinedSubscriptionFamily(t *testing.T, cfg *Config) nativeConnectionJoinedFamily {
	t.Helper()
	claude := nativeConnectionJoinedProfile(service.GatewayClaudeOAuthMessagesProfile, service.GatewayClaudeOAuthBaseURL, service.GatewayClaudeOAuthModel)
	grok := nativeConnectionJoinedProfile(service.GatewayGrokOAuthResponsesProfile, service.GatewayGrokOAuthBaseURL, service.GatewayGrokOAuthModel)
	profileConfig := func(p gatewaytransport.QualifiedProfile) ProfileConfig {
		return ProfileConfig{BaseURL: p.BaseURL, Model: p.Model, QualificationRef: p.QualificationRef,
			RequestBytes: p.RequestBytes, OutputBytes: p.OutputBytes, Tokens: p.Tokens, ProviderTokenUpperBound: p.ProviderTokenUpperBound}
	}
	c := profileConfig(claude)
	cfg.Claude = &c
	cfg.Grok = &GrokConfig{Profile: profileConfig(grok), ClientID: xai.DefaultClientID, RedirectURI: xai.DefaultRedirectURI, DeploymentQualified: true}
	family := nativeConnectionJoinedFamily{Profiles: []gatewaytransport.QualifiedProfile{claude, grok}, Private: make(map[string]string),
		Hosts: []string{"claude.ai:443", "platform.claude.com:443", "api.anthropic.com:443", "accounts.x.ai:443", "auth.x.ai:443", "cli-chat-proxy.grok.com:443"}}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	for _, spec := range []struct{ provider, method, variant string }{
		{"claude", "browser", "full_scope_oauth_code"},
		{"claude", "cookie_oauth", "session_key_assisted_oauth"},
		{"claude", "setup_token_code", "inference_scope_code"},
		{"claude", "setup_token_cookie", "session_key_assisted_inference_scope"},
		{"grok", "browser", "build_oauth"},
		{"grok", "refresh_token", "build_refresh_token"},
		{"grok", "sso", "web_sso_cookie"},
	} {
		profile, protocol := claude, "anthropic"
		if spec.provider == "grok" {
			profile, protocol = grok, "responses"
		}
		definition, ok := service.LookupGatewayConnectionProfile(profile.Profile)
		require.True(t, ok)
		f := &nativeJoinedSubscriptionSource{t: t, provider: spec.provider, method: spec.method, subject: uuid.NewString(), team: uuid.NewString(), key: key,
			codes: make(map[string]nativeJoinedSubscriptionCode), refreshes: make(map[string]nativeJoinedSubscriptionRefresh), access: make(map[string]bool),
			sessions: make(map[string]bool), devices: make(map[string]*nativeJoinedSubscriptionDevice), sessionDevice: make(map[string]string)}
		peer := &nativeConnectionJoinedPeer{Protocol: protocol, Row: nativeConnectionJoinedRow{
			ID: nativeConnectionJoinedRowID(profile.Profile, spec.method, spec.variant), ProfileID: profile.Profile, MethodID: spec.method, Variant: spec.variant,
			Fields: map[string]string{}, ReconnectFields: map[string]string{}, CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native",
			Provider: definition.Provider.ID, Model: profile.Model, ExecutionFamily: definition.DispatchFamily, Execute: true,
		}}
		if spec.provider == "claude" {
			peer.Row.ReviewProof = &nativeConnectionJoinedReviewProof{ReasoningEffort: "medium", MaxOutputTokens: 8192}
		}
		if strings.HasPrefix(spec.method, "setup_token_") {
			peer.Row.RefreshPolicy = "none"
		}
		switch spec.method {
		case "browser", "setup_token_code":
			peer.Row.Completion = []nativeConnectionJoinedCompletion{{Action: "submit", FromAuthorizationURL: true}}
			peer.Row.ReconnectCompletion = peer.Row.Completion
			peer.ObserveAuthorizeURL = f.observeAuthorizeURL
		case "cookie_oauth", "setup_token_cookie":
			initial, replacement := "sk-ant-sid01-"+uuid.NewString(), "sk-ant-sid01-"+uuid.NewString()
			f.sessions[initial], f.sessions[replacement] = false, false
			peer.Row.Fields["session_key"], peer.Row.ReconnectFields["session_key"] = initial, replacement
		case "refresh_token":
			initial, replacement := "joined-grok-refresh-"+uuid.NewString(), "joined-grok-refresh-"+uuid.NewString()
			f.refreshes[initial] = nativeJoinedSubscriptionRefresh{nonce: "original-import-nonce", initial: true}
			f.refreshes[replacement] = nativeJoinedSubscriptionRefresh{nonce: "fresh-reconnect-nonce", initial: true}
			peer.Row.Fields["refresh_token"], peer.Row.ReconnectFields["refresh_token"] = initial, replacement
			f.lostRefresh = "joined-grok-lost-refresh-" + uuid.NewString()
			family.Private["grokLostRefresh"] = f.lostRefresh
			family.Private["grokLostRefreshRowId"] = peer.Row.ID
		case "sso":
			initial, replacement := "joined-grok-sso-"+uuid.NewString(), "joined-grok-sso-"+uuid.NewString()
			f.sessions[initial], f.sessions[replacement] = false, false
			// The real native parser accepts both forms, and canonicalizes only
			// after validating the complete header for forbidden control bytes.
			peer.Row.Fields["sso_token"] = "sso=" + initial + "; sso-rw=" + initial
			peer.Row.ReconnectFields["sso_token"] = replacement
		}
		peer.Handle, peer.InferenceAuth, peer.Counters = f.handle, f.inferenceAuth, f.counters
		peer.Verify = func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			require.Greater(t, f.grants, int64(0), "native subscription token exchange was not observed")
			require.Greater(t, f.inferences, int64(0), "native subscription inference was not observed")
			require.Zero(t, f.rejectedReplays, "native replayed an already consumed provider grant")
			if spec.method == "sso" {
				require.GreaterOrEqual(t, f.devicePolls, int64(36), "the real slow SSO device flow was not completed")
			}
			if f.lostEntries > 0 {
				require.Equal(t, int64(1), f.lostEntries, "lost-ACK grant was entered more than once")
			}
		}
		family.Peers = append(family.Peers, peer)
	}
	family.Compose = func(b nativeConnectionJoinedBuild) ([]service.GatewayNativeConnectionMethodAdapter, map[string]service.GatewayNativeConnectionDispatchAdapter, error) {
		claudeRepo, err := repository.NewGatewayNativeClaudeRepository(b.DB)
		if err != nil {
			return nil, nil, err
		}
		b.OAuth.claude, err = service.NewGatewayNativeClaudeOAuth(claudeRepo, b.Custody, b.Transport)
		if err != nil {
			return nil, nil, err
		}
		grokRepo, err := repository.NewGatewayNativeGrokRepository(b.DB)
		if err != nil {
			return nil, nil, err
		}
		qualification := service.GatewayNativeGrokOAuthQualification{ClientID: b.Config.Grok.ClientID, RedirectURI: b.Config.Grok.RedirectURI, DeploymentQualified: b.Config.Grok.DeploymentQualified}
		b.OAuth.grok, err = service.NewGatewayNativeGrokOAuth(grokRepo, b.Custody, b.Transport, qualification)
		if err != nil {
			return nil, nil, err
		}
		authorize := func(refresh bool) service.GatewayNativeOAuthEntryAuthorizer {
			return func(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string) error {
				var owner string
				var err error
				if refresh {
					owner, err = b.Owners.AuthorizeNativeOAuthRefreshOwner(ctx, scope.Consumer, operation, scope.Account, scope.Generation)
				} else {
					owner, err = b.Owners.AuthorizeNativeOAuthOwner(ctx, scope.Consumer, operation, scope.Account, scope.Generation)
				}
				if err != nil || ctx.Err() != nil || owner != scope.Owner {
					return ErrDenied
				}
				return nil
			}
		}
		if err = b.OAuth.claude.SetEntryAuthorizers(authorize(false), authorize(true)); err != nil {
			return nil, nil, err
		}
		if err = b.OAuth.grok.SetEntryAuthorizers(authorize(false), authorize(true)); err != nil {
			return nil, nil, err
		}
		b.OAuth.cleanup = b.Owners.AuthorizeNativeOAuthCleanup
		var methods []service.GatewayNativeConnectionMethodAdapter
		for _, p := range family.Profiles {
			generation, err := connectionConfigurationGeneration(*b.Config, p)
			if err != nil {
				return nil, nil, err
			}
			mount := service.GatewayConnectionMount{BaseURL: p.BaseURL, Model: p.Model, QualificationRef: p.QualificationRef}
			var additions []service.GatewayNativeConnectionMethodAdapter
			if p.Profile == service.GatewayClaudeOAuthMessagesProfile {
				additions, err = service.NewGatewayNativeClaudeImportMethods(generation, mount, b.Transport)
			} else {
				additions, err = service.NewGatewayNativeGrokImportMethods(generation, mount, qualification, b.Transport)
			}
			if err != nil {
				return nil, nil, err
			}
			methods = append(methods, additions...)
		}
		claudeDispatch, err := service.NewGatewayNativeClaudeImportDispatcher(b.Gateway)
		if err != nil {
			return nil, nil, err
		}
		plain, err := service.NewGatewayNativeAPIKeyDispatchers(b.Gateway)
		if err != nil {
			return nil, nil, err
		}
		grokDispatch, err := service.NewGatewayNativeGrokImportDispatcher(b.Gateway, plain["grok_responses"])
		if err != nil {
			return nil, nil, err
		}
		return methods, map[string]service.GatewayNativeConnectionDispatchAdapter{"claude_messages": claudeDispatch, "grok_responses": grokDispatch}, nil
	}
	return family
}

func (f *nativeJoinedSubscriptionSource) observeAuthorizeURL(raw string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
		return nil, ErrDenied
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, ErrDenied
	}
	for _, values := range q {
		if len(values) != 1 {
			return nil, ErrDenied
		}
	}
	expected, client, redirect, scope := oauth.AuthorizeURL, oauth.ClientID, oauth.RedirectURI, oauth.ScopeAPI
	if f.method == "setup_token_code" {
		scope = oauth.ScopeInference
	}
	if f.provider == "grok" {
		expected, client, redirect, scope = xai.DefaultAuthorizeURL, xai.DefaultClientID, xai.DefaultRedirectURI, xai.DefaultScope
	}
	if "https://"+u.Host+u.Path != expected || q.Get("client_id") != client || q.Get("redirect_uri") != redirect || q.Get("scope") != scope ||
		q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		return nil, ErrDenied
	}
	state, challenge, nonce := q.Get("state"), q.Get("code_challenge"), q.Get("nonce")
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(challenge)
	if err != nil || len(decoded) != 32 {
		return nil, ErrDenied
	}
	if f.method == "setup_token_code" {
		decoded, err = base64.RawURLEncoding.Strict().DecodeString(state)
	} else {
		decoded, err = hex.DecodeString(state)
	}
	if err != nil || len(decoded) != 32 {
		return nil, ErrDenied
	}
	if f.provider == "grok" {
		decoded, err = base64.RawURLEncoding.Strict().DecodeString(nonce)
		if err != nil || len(decoded) != 32 {
			return nil, ErrDenied
		}
	}
	code := "joined-subscription-code-" + uuid.NewString()
	f.codes[code] = nativeJoinedSubscriptionCode{state: state, challenge: challenge, nonce: nonce}
	f.authorizations++
	completion := code
	if f.method == "setup_token_code" {
		completion += "#" + state
	}
	return map[string]string{"completion": completion}, nil
}

func (f *nativeJoinedSubscriptionSource) reject(w http.ResponseWriter, reason string, replay bool) bool {
	if replay {
		f.rejectedReplays++
	}
	f.t.Errorf("joined subscription peer rejected request: %s", reason)
	w.WriteHeader(http.StatusForbidden)
	return true
}

func (f *nativeJoinedSubscriptionSource) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if json.NewEncoder(w).Encode(value) != nil {
		f.t.Error("joined subscription peer could not write its bounded response")
	}
}

func (f *nativeJoinedSubscriptionSource) issue(w http.ResponseWriter, nonce string, initial bool, previousAccess string) {
	f.grants++
	access, refresh := "joined-subscription-access-"+uuid.NewString(), "joined-subscription-refresh-"+uuid.NewString()
	if previousAccess != "" {
		delete(f.access, previousAccess)
	}
	f.access[access] = true
	payload := map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 3600}
	if f.provider == "claude" {
		payload["scope"] = oauth.ScopeInference
		payload["account"] = map[string]string{"uuid": f.subject, "email_address": "joined-claude@example.test"}
		payload["organization"] = map[string]string{"uuid": f.team}
		if strings.HasPrefix(f.method, "setup_token_") {
			delete(payload, "refresh_token")
			payload["expires_in"] = 365 * 24 * 60 * 60
		} else {
			f.refreshes[refresh] = nativeJoinedSubscriptionRefresh{access: access, nonce: nonce}
		}
	} else {
		f.refreshes[refresh] = nativeJoinedSubscriptionRefresh{access: access, nonce: nonce}
		payload["scope"] = xai.DefaultScope
		if initial {
			if nonce == "" {
				nonce = "native-device-grant-nonce"
			}
			token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"iss": xai.OAuthIssuer, "aud": xai.DefaultClientID,
				"sub": f.subject, "team_id": f.team, "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(), "nonce": nonce})
			token.Header["kid"] = "joined-subscription-source"
			signed, err := token.SignedString(f.key)
			if err != nil {
				f.reject(w, "could not sign native identity", false)
				return
			}
			payload["id_token"] = signed
		}
	}
	f.writeJSON(w, payload)
}

func (f *nativeJoinedSubscriptionSource) handle(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	endpoint := "https://" + r.Host + r.URL.Path
	if f.provider == "claude" {
		switch endpoint {
		case "https://claude.ai/api/organizations":
			cookie, err := r.Cookie("sessionKey")
			if err != nil || r.Method != http.MethodGet {
				return f.reject(w, "cookie organizations authority", false)
			}
			if used, exists := f.sessions[cookie.Value]; !exists || used {
				return f.reject(w, "cookie organizations identity", used)
			}
			f.writeJSON(w, []any{map[string]string{"uuid": f.team, "raven_type": "team"}})
			return true
		case "https://claude.ai/v1/oauth/" + f.team + "/authorize":
			cookie, err := r.Cookie("sessionKey")
			if err != nil || r.Method != http.MethodPost {
				return f.reject(w, "cookie authorization authority", false)
			}
			if used, exists := f.sessions[cookie.Value]; !exists || used {
				return f.reject(w, "cookie authorization identity", used)
			}
			var body map[string]string
			scope := oauth.ScopeAPI
			if f.method == "setup_token_cookie" {
				scope = oauth.ScopeInference
			}
			if json.NewDecoder(io.LimitReader(r.Body, 65537)).Decode(&body) != nil || body["client_id"] != oauth.ClientID || body["redirect_uri"] != oauth.RedirectURI ||
				body["scope"] != scope || body["code_challenge_method"] != "S256" || body["state"] == "" || body["code_challenge"] == "" {
				return f.reject(w, "cookie authorization PKCE contract", false)
			}
			code := "joined-cookie-code-" + uuid.NewString()
			f.codes[code] = nativeJoinedSubscriptionCode{state: body["state"], challenge: body["code_challenge"]}
			f.sessions[cookie.Value] = true
			f.cookieGrants++
			f.writeJSON(w, map[string]string{"redirect_uri": oauth.RedirectURI + "?" + url.Values{"code": {code}, "state": {body["state"]}}.Encode()})
			return true
		case oauth.TokenURL:
			var body map[string]string
			if r.Method != http.MethodPost || r.Header.Get("Cookie") != "" || json.NewDecoder(io.LimitReader(r.Body, 65537)).Decode(&body) != nil || body["client_id"] != oauth.ClientID {
				return f.reject(w, "Claude token authority", false)
			}
			switch body["grant_type"] {
			case "authorization_code":
				code, exists := f.codes[body["code"]]
				if !exists || body["redirect_uri"] != oauth.RedirectURI || body["state"] != code.state || oauth.GenerateCodeChallenge(body["code_verifier"]) != code.challenge {
					return f.reject(w, "Claude one-use authorization code", !exists)
				}
				delete(f.codes, body["code"])
				f.codesUsed++
				f.issue(w, "", true, "")
			case "refresh_token":
				refresh, exists := f.refreshes[body["refresh_token"]]
				if !exists {
					return f.reject(w, "Claude one-use refresh token", true)
				}
				delete(f.refreshes, body["refresh_token"])
				f.rotations++
				f.issue(w, refresh.nonce, false, refresh.access)
			default:
				return f.reject(w, "Claude grant family", false)
			}
			return true
		}
		return false
	}
	return f.handleGrok(w, r, endpoint)
}

func (f *nativeJoinedSubscriptionSource) grokSession(r *http.Request) (string, bool) {
	cookie, err := r.Cookie("sso")
	if err != nil {
		return "", false
	}
	_, exists := f.sessions[cookie.Value]
	return cookie.Value, exists
}

// Called with f.mu held. Every redirect is the source's fixed native host and
// every successful poll consumes one real device grant. The first flow takes
// over 35 seconds and consequently crosses all former 15/30-second budgets.
func (f *nativeJoinedSubscriptionSource) handleGrok(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	switch endpoint {
	case xai.SSOAccountsURL:
		_, valid := f.grokSession(r)
		if r.Method != http.MethodGet || !valid {
			return f.reject(w, "SSO account cookie", false)
		}
		f.writeJSON(w, map[string]bool{"authenticated": true})
		return true
	case xai.SSODeviceURL:
		session, valid := f.grokSession(r)
		if r.Method != http.MethodPost || !valid || r.ParseForm() != nil || r.PostForm.Get("client_id") != xai.DefaultClientID {
			return f.reject(w, "SSO device authority", false)
		}
		if f.sessions[session] {
			return f.reject(w, "SSO cookie entered twice", true)
		}
		device := &nativeJoinedSubscriptionDevice{code: "joined-device-" + uuid.NewString(), user: "joined-user-" + uuid.NewString(), consent: "joined-consent-" + uuid.NewString()}
		if len(f.devices) == 0 {
			device.pending = 35
		}
		f.devices[device.code], f.sessionDevice[session], f.sessions[session] = device, device.code, true
		f.writeJSON(w, map[string]any{"device_code": device.code, "user_code": device.user,
			"verification_uri_complete": "https://auth.x.ai/device?device=" + device.code, "interval": 1, "expires_in": 120})
		return true
	case "https://auth.x.ai/device", "https://auth.x.ai/consent", "https://auth.x.ai/done":
		session, valid := f.grokSession(r)
		device := f.devices[r.URL.Query().Get("device")]
		if !valid || device == nil || f.sessionDevice[session] != device.code || r.Method != http.MethodGet {
			return f.reject(w, "SSO redirected device authority", false)
		}
		if r.URL.Path == "/consent" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<input type="hidden" name="consent_token" value="`+device.consent+`">`)
		} else {
			f.writeJSON(w, map[string]bool{"ready": true})
		}
		return true
	case xai.SSOVerifyURL, xai.SSOApproveURL:
		session, valid := f.grokSession(r)
		device := f.devices[f.sessionDevice[session]]
		if !valid || device == nil || r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("user_code") != device.user {
			return f.reject(w, "SSO user-code authority", false)
		}
		location := "/consent?device=" + device.code
		if endpoint == xai.SSOApproveURL {
			if device.approved || r.PostForm.Get("consent_token") != device.consent || r.PostForm.Get("action") != "allow" {
				return f.reject(w, "SSO one-use consent", device.approved)
			}
			device.approved = true
			location = "/done?device=" + device.code
		}
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusSeeOther)
		return true
	case xai.DefaultTokenURL:
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("client_id") != xai.DefaultClientID {
			return f.reject(w, "Grok token authority", false)
		}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			code, exists := f.codes[r.PostForm.Get("code")]
			if r.Header.Get("Cookie") != "" || !exists || r.PostForm.Get("redirect_uri") != xai.DefaultRedirectURI || oauth.GenerateCodeChallenge(r.PostForm.Get("code_verifier")) != code.challenge {
				return f.reject(w, "Grok one-use authorization code", !exists)
			}
			delete(f.codes, r.PostForm.Get("code"))
			f.codesUsed++
			f.issue(w, code.nonce, true, "")
		case "refresh_token":
			if r.Header.Get("Cookie") != "" {
				return f.reject(w, "Grok refresh carried a browser cookie", false)
			}
			input := r.PostForm.Get("refresh_token")
			if f.lostRefresh != "" && input == f.lostRefresh {
				f.lostEntries++
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					f.t.Error("joined subscription could not close lost-ACK connection")
				} else {
					_ = connection.Close()
				}
				return true
			}
			refresh, exists := f.refreshes[input]
			if !exists {
				return f.reject(w, "Grok one-use refresh token", true)
			}
			delete(f.refreshes, input)
			if !refresh.initial {
				f.rotations++
			}
			f.issue(w, refresh.nonce, refresh.initial, refresh.access)
		case "urn:ietf:params:oauth:grant-type:device_code":
			device := f.devices[r.PostForm.Get("device_code")]
			session, valid := f.grokSession(r)
			if !valid || device == nil || f.sessionDevice[session] != device.code || !device.approved || device.used {
				return f.reject(w, "Grok one-use device code", device != nil && device.used)
			}
			f.devicePolls++
			if device.pending > 0 {
				device.pending--
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				f.writeJSON(w, map[string]string{"error": "authorization_pending"})
				return true
			}
			device.used = true
			f.deviceGrants++
			f.issue(w, "native-device-grant-nonce", true, "")
		default:
			return f.reject(w, "Grok grant family", false)
		}
		return true
	case xai.ProtectedJWKSURL:
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			return f.reject(w, "Grok public signing key authority", false)
		}
		key := f.key.PublicKey
		f.writeJSON(w, map[string]any{"keys": []any{map[string]string{"kid": "joined-subscription-source", "kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig",
			"x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))), "y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32)))}}})
		return true
	case xai.ProtectedUserinfoURL, "https://cli-chat-proxy.grok.com/v1/billing":
		auth := r.Header.Get("Authorization")
		if r.Method != http.MethodGet || !strings.HasPrefix(auth, "Bearer ") || !f.access[strings.TrimPrefix(auth, "Bearer ")] || r.Header.Get("Cookie") != "" {
			return f.reject(w, "Grok authenticated identity authority", false)
		}
		if endpoint == xai.ProtectedUserinfoURL {
			f.userinfo++
			f.writeJSON(w, map[string]string{"sub": f.subject, "team_id": f.team, "email": "joined-grok@example.test", "name": "Joined Grok account"})
		} else {
			if r.Header.Get("x-grok-client-identifier") != xai.ProtectedClientIdentifier {
				return f.reject(w, "Grok billing client binding", false)
			}
			f.quotas++
			f.writeJSON(w, map[string]any{"config": map[string]any{"currentPeriod": map[string]string{"type": "weekly"}, "creditUsagePercent": 35,
				"monthlyLimit": map[string]int{"val": 15000}, "used": map[string]int{"val": 7500}}})
		}
		return true
	}
	return false
}

func (f *nativeJoinedSubscriptionSource) inferenceAuth(r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	auth := r.Header.Get("Authorization")
	if r.Method != http.MethodPost || !strings.HasPrefix(auth, "Bearer ") || !f.access[strings.TrimPrefix(auth, "Bearer ")] ||
		r.Header.Get("Cookie") != "" || r.Header.Get("x-api-key") != "" || r.Header.Get("X-Codex-Turn-State") != "" {
		return false
	}
	if f.provider == "claude" {
		if "https://"+r.Host+r.URL.RequestURI() != service.GatewayClaudeOAuthBaseURL+"/v1/messages?beta=true" || !strings.Contains(r.Header.Get("anthropic-beta"), "oauth-") || r.Header.Get("anthropic-version") != "2023-06-01" {
			return false
		}
	} else if "https://"+r.Host+r.URL.RequestURI() != service.GatewayGrokOAuthBaseURL+"/v1/responses" || r.Header.Get("X-XAI-Token-Auth") != xai.CLITokenAuth || r.Header.Get("x-grok-client-identifier") != xai.ProtectedClientIdentifier {
		return false
	}
	f.inferences++
	return true
}

func (f *nativeJoinedSubscriptionSource) counters() map[string]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[string]int64{"tokenExchanges": f.grants, "refreshRotations": f.rotations, "authorizationCodeGrants": f.codesUsed,
		"browserAuthorizations": f.authorizations, "cookieAuthorizations": f.cookieGrants, "devicePolls": f.devicePolls,
		"deviceGrants": f.deviceGrants, "userinfoReads": f.userinfo, "quotaReads": f.quotas, "inferenceEntries": f.inferences,
		"lostRefreshEntries": f.lostEntries, "rejectedGrantReplays": f.rejectedReplays}
}
