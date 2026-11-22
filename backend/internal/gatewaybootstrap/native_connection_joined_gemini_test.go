//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"crypto/rsa"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newNativeConnectionJoinedGeminiFamily(t *testing.T, cfg *Config, express bool) nativeConnectionJoinedFamily {
	t.Helper()
	cfg.GeminiOAuth = &NativeGeminiOAuthConfig{ClientID: "joined-custom.apps.googleusercontent.com", ClientSecret: "joined-fixture-client-secret"}
	family := nativeConnectionJoinedFamily{Hosts: []string{"oauth2.googleapis.com:443", "us-central1-aiplatform.googleapis.com:443", "aiplatform.googleapis.com:443",
		"cloudcode-pa.googleapis.com:443", "generativelanguage.googleapis.com:443", "qualified-native.example.test:443"}}
	variants := []string{"google_service_account_json", "project_region", "google_oauth_google_one", "google_oauth_code_assist", "google_oauth_ai_studio", "google_oauth", "google_refresh_token", "upstream_api_key"}
	if express {
		variants = []string{"express_projectless"}
	}
	profiles := map[string]bool{}
	for _, variant := range variants {
		profile, method, base, kind, policy, refreshPolicy := "gemini-vertex-chat-v1", "vertex_api_key", "https://us-central1-aiplatform.googleapis.com", "api_key", "cloud_identity", "none"
		model, secret := "gemini-3.1-pro-preview", "joined-google-key-"+uuid.NewString()
		initialRefresh, replacementRefresh := "joined-google-refresh-"+uuid.NewString(), "joined-google-refresh-"+uuid.NewString()
		fields := map[string]string{"api_key": secret, "project_id": "selected-project", "location": "us-central1"}
		reconnect := maps.Clone(fields)
		reconnect["api_key"] = "joined-google-replacement-" + uuid.NewString()
		var key *rsa.PrivateKey
		const email = "fixture@gemini-project.iam.gserviceaccount.com"
		clientID, redirect, wrapped := "", "", false
		endpoint := base + "/v1/projects/selected-project/locations/us-central1/publishers/google/models/" + model + ":streamGenerateContent?alt=sse"
		if variant == "google_service_account_json" {
			method, kind = "vertex_service_account", "native"
			var document string
			key, document = nativeConnectionJoinedServiceAccount(t, email, "gemini-project")
			fields = map[string]string{"service_account_json": document, "project_id": "selected-project", "location": "us-central1"}
			reconnect = maps.Clone(fields)
			_, reconnect["service_account_json"] = nativeConnectionJoinedServiceAccount(t, email, "gemini-project")
		} else if variant == "express_projectless" {
			base = "https://aiplatform.googleapis.com"
			endpoint = base + "/v1/publishers/google/models/" + model + ":streamGenerateContent?alt=sse"
			fields = map[string]string{"api_key": secret}
			reconnect = map[string]string{"api_key": "joined-google-replacement-" + uuid.NewString()}
		} else if variant != "project_region" {
			method, kind, policy, refreshPolicy = "browser", "oauth", "subscription_session", "native"
			base, clientID, redirect, wrapped = geminicli.GeminiCliBaseURL, geminicli.GeminiCLIOAuthClientID, geminicli.GeminiCLIRedirectURI, true
			fields, reconnect = map[string]string{}, map[string]string{}
			profile = map[string]string{"google_oauth_google_one": "gemini-google-one-responses-oauth-v1", "google_oauth_code_assist": "gemini-code-assist-responses-oauth-v1", "google_oauth_ai_studio": "gemini-ai-studio-responses-oauth-v1"}[variant]
			if variant == "google_oauth_code_assist" {
				fields["project_id"], reconnect["project_id"] = "selected-project", "selected-project"
			}
			if variant == "google_oauth_ai_studio" {
				base, clientID, redirect, wrapped = geminicli.AIStudioBaseURL, cfg.GeminiOAuth.ClientID, geminicli.AIStudioOAuthRedirectURI, false
			}
			if profile == "" {
				profile, clientID, redirect = "antigravity-responses-oauth-v1", antigravity.ClientID, antigravity.RedirectURI
				if variant == "google_refresh_token" {
					method = "refresh_token"
					fields["refresh_token"], reconnect["refresh_token"] = initialRefresh, replacementRefresh
				} else if variant == "upstream_api_key" {
					profile, method, base, wrapped = "antigravity-target-responses-apikey-v1", "api_key", "https://qualified-native.example.test", false
					kind, policy, refreshPolicy = "api_key", "provider_key", "none"
					fields = map[string]string{"api_key": secret, "target_id": "joined-connections", "project_id": "declared-project"}
					reconnect = maps.Clone(fields)
					reconnect["api_key"] = "joined-google-replacement-" + uuid.NewString()
				}
			}
			endpoint = base + "/v1internal:streamGenerateContent?alt=sse"
			if !wrapped {
				prefix := ""
				if variant == "upstream_api_key" {
					prefix = "/antigravity"
				}
				endpoint = base + prefix + "/v1beta/models/" + model + ":streamGenerateContent?alt=sse"
			}
		}
		if !profiles[profile] {
			profiles[profile] = true
			family.Profiles = append(family.Profiles, nativeConnectionJoinedProfile(profile, base, model))
		}
		definition, ok := service.LookupGatewayConnectionProfile(profile)
		require.True(t, ok)
		peer := &nativeConnectionJoinedPeer{Protocol: "gemini", Row: nativeConnectionJoinedRow{
			ID: nativeConnectionJoinedRowID(profile, method, variant), ProfileID: profile, MethodID: method, Variant: variant, Fields: fields, ReconnectFields: reconnect,
			ReconnectCustody: strings.HasPrefix(method, "vertex_") || method == "api_key",
			CredentialKind:   kind, CredentialPolicy: policy, RefreshPolicy: refreshPolicy, Provider: definition.Provider.ID, Model: model, ExecutionFamily: definition.DispatchFamily, Execute: true,
		}}
		var lock sync.Mutex
		access, refresh, code, challenge := "joined-google-access-"+uuid.NewString(), initialRefresh, "", ""
		var tokens, projects, authorizations atomic.Int64
		if method == "browser" {
			peer.Row.Completion = []nativeConnectionJoinedCompletion{{Action: "submit", FromAuthorizationURL: true}}
			peer.Row.ReconnectCompletion = peer.Row.Completion
			peer.ObserveAuthorizeURL = func(raw string) (map[string]string, error) {
				u, err := url.Parse(raw)
				if err != nil || u.Scheme != "https" || u.Host != "accounts.google.com" || u.User != nil || u.Fragment != "" {
					return nil, ErrDenied
				}
				q := u.Query()
				if q.Get("client_id") != clientID || q.Get("redirect_uri") != redirect || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" {
					return nil, ErrDenied
				}
				lock.Lock()
				defer lock.Unlock()
				authorizations.Add(1)
				challenge, code = q.Get("code_challenge"), "joined-browser-code-"+uuid.NewString()
				return map[string]string{"completion": redirect + "?" + url.Values{"state": {q.Get("state")}, "code": {code}}.Encode()}, nil
			}
		}
		peer.Handle = func(w http.ResponseWriter, r *http.Request) bool {
			lock.Lock()
			defer lock.Unlock()
			if r.Host == "oauth2.googleapis.com" && r.URL.Path == "/token" {
				tokens.Add(1)
				if method == "api_key" || method == "vertex_api_key" {
					w.WriteHeader(http.StatusForbidden)
					return true
				}
				if key != nil {
					return nativeConnectionJoinedServiceAccountToken(w, r, key, email, access)
				}
				if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("client_id") != clientID || r.PostForm.Get("client_secret") == "" {
					w.WriteHeader(http.StatusForbidden)
					return true
				}
				switch r.PostForm.Get("grant_type") {
				case "authorization_code":
					if method != "browser" || code == "" || r.PostForm.Get("code") != code || r.PostForm.Get("redirect_uri") != redirect || geminicli.GenerateCodeChallenge(r.PostForm.Get("code_verifier")) != challenge {
						w.WriteHeader(http.StatusForbidden)
						return true
					}
					code = "" // Actual one-use code; retrying the consumed code cannot pass.
				case "refresh_token":
					input := r.PostForm.Get("refresh_token")
					if input == "" || input != refresh && input != replacementRefresh {
						w.WriteHeader(http.StatusForbidden)
						return true
					}
				default:
					w.WriteHeader(http.StatusForbidden)
					return true
				}
				access, refresh = "joined-google-access-"+uuid.NewString(), "joined-google-refresh-"+uuid.NewString()
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 3600, "scope": "fixture-provider-scope"})
				return true
			}
			if r.Host == "cloudcode-pa.googleapis.com" && r.URL.Path == "/v1internal:loadCodeAssist" {
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+access {
					w.WriteHeader(http.StatusForbidden)
					return true
				}
				projects.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": "auto-assigned-project", "currentTier": map[string]string{"id": "free-tier"}})
				return true
			}
			return false
		}
		peer.InferenceAuth = func(r *http.Request) bool {
			lock.Lock()
			defer lock.Unlock()
			if r.Method != http.MethodPost || "https://"+r.Host+r.URL.RequestURI() != endpoint || r.Header.Get("Cookie") != "" || r.Header.Get("X-Codex-Turn-State") != "" || r.Header.Get("x-api-key") != "" {
				return false
			}
			if method == "api_key" || method == "vertex_api_key" {
				return r.Header.Get("Authorization") == "" && r.Header.Get("x-goog-api-key") == secret
			}
			return r.Header.Get("Authorization") == "Bearer "+access && r.Header.Get("x-goog-api-key") == ""
		}
		peer.Counters = func() map[string]int64 {
			return map[string]int64{"tokenExchanges": tokens.Load(), "projectReads": projects.Load(), "browserAuthorizations": authorizations.Load()}
		}
		peer.Verify = func() {
			if method == "api_key" || method == "vertex_api_key" {
				require.Zero(t, tokens.Load(), "API key cannot enter OAuth")
			} else {
				require.Positive(t, tokens.Load(), "native OAuth/signing must reach the fixed token peer")
			}
			if method == "browser" {
				require.Positive(t, authorizations.Load())
				require.GreaterOrEqual(t, tokens.Load(), authorizations.Load())
			}
		}
		family.Peers = append(family.Peers, peer)
	}
	family.Compose = func(b nativeConnectionJoinedBuild) ([]service.GatewayNativeConnectionMethodAdapter, map[string]service.GatewayNativeConnectionDispatchAdapter, error) {
		dispatchers, err := service.NewGatewayNativeGeminiDispatchers(b.Gateway, b.Transport)
		if err != nil {
			return nil, nil, err
		}
		anti, err := service.NewGatewayNativeAntigravityDispatchers(b.Gateway)
		if err != nil {
			return nil, nil, err
		}
		for family, adapter := range anti {
			dispatchers[family] = adapter
		}
		nativeConfig := &config.Config{}
		nativeConfig.Gemini.OAuth.ClientID, nativeConfig.Gemini.OAuth.ClientSecret = cfg.GeminiOAuth.ClientID, cfg.GeminiOAuth.ClientSecret
		b.OAuth.gemini = service.NewGeminiOAuthService(nil, repository.NewGatewayNativeGeminiOAuthClient(nativeConfig, b.Transport), nil, nil, nativeConfig)
		t.Cleanup(b.OAuth.gemini.Stop)
		methods := []service.GatewayNativeConnectionMethodAdapter{}
		seen := map[string]bool{}
		mounts := map[string]gatewaytransport.QualifiedProfile{}
		for _, p := range family.Profiles {
			mounts[p.Profile] = p
		}
		for _, peer := range family.Peers {
			row := peer.Row
			if seen[row.ProfileID+"/"+row.MethodID] {
				continue
			}
			seen[row.ProfileID+"/"+row.MethodID] = true
			p := mounts[row.ProfileID]
			generation, err := connectionConfigurationGeneration(*b.Config, p)
			if err != nil {
				return nil, nil, err
			}
			mount := service.GatewayConnectionMount{BaseURL: p.BaseURL, Model: p.Model, QualificationRef: p.QualificationRef}
			var adapter service.GatewayNativeConnectionMethodAdapter
			if strings.HasPrefix(row.MethodID, "vertex_") {
				adapter, err = service.NewGatewayNativeCloudConnectionMethod(p.Profile, row.MethodID, generation, mount)
			} else if strings.HasPrefix(p.Profile, "antigravity-") {
				adapter, err = service.NewGatewayNativeAntigravityConnectionMethod(p.Profile, row.MethodID, generation, mount, b.Transport)
			} else {
				adapter, err = service.NewGatewayNativeGeminiConnectionMethod(p.Profile, generation, mount, b.OAuth.gemini)
			}
			if err != nil {
				return nil, nil, err
			}
			methods = append(methods, adapter)
		}
		return methods, dispatchers, nil
	}
	return family
}
