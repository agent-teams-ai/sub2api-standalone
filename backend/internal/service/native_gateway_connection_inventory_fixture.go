//go:build gatewayextension

package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// This provider exists only in the explicit native qualification build. It is
// deliberately absent from production consumers and from the 53-row G0 oracle.
const (
	GatewayNativeExtensionFixtureProfile  = "fixture-native-extension-responses-v1"
	GatewayNativeExtensionFixtureProvider = "fixture-native-extension"
	GatewayNativeExtensionFixtureMethod   = "fixture_browser_v1"
	GatewayNativeExtensionFixtureModel    = "fixture-review-model"
	GatewayNativeExtensionFixtureBaseURL  = "https://native-extension.example"
	GatewayNativeExtensionFixtureRedirect = "https://native-extension.invalid/callback"
	GatewayNativeExtensionFixtureClient   = "native-extension-qualified-client"
)

func gatewayNativeConnectionExtensionInventory() []GatewayConnectionProfileDefinition {
	method := gatewayConnectionBrowser(GatewayNativeExtensionFixtureMethod, "Synthetic browser grant", "manual", "callback_url")
	method.AllowedCredentialPolicies = []string{"provider_key"}
	profile := gatewayConnectionProfile(GatewayNativeExtensionFixtureProfile, PlatformOpenAI,
		GatewayNativeExtensionFixtureProvider, "Native extension fixture", "synthetic", "Qualification only", "openai_responses", method)
	profile.LimitsContract = GatewayAccountLimitsContractV3
	return []GatewayConnectionProfileDefinition{profile}
}

