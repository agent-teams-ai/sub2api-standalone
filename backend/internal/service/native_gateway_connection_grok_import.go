package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

type gatewayGrokImportMaterial struct {
	Connection  GatewayConnectionSelection `json:"connection"`
	ClientID    string                     `json:"client_id"`
	RedirectURI string                     `json:"redirect_uri"`
	Secret      string                     `json:"secret"`
}

type gatewayGrokImportCredential struct {
	gatewayGrokBundle
	RedirectURI string `json:"redirect_uri"`
}

// A refresh-token exchange uses the deployment-qualified client. The native SSO
// converter has its own fixed public client, so that method is mounted only for
// an explicitly qualified matching client, never an implicit CLI fallback.
func NewGatewayNativeGrokImportMethods(generation string, mount GatewayConnectionMount, qualification GatewayNativeGrokOAuthQualification, transport http.RoundTripper) ([]GatewayNativeConnectionMethodAdapter, error) {
	if !qualification.valid() || !GatewayConnectionMountValid(mount) || mount.BaseURL != GatewayGrokOAuthBaseURL || mount.Model != GatewayGrokOAuthModel {
		return nil, ErrGatewayNativeIdentity
	}
	profile, _ := LookupGatewayConnectionProfile(GatewayGrokOAuthResponsesProfile)
	client := gatewaySubscriptionImportClient(transport)
	attestation := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native"}
	var methods []GatewayNativeConnectionMethodAdapter
	for _, definition := range profile.Methods {
		if definition.MethodID != "refresh_token" && definition.MethodID != "sso" || definition.MethodID == "sso" && qualification.ClientID != xai.DefaultClientID {
			continue
		}
		selection := GatewayConnectionSelection{profile.ProfileID, definition.MethodID, definition.Revision, generation}
		if !GatewayConnectionSelectionValid(selection) {
			return nil, ErrGatewayNativeIdentity
		}
		finish := func(ctx context.Context, token *xai.TokenResponse, observed time.Time, previous *gatewayGrokImportCredential) (GatewayNativeConnectionCredential, error) {
			principal, nonce, err := gatewayGrokImportPrincipal(ctx, client, token, qualification.ClientID)
			if err != nil || previous != nil && (principal.Subject != previous.Subject || principal.Account != previous.Team) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
			}
			bundle := gatewayGrokBundle{Connection: &selection, Kind: "grok-oauth/v1", Revision: 1, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
				Subject: principal.Subject, Team: principal.Account, ExpiresAt: observed.Add(time.Duration(token.ExpiresIn) * time.Second), ObservedAt: observed, Nonce: nonce, ClientID: qualification.ClientID}
			if previous != nil {
				bundle.Revision = previous.Revision + 1
			}
			if !bundle.ExpiresAt.After(time.Now()) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
			}
			document, err := json.Marshal(gatewayGrokImportCredential{bundle, qualification.RedirectURI})
			if err != nil || len(document) > GatewayConnectionInputBytes {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
			}
			fields := GatewayAccountInfoEmptyFields(profile.ProfileID)
			if gatewayInfoText(principal.Email, 320) {
				fields.Email = gatewayKnownFact(principal.Email, "provider_api", observed)
			}
			if gatewayInfoText(principal.Name, 200) {
				fields.AccountLabel = gatewayKnownFact(principal.Name, "provider_api", observed)
			}
			return GatewayNativeConnectionCredential{Document: document, Credential: attestation, AuthFamily: "grok_oauth", Observations: []GatewayAccountInfoObservation{
				{Source: "account_check", Fields: fields}, {Source: "credential", Fields: gatewayOAuthCredentialExpiryInfo(profile.ProfileID, bundle.ExpiresAt, observed)},
			}}, nil
		}
		methods = append(methods, GatewayNativeConnectionMethodAdapter{
			ProfileID: selection.ProfileID, MethodID: selection.MethodID, MethodRevision: selection.MethodRevision, ConfigurationGeneration: generation,
			DispatchFamily: profile.DispatchFamily, AccountType: AccountTypeOAuth, Mount: mount,
			Prepare: func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
				if ValidateGatewayConnectionInput(definition.Interaction.InputFields, input) != nil {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				secret := input["refresh_token"]
				if definition.MethodID == "sso" {
					// Native SSO accepts a cookie header as well as a bare token.
					// Validate the original text before normalization can strip control
					// bytes, then apply the token grammar to the extracted identity.
					if !gatewayConnectionTextValid(input["sso_token"], 32768, false) {
						return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
					}
					secret = xai.NormalizeSSOToken(input["sso_token"])
					if (&http.Cookie{Name: "sso", Value: secret}).Valid() != nil {
						return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
					}
				}
				if !grokToken(secret) {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				material, err := json.Marshal(gatewayGrokImportMaterial{selection, qualification.ClientID, qualification.RedirectURI, secret})
				if err != nil || len(material) > GatewayConnectionInputBytes {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				return GatewayNativeConnectionPrepared{Material: material, EntryClaim: []byte(secret), Credential: attestation, AuthFamily: "grok_oauth"}, nil
			},
			Complete: func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
				var material gatewayGrokImportMaterial
				if !gatewayImportAuthorized(ctx, in, selection) || len(completion) != 0 || json.Unmarshal(raw, &material) != nil || material.Connection != selection || material.ClientID != qualification.ClientID || material.RedirectURI != qualification.RedirectURI || !grokToken(material.Secret) {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
				}
				if _, err := gatewayOAuthJSON(raw); err != nil {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
				}
				var token *xai.TokenResponse
				var observed time.Time
				var err error
				if definition.MethodID == "sso" {
					if (&http.Cookie{Name: "sso", Value: material.Secret}).Valid() != nil {
						return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
					}
					bounded, cancel := context.WithTimeout(ctx, xai.SSOConversionTimeout)
					defer cancel()
					strict := &gatewayGrokSSOImportClient{client: client}
					defer func() { clear(strict.raw) }()
					_, err = xai.ConvertSSOToBuild(bounded, material.Secret, &xai.SSODeviceOptions{HTTPClient: strict})
					token, observed = strict.token, strict.observed
				} else {
					token, observed, err = gatewayGrokImportExchange(ctx, client, material.ClientID, material.Secret)
				}
				if err != nil || token == nil {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
				}
				return finish(ctx, token, observed, nil)
			},
			RefreshNeeded: func(raw json.RawMessage, now time.Time) (bool, error) {
				credential, err := gatewayGrokImportDecode(raw, selection, qualification)
				return err == nil && !credential.ExpiresAt.After(now.Add(time.Minute)), err
			},
			Refresh: func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage) (GatewayNativeConnectionCredential, error) {
				credential, err := gatewayGrokImportDecode(raw, selection, qualification)
				if err != nil || !gatewayImportAuthorized(ctx, in, selection) {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
				}
				token, observed, err := gatewayGrokImportExchange(ctx, client, credential.ClientID, credential.RefreshToken)
				if err != nil {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
				}
				return finish(ctx, token, observed, &credential)
			},
		})
	}
	return methods, nil
}

