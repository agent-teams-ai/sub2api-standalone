package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

type gatewayClaudeImportMaterial struct {
	Connection  GatewayConnectionSelection `json:"connection"`
	ClientID    string                     `json:"client_id"`
	RedirectURI string                     `json:"redirect_uri"`
	State       string                     `json:"state"`
	Verifier    string                     `json:"verifier"`
	Session     string                     `json:"session_key,omitempty"`
}

type gatewayClaudeImportCredential struct {
	gatewayClaudeBundle
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
}

func gatewaySubscriptionImportClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, MaxConnsPerHost: 2, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 8192}
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func gatewayImportAuthorized(ctx context.Context, in GatewayNativeConnectionScope, selection GatewayConnectionSelection) bool {
	return ctx != nil && ctx.Err() == nil && in.Connection == selection && GatewayNativeCredentialRefValid(in.Operation) &&
		GatewayConnectionScopeAuthorized(ctx, in.Scope) && GatewayConnectionSelectionAuthorized(ctx, &selection)
}

// These methods use the native Claude cookie and setup-token grants. The common
// journal seals PKCE/session material and owns every provider-effect entry.
func NewGatewayNativeClaudeImportMethods(generation string, mount GatewayConnectionMount, transport http.RoundTripper) ([]GatewayNativeConnectionMethodAdapter, error) {
	if !GatewayConnectionMountValid(mount) || mount.BaseURL != GatewayClaudeOAuthBaseURL || mount.Model != GatewayClaudeOAuthModel {
		return nil, ErrGatewayNativeIdentity
	}
	profile, _ := LookupGatewayConnectionProfile(GatewayClaudeOAuthMessagesProfile)
	client := gatewaySubscriptionImportClient(transport)
	exchange := &GatewayNativeClaudeOAuth{client: client}
	var methods []GatewayNativeConnectionMethodAdapter
	for _, definition := range profile.Methods {
		if definition.MethodID != "cookie_oauth" && definition.MethodID != "setup_token_code" && definition.MethodID != "setup_token_cookie" {
			continue
		}
		selection := GatewayConnectionSelection{profile.ProfileID, definition.MethodID, definition.Revision, generation}
		if !GatewayConnectionSelectionValid(selection) {
			return nil, ErrGatewayNativeIdentity
		}
		setup := definition.MethodID != "cookie_oauth"
		family, accountType := "claude_oauth", AccountTypeOAuth
		if setup {
			accountType = AccountTypeSetupToken
		}
		finish := func(bundle gatewayClaudeBundle) (GatewayNativeConnectionCredential, error) {
			bundle.Connection = &selection
			document, err := json.Marshal(gatewayClaudeImportCredential{bundle, oauth.ClientID, oauth.RedirectURI})
			if err != nil || len(document) > GatewayConnectionInputBytes || !bundle.ExpiresAt.After(time.Now()) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
			}
			attestation := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "none"}
			if bundle.RefreshToken != "" {
				attestation.RefreshPolicy = "native"
			}
			return GatewayNativeConnectionCredential{Document: document, Credential: attestation, AuthFamily: family, Observations: bundle.info}, nil
		}
		methods = append(methods, GatewayNativeConnectionMethodAdapter{
			ProfileID: selection.ProfileID, MethodID: selection.MethodID, MethodRevision: selection.MethodRevision, ConfigurationGeneration: generation,
			DispatchFamily: profile.DispatchFamily, AccountType: accountType, Mount: mount,
			Prepare: func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
				if ValidateGatewayConnectionInput(definition.Interaction.InputFields, input) != nil {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				session := input["session_key"]
				if definition.MethodID != "setup_token_code" && (!claudeToken(session) || (&http.Cookie{Name: "sessionKey", Value: session}).Valid() != nil) {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				state, err := oauth.GenerateState()
				if err != nil {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				verifier, err := oauth.GenerateCodeVerifier()
				if err != nil {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				material, err := json.Marshal(gatewayClaudeImportMaterial{selection, oauth.ClientID, oauth.RedirectURI, state, verifier, session})
				if err != nil || len(material) > GatewayConnectionInputBytes {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				prepared := GatewayNativeConnectionPrepared{Material: material, Credential: GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "none"}, AuthFamily: family}
				if session != "" {
					prepared.EntryClaim = []byte(session)
				}
				if !setup {
					prepared.Credential.RefreshPolicy = "native"
				}
				if definition.MethodID == "setup_token_code" {
					prepared.AuthorizeURL = oauth.BuildAuthorizationURL(state, oauth.GenerateCodeChallenge(verifier), oauth.ScopeInference)
				}
				return prepared, nil
			},
			Complete: func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
				var material gatewayClaudeImportMaterial
				if !gatewayImportAuthorized(ctx, in, selection) || json.Unmarshal(raw, &material) != nil || material.Connection != selection || material.ClientID != oauth.ClientID || material.RedirectURI != oauth.RedirectURI || !gatewayImportPKCE(material.State) || !gatewayImportPKCE(material.Verifier) {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
				}
				if _, err := gatewayOAuthJSON(raw); err != nil {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
				}
				var code, organization string
				if definition.MethodID == "setup_token_code" {
					if len(completion) != 1 || material.Session != "" {
						return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
					}
					var valid bool
					code, valid = gatewayClaudeImportCode(completion["completion"], material.State)
					if !valid {
						return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
					}
				} else {
					if len(completion) != 0 || !claudeToken(material.Session) || (&http.Cookie{Name: "sessionKey", Value: material.Session}).Valid() != nil {
						return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
					}
					scope := oauth.ScopeAPI
					if setup {
						scope = oauth.ScopeInference
					}
					var err error
					code, organization, err = gatewayClaudeCookieCode(ctx, client, material, scope)
					if err != nil {
						return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
					}
				}
				bundle, err := exchange.exchangeWithPolicy(ctx, map[string]any{"grant_type": "authorization_code", "client_id": material.ClientID, "redirect_uri": material.RedirectURI, "state": material.State, "code_verifier": material.Verifier, "code": code}, nil, setup)
				if err != nil || organization != "" && bundle.Organization != organization {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
				}
				return finish(bundle)
			},
			RefreshNeeded: func(raw json.RawMessage, now time.Time) (bool, error) {
				credential, err := gatewayClaudeImportDecode(raw, selection)
				return err == nil && credential.RefreshToken != "" && !credential.ExpiresAt.After(now.Add(time.Minute)), err
			},
			Refresh: func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage) (GatewayNativeConnectionCredential, error) {
				credential, err := gatewayClaudeImportDecode(raw, selection)
				if err != nil || !gatewayImportAuthorized(ctx, in, selection) || credential.RefreshToken == "" {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
				}
				bundle, err := exchange.exchangeWithPolicy(ctx, map[string]any{"grant_type": "refresh_token", "client_id": credential.ClientID, "refresh_token": credential.RefreshToken}, &credential.gatewayClaudeBundle, setup)
				if err != nil || bundle.RefreshToken == "" {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
				}
				return finish(bundle)
			},
		})
	}
	return methods, nil
}