// The fixture uses the production prepared/entered journal and sealed custody.
// Only its new provider's browser grant parser is test-specific. An actual
// fixed TLS endpoint must consume the original code and PKCE verifier once.
func NewGatewayNativeExtensionFixtureMethod(generation string, mount GatewayConnectionMount, transport http.RoundTripper) (GatewayNativeConnectionMethodAdapter, error) {
	empty := GatewayNativeConnectionMethodAdapter{}
	if mount.BaseURL != GatewayNativeExtensionFixtureBaseURL || mount.Model != GatewayNativeExtensionFixtureModel || transport == nil || !GatewayConnectionMountValid(mount) {
		return empty, ErrGatewayNativeIdentity
	}
	client, err := newGatewayNativeCloudTokenClient(transport)
	if err != nil {
		return empty, err
	}
	profile := gatewayNativeConnectionExtensionInventory()[0]
	method := profile.Methods[0]
	selection := GatewayConnectionSelection{ProfileID: profile.ProfileID, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation}
	if !GatewayConnectionSelectionValid(selection) {
		return empty, ErrGatewayNativeIdentity
	}
	attestation := GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"}
	type material struct{ State, Verifier string }
	return GatewayNativeConnectionMethodAdapter{
		ProfileID: profile.ProfileID, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation,
		DispatchFamily: profile.DispatchFamily, AccountType: AccountTypeAPIKey, Mount: mount,
		Prepare: func(fields map[string]string) (GatewayNativeConnectionPrepared, error) {
			if len(fields) != 0 {
				return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
			}
			state, verifier := make([]byte, 32), make([]byte, 32)
			if _, err := rand.Read(state); err != nil {
				return GatewayNativeConnectionPrepared{}, err
			}
			if _, err := rand.Read(verifier); err != nil {
				return GatewayNativeConnectionPrepared{}, err
			}
			saved := material{State: hex.EncodeToString(state), Verifier: base64.RawURLEncoding.EncodeToString(verifier)}
			clear(state)
			clear(verifier)
			challenge := sha256.Sum256([]byte(saved.Verifier))
			query := url.Values{"response_type": {"code"}, "client_id": {GatewayNativeExtensionFixtureClient}, "redirect_uri": {GatewayNativeExtensionFixtureRedirect},
				"state": {saved.State}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
			raw, err := json.Marshal(saved)
			if err != nil {
				return GatewayNativeConnectionPrepared{}, err
			}
			return GatewayNativeConnectionPrepared{Material: raw, Credential: attestation, AuthFamily: "provider_api_key", AuthorizeURL: GatewayNativeExtensionFixtureBaseURL + "/authorize?" + query.Encode()}, nil
		},
		Complete: func(ctx context.Context, scope GatewayNativeConnectionScope, raw json.RawMessage, fields map[string]string) (GatewayNativeConnectionCredential, error) {
			var saved material
			if ctx == nil || ctx.Err() != nil || scope.Connection != selection || !GatewayConnectionScopeAuthorized(ctx, scope.Scope) || len(fields) != 1 || json.Unmarshal(raw, &saved) != nil {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			state, code, err := gatewayConnectionCallback(fields["completion"], GatewayNativeExtensionFixtureRedirect)
			if err != nil || state != saved.State || len(saved.Verifier) != 43 {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {GatewayNativeExtensionFixtureClient}, "redirect_uri": {GatewayNativeExtensionFixtureRedirect}, "code": {code}, "code_verifier": {saved.Verifier}}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, GatewayNativeExtensionFixtureBaseURL+"/token", strings.NewReader(form.Encode()))
			if err != nil {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.GetBody = nil
			response, err := client.Do(request)
			if response != nil && response.Body != nil {
				defer func() { _ = response.Body.Close() }()
			}
			if err != nil || response == nil || response.Body == nil || response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL.String() != GatewayNativeExtensionFixtureBaseURL+"/token" {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
			defer clear(body)
			if err != nil || len(body) > 4096 {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			object, err := gatewayOAuthJSON(body)
			key, valid := gatewayOAuthString(object, "api_key")
			if err != nil || len(object) != 1 || !valid || !GatewayNativeIngressKeyValid(key) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			document, err := json.Marshal(map[string]string{"api_key": key, "base_url": GatewayNativeExtensionFixtureBaseURL})
			if err != nil {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			return GatewayNativeConnectionCredential{Document: document, Credential: attestation, AuthFamily: "provider_api_key"}, nil
		},
	}, nil
}

type gatewayExtensionQuotaKey struct{}
type gatewayExtensionQuota struct{ header http.Header }
type gatewayExtensionTransport struct{ next http.RoundTripper }

// Capture the real source response headers in the exact admitted request's
// context. It neither creates a response nor changes request/response bytes.
func GatewayNativeExtensionFixtureTransport(next http.RoundTripper) http.RoundTripper {
	return &gatewayExtensionTransport{next: next}
}
func (t *gatewayExtensionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err == nil && response != nil && response.StatusCode == http.StatusOK && request.URL.String() == GatewayNativeExtensionFixtureBaseURL+"/v1/responses" {
		if observed, ok := request.Context().Value(gatewayExtensionQuotaKey{}).(*gatewayExtensionQuota); ok {
			observed.header = response.Header.Clone()
		}
	}
	return response, err
}

// A new native provider may project its own observed rate headers through the
// existing versioned cache. All dispatch/custody/body validation is unchanged.
func NewGatewayNativeExtensionFixtureDispatcher(gateway *OpenAIGatewayService) (GatewayNativeConnectionDispatchAdapter, error) {
	adapters, err := NewGatewayNativeAPIKeyDispatchers(gateway)
	if err != nil {
		return GatewayNativeConnectionDispatchAdapter{}, err
	}
	base := adapters["openai_responses"]
	return GatewayNativeConnectionDispatchAdapter{ValidateRequest: base.ValidateRequest,
		Forward: func(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
			observed := &gatewayExtensionQuota{}
			result, err := base.Forward(context.WithValue(ctx, gatewayExtensionQuotaKey{}, observed), c, account, route, body)
			if err != nil || route.Profile != GatewayNativeExtensionFixtureProfile {
				return result, err
			}
			state, _ := ctx.Value(gatewayConnectionDispatchStateKey{}).(*gatewayConnectionDispatchState)
			if state == nil || state.account != account || !state.entered.Load() {
				return result, ErrGatewayNativeIdentity
			}
			repository, ok := state.binding.service.repository.(GatewayNativeConnectionQuotaRepository)
			if !ok {
				return result, ErrGatewayNativeIdentity
			}
			limit, e1 := strconv.ParseUint(observed.header.Get("X-Fixture-Rate-Limit"), 10, 32)
			remaining, e2 := strconv.ParseUint(observed.header.Get("X-Fixture-Rate-Remaining"), 10, 32)
			reset, e3 := time.Parse(time.RFC3339, observed.header.Get("X-Fixture-Rate-Reset"))
			if e1 != nil || e2 != nil || e3 != nil || limit == 0 || remaining > limit {
				return result, ErrGatewayNativeIdentity
			}
			now := time.Now().UTC()
			at, resetAt := now.Format(time.RFC3339Nano), reset.UTC().Format(time.RFC3339)
			used, duration := float64(limit-remaining)*100/float64(limit), int64(60)
			snapshot := GatewayAccountLimitsSnapshotV2{Scope: "account", Windows: []GatewayAccountLimitWindowV2{{ID: "request_rate", Provider: GatewayNativeExtensionFixtureProvider, Dimension: "rate_limit", Unit: "requests",
				Period: GatewayAccountLimitPeriodV2{Kind: "duration", DurationSeconds: &duration}, UsedPercent: &used, ObservedAt: &at, ResetAt: &resetAt}}}
			bounded, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer stop()
			if err := repository.StoreGatewayNativeConnectionQuota(bounded, state.physical, GatewayProviderQuotaObservation{Limits: snapshot, ObservedAt: now}, 0); err != nil {
				return result, err
			}
			return result, nil
		},
	}, nil
}