func gatewayGrokImportToken(raw []byte, previousRefresh string) (*xai.TokenResponse, error) {
	var token xai.TokenResponse
	if _, err := gatewayOAuthJSON(raw); err != nil || json.Unmarshal(raw, &token) != nil || !grokToken(token.AccessToken) || !strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn <= 0 || token.ExpiresIn > 90*24*60*60 {
		return nil, ErrGatewayNativeIdentity
	}
	if token.RefreshToken == "" {
		token.RefreshToken = previousRefresh
	}
	if !grokToken(token.RefreshToken) || token.IDToken != "" && !grokToken(token.IDToken) {
		return nil, ErrGatewayNativeIdentity
	}
	if token.Scope != "" {
		found := false
		for _, scope := range strings.Fields(token.Scope) {
			if scope == "grok-cli:access" {
				found = true
			}
		}
		if !found {
			return nil, ErrGatewayNativeIdentity
		}
	}
	return &token, nil
}

func gatewayGrokImportExchange(ctx context.Context, client *http.Client, clientID, refresh string) (*xai.TokenResponse, time.Time, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, xai.DefaultTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, time.Time{}, ErrGatewayNativeIdentity
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", xai.ProtectedUserAgent)
	request.GetBody = nil
	observed := time.Now().UTC()
	response, err := client.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return nil, observed, ErrGatewayNativeIdentity
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	defer clear(raw)
	if err != nil || len(raw) > 65536 || response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL.String() != xai.DefaultTokenURL {
		return nil, observed, ErrGatewayNativeIdentity
	}
	token, err := gatewayGrokImportToken(raw, refresh)
	return token, observed, err
}