func gatewayImportPKCE(value string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32 && len(value) == 43
}

func gatewayClaudeImportCode(value, state string) (string, bool) {
	code, actual, found := strings.Cut(value, "#")
	return code, found && actual == state && !strings.Contains(code, "#") && connectCodeValid(code)
}

func gatewayClaudeImportDecode(raw json.RawMessage, selection GatewayConnectionSelection) (gatewayClaudeImportCredential, error) {
	var credential gatewayClaudeImportCredential
	if _, err := gatewayOAuthJSON(raw); err != nil || json.Unmarshal(raw, &credential) != nil || !SameGatewayConnectionSelection(credential.Connection, &selection) ||
		credential.Kind != "claude-oauth/v1" || credential.ClientID != oauth.ClientID || credential.RedirectURI != oauth.RedirectURI || credential.Revision < 1 ||
		!claudeToken(credential.AccessToken) || credential.RefreshToken != "" && !claudeToken(credential.RefreshToken) || !claudeUUID(credential.Account) || !claudeUUID(credential.Organization) ||
		credential.ExpiresAt.IsZero() || credential.ObservedAt.IsZero() || !credential.ExpiresAt.After(credential.ObservedAt) {
		return credential, ErrGatewayNativeIdentity
	}
	return credential, nil
}

func gatewayClaudeCookieJSON(ctx context.Context, client *http.Client, method, endpoint, session string, payload any) ([]byte, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, ErrGatewayNativeIdentity
		}
	}
	defer clear(body)
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	request.AddCookie(&http.Cookie{Name: "sessionKey", Value: session})
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://claude.ai")
	request.Header.Set("Referer", "https://claude.ai/new")
	request.GetBody = nil
	response, err := client.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return nil, ErrGatewayNativeIdentity
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 || response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL.String() != endpoint {
		clear(raw)
		return nil, ErrGatewayNativeIdentity
	}
	return raw, nil
}

func gatewayClaudeCookieCode(ctx context.Context, client *http.Client, material gatewayClaudeImportMaterial, scope string) (string, string, error) {
	raw, err := gatewayClaudeCookieJSON(ctx, client, http.MethodGet, "https://claude.ai/api/organizations", material.Session, nil)
	if err != nil {
		return "", "", err
	}
	defer clear(raw)
	var organizations []json.RawMessage
	if json.Unmarshal(raw, &organizations) != nil || len(organizations) == 0 || len(organizations) > 128 {
		return "", "", ErrGatewayNativeIdentity
	}
	selected, teamSelected := "", false
	for _, item := range organizations {
		if _, err := gatewayOAuthJSON(item); err != nil {
			return "", "", ErrGatewayNativeIdentity
		}
		var organization struct {
			UUID      string  `json:"uuid"`
			RavenType *string `json:"raven_type"`
		}
		if json.Unmarshal(item, &organization) != nil || !claudeUUID(organization.UUID) {
			return "", "", ErrGatewayNativeIdentity
		}
		if selected == "" {
			selected = organization.UUID
		}
		if !teamSelected && organization.RavenType != nil && *organization.RavenType == "team" {
			selected, teamSelected = organization.UUID, true
		}
	}
	response, err := gatewayClaudeCookieJSON(ctx, client, http.MethodPost, "https://claude.ai/v1/oauth/"+selected+"/authorize", material.Session,
		map[string]any{"response_type": "code", "client_id": material.ClientID, "organization_uuid": selected, "redirect_uri": material.RedirectURI, "scope": scope, "state": material.State, "code_challenge": oauth.GenerateCodeChallenge(material.Verifier), "code_challenge_method": "S256"})
	if err != nil {
		return "", "", err
	}
	defer clear(response)
	fields, err := gatewayOAuthJSON(response)
	redirect, valid := gatewayOAuthString(fields, "redirect_uri")
	if err != nil || !valid {
		return "", "", ErrGatewayNativeIdentity
	}
	u, err := url.Parse(redirect)
	expected, _ := url.Parse(material.RedirectURI)
	if err != nil || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
		return "", "", ErrGatewayNativeIdentity
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 2 || len(query["state"]) != 1 || query.Get("state") != material.State || len(query["code"]) != 1 || !connectCodeValid(query.Get("code")) {
		return "", "", ErrGatewayNativeIdentity
	}
	return query.Get("code"), selected, nil
}
