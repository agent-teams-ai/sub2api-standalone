//go:build linux && (integration || gatewaycustody) && gatewayextension

package gatewaybootstrap

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeConnectionJoinedExtensionTransport(batch string, transport http.RoundTripper) http.RoundTripper {
	if batch == "extensibility" {
		return service.GatewayNativeExtensionFixtureTransport(transport)
	}
	return transport
}

// This separate source oracle is intentionally not a row in the G0 inventory.
// Native introduces a new provider/profile/method; consumer packages have no
// implementation, alias, or endpoint branch for any of these identifiers.
func newNativeConnectionJoinedExtensionFamily(t *testing.T, _ *Config) nativeConnectionJoinedFamily {
	t.Helper()
	const profile = service.GatewayNativeExtensionFixtureProfile
	const method = service.GatewayNativeExtensionFixtureMethod
	const variant = "same_consumer_build"
	mount := nativeConnectionJoinedProfile(profile, service.GatewayNativeExtensionFixtureBaseURL, service.GatewayNativeExtensionFixtureModel)
	secrets := []string{"extension-sentinel-" + uuid.NewString(), "extension-sentinel-" + uuid.NewString()}
	type grant struct {
		challenge, secret string
		consumed          bool
	}
	var mu sync.Mutex
	grants := map[string]*grant{}
	var authorizations, exchanges, rejected, dispatches atomic.Int64
	peer := &nativeConnectionJoinedPeer{Protocol: "responses", Row: nativeConnectionJoinedRow{
		ID: nativeConnectionJoinedRowID(profile, method, variant), ProfileID: profile, MethodID: method, Variant: variant,
		Fields: map[string]string{}, Completion: []nativeConnectionJoinedCompletion{{Action: "submit", FromAuthorizationURL: true}},
		ReconnectCompletion: []nativeConnectionJoinedCompletion{{Action: "submit", FromAuthorizationURL: true}},
		CredentialKind:      "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none",
		Provider: service.GatewayNativeExtensionFixtureProvider, Model: service.GatewayNativeExtensionFixtureModel, ExecutionFamily: "openai_responses", Execute: true,
	}}
	peer.ObserveAuthorizeURL = func(raw string) (map[string]string, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host != "native-extension.example" || u.Path != "/authorize" || u.User != nil || u.Fragment != "" || u.RawPath != "" {
			return nil, ErrDenied
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q) != 6 || q.Get("response_type") != "code" || q.Get("client_id") != service.GatewayNativeExtensionFixtureClient || q.Get("redirect_uri") != service.GatewayNativeExtensionFixtureRedirect || q.Get("code_challenge_method") != "S256" {
			return nil, ErrDenied
		}
		for _, values := range q {
			if len(values) != 1 {
				return nil, ErrDenied
			}
		}
		state, err := hex.DecodeString(q.Get("state"))
		if err != nil || len(state) != 32 || hex.EncodeToString(state) != q.Get("state") {
			return nil, ErrDenied
		}
		challenge, err := base64.RawURLEncoding.Strict().DecodeString(q.Get("code_challenge"))
		if err != nil || len(challenge) != 32 {
			return nil, ErrDenied
		}
		mu.Lock()
		defer mu.Unlock()
		index := int(authorizations.Load())
		if index >= len(secrets) {
			return nil, ErrDenied
		}
		code := "extension-code-" + uuid.NewString()
		grants[code] = &grant{challenge: q.Get("code_challenge"), secret: secrets[index]}
		authorizations.Add(1)
		callback := service.GatewayNativeExtensionFixtureRedirect + "?" + (url.Values{"state": {q.Get("state")}, "code": {code}}).Encode()
		return map[string]string{"completion": callback}, nil
	}
	peer.Handle = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Host != "native-extension.example" {
			return false
		}
		if r.URL.Path == "/v1/responses" {
			w.Header().Set("X-Fixture-Rate-Limit", "100")
			w.Header().Set("X-Fixture-Rate-Remaining", strconv.FormatInt(99-dispatches.Load(), 10))
			w.Header().Set("X-Fixture-Rate-Reset", time.Now().UTC().Add(time.Minute).Format(time.RFC3339))
			return false
		}
		if r.URL.Path != "/token" {
			return false
		}
		deny := func() { rejected.Add(1); w.WriteHeader(http.StatusForbidden) }
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			deny()
			return true
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil || len(r.PostForm) != 5 || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("client_id") != service.GatewayNativeExtensionFixtureClient || r.PostForm.Get("redirect_uri") != service.GatewayNativeExtensionFixtureRedirect {
			deny()
			return true
		}
		for _, values := range r.PostForm {
			if len(values) != 1 {
				deny()
				return true
			}
		}
		verifier, err := base64.RawURLEncoding.Strict().DecodeString(r.PostForm.Get("code_verifier"))
		if err != nil || len(verifier) != 32 {
			deny()
			return true
		}
		challenge := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		mu.Lock()
		defer mu.Unlock()
		g := grants[r.PostForm.Get("code")]
		if g == nil || g.consumed || base64.RawURLEncoding.EncodeToString(challenge[:]) != g.challenge {
			deny()
			return true
		}
		g.consumed = true
		exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"api_key": g.secret})
		return true
	}
	peer.InferenceAuth = func(r *http.Request) bool {
		if r.Method != http.MethodPost || "https://"+r.Host+r.URL.RequestURI() != service.GatewayNativeExtensionFixtureBaseURL+"/v1/responses" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Codex-Turn-State") != "" {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		for _, g := range grants {
			if g.consumed && r.Header.Get("Authorization") == "Bearer "+g.secret && r.Header.Get("x-api-key") == "" {
				dispatches.Add(1)
				return true
			}
		}
		return false
	}
	peer.Counters = func() map[string]int64 {
		return map[string]int64{"browserAuthorizations": authorizations.Load(), "tokenExchanges": exchanges.Load(), "rejectedGrantReplays": rejected.Load(), "inferences": dispatches.Load()}
	}
	peer.Verify = func() {
		require.EqualValues(t, 2, authorizations.Load())
		require.EqualValues(t, 2, exchanges.Load())
		require.Zero(t, rejected.Load())
		require.GreaterOrEqual(t, dispatches.Load(), int64(2))
	}
	family := nativeConnectionJoinedFamily{Profiles: []gatewaytransport.QualifiedProfile{mount}, Peers: []*nativeConnectionJoinedPeer{peer}, Hosts: []string{"native-extension.example:443"},
		Private: map[string]string{"extensionSentinelFirst": secrets[0], "extensionSentinelReplacement": secrets[1]}}
	family.Compose = func(b nativeConnectionJoinedBuild) ([]service.GatewayNativeConnectionMethodAdapter, map[string]service.GatewayNativeConnectionDispatchAdapter, error) {
		generation, err := connectionConfigurationGeneration(*b.Config, mount)
		if err != nil {
			return nil, nil, err
		}
		adapter, err := service.NewGatewayNativeExtensionFixtureMethod(generation, service.GatewayConnectionMount{BaseURL: mount.BaseURL, Model: mount.Model, QualificationRef: mount.QualificationRef}, b.Transport)
		if err != nil {
			return nil, nil, err
		}
		dispatch, err := service.NewGatewayNativeExtensionFixtureDispatcher(b.Gateway)
		if err != nil {
			return nil, nil, err
		}
		return []service.GatewayNativeConnectionMethodAdapter{adapter}, map[string]service.GatewayNativeConnectionDispatchAdapter{"openai_responses": dispatch}, nil
	}
	return family
}