func gatewayGrokImportPrincipal(ctx context.Context, client *http.Client, token *xai.TokenResponse, audience string) (xai.ProtectedPrincipal, string, error) {
	if token == nil {
		return xai.ProtectedPrincipal{}, "", ErrGatewayNativeIdentity
	}
	if token.IDToken != "" {
		// An imported refresh/device grant has no new authorization nonce. Read
		// its optional nonce only to preserve the signed token's claim; issuer,
		// audience, signature, time and same-principal userinfo are still verified.
		parts := strings.Split(token.IDToken, ".")
		if len(parts) != 3 {
			return xai.ProtectedPrincipal{}, "", ErrGatewayNativeIdentity
		}
		claims, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
		if err != nil {
			return xai.ProtectedPrincipal{}, "", ErrGatewayNativeIdentity
		}
		defer clear(claims)
		fields, err := gatewayOAuthJSON(claims)
		var nonce string
		if err != nil || fields["nonce"] != nil && json.Unmarshal(fields["nonce"], &nonce) != nil {
			return xai.ProtectedPrincipal{}, "", ErrGatewayNativeIdentity
		}
		principal, err := xai.VerifyProtectedPrincipal(ctx, client, token.IDToken, token.AccessToken, audience, nonce, false)
		return principal, nonce, err
	}
	// Refresh grants can omit id_token. Identity then comes exclusively from
	// authenticated userinfo for the access token just returned by the fixed
	// issuer for this qualified client; refresh additionally checks continuity.
	var user struct {
		Subject string          `json:"sub"`
		Team    string          `json:"team_id"`
		Email   json.RawMessage `json:"email"`
		Name    json.RawMessage `json:"name"`
	}
	if xai.FetchProtectedJSON(ctx, client, xai.ProtectedUserinfoURL, token.AccessToken, &user) != nil || !grokPrincipal(user.Subject) || user.Team != "" && !grokPrincipal(user.Team) {
		return xai.ProtectedPrincipal{}, "", ErrGatewayNativeIdentity
	}
	principal := xai.ProtectedPrincipal{Subject: user.Subject, Account: "personal:" + user.Subject}
	if user.Team != "" {
		principal.Account = "team:" + user.Team
	}
	_ = json.Unmarshal(user.Email, &principal.Email)
	_ = json.Unmarshal(user.Name, &principal.Name)
	return principal, "", nil
}

func gatewayGrokImportDecode(raw json.RawMessage, selection GatewayConnectionSelection, qualification GatewayNativeGrokOAuthQualification) (gatewayGrokImportCredential, error) {
	var credential gatewayGrokImportCredential
	if _, err := gatewayOAuthJSON(raw); err != nil || json.Unmarshal(raw, &credential) != nil || !SameGatewayConnectionSelection(credential.Connection, &selection) || credential.Kind != "grok-oauth/v1" ||
		credential.ClientID != qualification.ClientID || credential.RedirectURI != qualification.RedirectURI || credential.Revision < 1 || !grokToken(credential.AccessToken) || !grokToken(credential.RefreshToken) ||
		!grokPrincipal(credential.Subject) || credential.Team != "personal:"+credential.Subject && (!strings.HasPrefix(credential.Team, "team:") || !grokPrincipal(strings.TrimPrefix(credential.Team, "team:"))) ||
		credential.ObservedAt.IsZero() || !credential.ExpiresAt.After(credential.ObservedAt) {
		return credential, ErrGatewayNativeIdentity
	}
	return credential, nil
}

// Reuse native's SSO/device algorithm while enforcing private transport policy
// and requiring the actual token type/expiry, before its legacy defaults run.
type gatewayGrokSSOImportClient struct {
	client   *http.Client
	token    *xai.TokenResponse
	observed time.Time
	raw      []byte
}

func (s *gatewayGrokSSOImportClient) Do(request *http.Request) (*http.Response, error) {
	if s == nil || s.client == nil || request == nil || request.URL == nil || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Port() != "" || request.URL.Fragment != "" ||
		request.URL.Opaque != "" || request.URL.RawPath != "" || request.RequestURI != "" || request.Host != "" && request.Host != request.URL.Host ||
		(request.URL.Host != "accounts.x.ai" && request.URL.Host != "auth.x.ai") || (request.Method != http.MethodGet && request.Method != http.MethodPost) {
		return nil, ErrGatewayNativeIdentity
	}
	if request.URL.Host == "accounts.x.ai" && (request.Method != http.MethodGet || request.URL.Path != "/" || request.URL.RawQuery != "") {
		return nil, ErrGatewayNativeIdentity
	}
	if request.Method == http.MethodPost {
		switch request.URL.String() {
		case xai.SSODeviceURL, xai.SSOVerifyURL, xai.SSOApproveURL, xai.SSOTokenURL:
		default:
			return nil, ErrGatewayNativeIdentity
		}
	}
	// Native follows only explicit, bounded device/consent redirects. Disable
	// automatic following on a private client copy so each hop must re-enter
	// the exact HTTPS-authority and method checks above; Host cannot override it.
	client := *s.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request.GetBody = nil
	observed := time.Now().UTC()
	response, err := client.Do(request) // #nosec G704 -- Exact HTTPS hosts, no alternate authority, fixed POST endpoints, and every redirect is checked above.
	if err != nil || response == nil || response.Body == nil {
		return nil, ErrGatewayNativeIdentity
	}
	if response.Request == nil || response.Request.URL.String() != request.URL.String() {
		_ = response.Body.Close()
		return nil, ErrGatewayNativeIdentity
	}
	if request.URL.String() != xai.SSOTokenURL || response.StatusCode != http.StatusOK {
		return response, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	_ = response.Body.Close()
	if err != nil || len(raw) > 65536 {
		clear(raw)
		return nil, ErrGatewayNativeIdentity
	}
	token, err := gatewayGrokImportToken(raw, "")
	if err != nil {
		clear(raw)
		return nil, err
	}
	clear(s.raw)
	s.raw, s.token, s.observed = raw, token, observed
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}
