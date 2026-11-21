package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
)

type gatewayGeminiOAuthMaterial struct {
	State       string                `json:"state"`
	Verifier    string                `json:"verifier"`
	RedirectURI string                `json:"redirect_uri"`
	ProjectID   string                `json:"project_id"`
	TierID      string                `json:"tier_id"`
	OAuthType   string                `json:"oauth_type"`
	Client      geminicli.OAuthConfig `json:"client"`
}

func gatewayGeminiOAuthType(profile string) string {
	switch profile {
	case "gemini-google-one-responses-oauth-v1":
		return "google_one"
	case "gemini-code-assist-responses-oauth-v1":
		return "code_assist"
	case "gemini-ai-studio-responses-oauth-v1":
		return "ai_studio"
	}
	return ""
}

// Gemini CLI and Antigravity generate a canonical base64url 32-byte state.
// Keep that native grammar; the legacy Codex callback requires hexadecimal
// state and cannot validate these provider-owned browser continuations.
func gatewayGeminiConnectionCallback(input, expectedRedirect string) (string, string, error) {
	if len(input) == 0 || len(input) > 8192 || strings.TrimSpace(input) != input {
		return "", "", ErrGatewayNativeIdentity
	}
	u, err := url.Parse(input)
	expected, expectedErr := url.Parse(expectedRedirect)
	if err != nil || expectedErr != nil || u.User != nil || u.Fragment != "" || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.RawPath != "" || u.Opaque != "" {
		return "", "", ErrGatewayNativeIdentity
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 2 || len(query["state"]) != 1 || len(query["code"]) != 1 || !connectCodeValid(query.Get("code")) {
		return "", "", ErrGatewayNativeIdentity
	}
	state := query.Get("state")
	random, err := base64.RawURLEncoding.Strict().DecodeString(state)
	if err != nil || len(random) != 32 || base64.RawURLEncoding.EncodeToString(random) != state {
		return "", "", ErrGatewayNativeIdentity
	}
	return state, query.Get("code"), nil
}

func gatewayGeminiOAuthConfig(s *GeminiOAuthService, oauthType string) (geminicli.OAuthConfig, error) {
	if s == nil || s.cfg == nil || s.oauthClient == nil || oauthType == "" {
		return geminicli.OAuthConfig{}, ErrGatewayNativeIdentity
	}
	input := geminicli.OAuthConfig{ClientID: s.cfg.Gemini.OAuth.ClientID, ClientSecret: s.cfg.Gemini.OAuth.ClientSecret, Scopes: s.cfg.Gemini.OAuth.Scopes}
	if oauthType == "ai_studio" {
		if !s.GetOAuthConfig().AIStudioOAuthEnabled {
			return geminicli.OAuthConfig{}, ErrGatewayNativeIdentity
		}
	} else {
		input.ClientID, input.ClientSecret = "", ""
	}
	effective, err := geminicli.EffectiveOAuthConfig(input, oauthType)
	if err != nil {
		return geminicli.OAuthConfig{}, ErrGatewayNativeIdentity
	}
	return effective, nil
}

// OAuth state, client configuration and PKCE are sealed in the existing native
// connection journal. The ordinary ephemeral OAuth session/cache is not a
// second owner and is never consulted for completion or refresh.
func NewGatewayNativeGeminiConnectionMethod(profile, generation string, mount GatewayConnectionMount, oauth *GeminiOAuthService) (GatewayNativeConnectionMethodAdapter, error) {
	var empty GatewayNativeConnectionMethodAdapter
	definition, found := LookupGatewayConnectionProfile(profile)
	oauthType := gatewayGeminiOAuthType(profile)
	client, err := gatewayGeminiOAuthConfig(oauth, oauthType)
	if err != nil || !found || definition.DispatchFamily != "gemini_oauth" || !GatewayConnectionMountValid(mount) ||
		(mount.BaseURL != geminicli.GeminiCliBaseURL && mount.BaseURL != geminicli.AIStudioBaseURL) || !GatewayAccountModelIDValid(mount.Model) {
		return empty, ErrGatewayNativeIdentity
	}
	var method GatewayConnectionMethodDefinition
	for _, candidate := range definition.Methods {
		if candidate.MethodID == "browser" && candidate.Interaction.Kind == "browser" {
			method = candidate
		}
	}
	selection := GatewayConnectionSelection{ProfileID: profile, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation}
	if !GatewayConnectionSelectionValid(selection) {
		return empty, ErrGatewayNativeIdentity
	}
	redirect := geminicli.GeminiCLIRedirectURI
	if oauthType == "ai_studio" {
		redirect = geminicli.AIStudioOAuthRedirectURI
	}
	attestation := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native"}
	authFamily := "gemini_" + oauthType + "_oauth"
	current := func() bool {
		actual, err := gatewayGeminiOAuthConfig(oauth, oauthType)
		return err == nil && actual == client
	}
	authorized := func(ctx context.Context, in GatewayNativeConnectionScope) bool {
		return ctx != nil && ctx.Err() == nil && in.Connection == selection && GatewayConnectionScopeAuthorized(ctx, in.Scope) && GatewayConnectionSelectionAuthorized(ctx, &in.Connection) && current()
	}
	adapter := GatewayNativeConnectionMethodAdapter{ProfileID: profile, MethodID: method.MethodID, MethodRevision: method.Revision,
		ConfigurationGeneration: generation, DispatchFamily: definition.DispatchFamily, AccountType: AccountTypeOAuth, Mount: mount}
	adapter.Prepare = func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
		if !current() || ValidateGatewayConnectionInput(method.Interaction.PreflightFields, input) != nil {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		project, tier := strings.TrimSpace(input["project_id"]), strings.TrimSpace(input["tier_id"])
		if project != "" && !gatewayCloudProject.MatchString(project) || oauthType == "code_assist" && project == "" || validateTierID(tier) != nil {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		base := geminicli.GeminiCliBaseURL
		if oauthType == "ai_studio" && project == "" {
			base = geminicli.AIStudioBaseURL
		}
		if mount.BaseURL != base {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		state, err := geminicli.GenerateState()
		if err != nil {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		verifier, err := geminicli.GenerateCodeVerifier()
		if err != nil {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		authorizeURL, err := geminicli.BuildAuthorizationURL(client, state, geminicli.GenerateCodeChallenge(verifier), redirect, project, oauthType)
		if err != nil {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		material, err := json.Marshal(gatewayGeminiOAuthMaterial{state, verifier, redirect, project, tier, oauthType, client})
		if err != nil || len(material) > GatewayConnectionInputBytes {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		return GatewayNativeConnectionPrepared{Material: material, Credential: attestation, AuthFamily: authFamily, AuthorizeURL: authorizeURL}, nil
	}
	adapter.Complete = func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
		if !authorized(ctx, in) || len(completion) != 1 {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
		}
		var material gatewayGeminiOAuthMaterial
		if _, err := gatewayOAuthJSON(raw); err != nil || json.Unmarshal(raw, &material) != nil || material.Client != client ||
			material.OAuthType != oauthType || material.RedirectURI != redirect || material.Verifier == "" {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
		}
		state, code, err := gatewayGeminiConnectionCallback(completion["completion"], material.RedirectURI)
		if err != nil || state != material.State {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
		}
		token, err := oauth.oauthClient.ExchangeCode(ctx, oauthType, code, material.Verifier, material.RedirectURI, "")
		if err != nil {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
		}
		if oauthType == "google_one" && material.ProjectID == "" {
			reader, ok := oauth.oauthClient.(interface {
				GatewayNativeGeminiProject(context.Context, string) (string, error)
			})
			if !ok || token == nil || !gatewayCloudSecretValid(token.AccessToken, 32768, true) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
			}
			material.ProjectID, err = reader.GatewayNativeGeminiProject(ctx, token.AccessToken)
			if err != nil || !gatewayCloudProject.MatchString(material.ProjectID) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
			}
		}
		return gatewayGeminiConnectionCredential(profile, oauthType, authFamily, material.ProjectID, material.TierID, "", token, time.Now())
	}
	adapter.RefreshNeeded = func(raw json.RawMessage, now time.Time) (bool, error) {
		credentials, err := gatewayGeminiCredentialDocument(raw, oauthType)
		if err != nil {
			return false, err
		}
		if credentials["refresh_token"] == "" {
			return false, nil
		}
		expiry, err := strconv.ParseInt(credentials["expires_at"], 10, 64)
		return !now.Add(5 * time.Minute).Before(time.Unix(expiry, 0)), err
	}
	adapter.Refresh = func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage) (GatewayNativeConnectionCredential, error) {
		credentials, err := gatewayGeminiCredentialDocument(raw, oauthType)
		if err != nil || !authorized(ctx, in) || credentials["refresh_token"] == "" {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
		}
		token, err := oauth.oauthClient.RefreshToken(ctx, oauthType, credentials["refresh_token"], "")
		if err != nil {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
		}
		return gatewayGeminiConnectionCredential(profile, oauthType, authFamily, credentials["project_id"], credentials["tier_id"], credentials["refresh_token"], token, time.Now())
	}
	return adapter, nil
}

func gatewayGeminiCredentialDocument(raw json.RawMessage, oauthType string) (map[string]string, error) {
	var credentials map[string]string
	if _, err := gatewayOAuthJSON(raw); err != nil || json.Unmarshal(raw, &credentials) != nil || credentials["oauth_type"] != oauthType ||
		!gatewayCloudSecretValid(credentials["access_token"], 32768, true) || !gatewayCloudSecretValid(credentials["refresh_token"], 32768, false) {
		return nil, ErrGatewayNativeIdentity
	}
	expiry, err := strconv.ParseInt(credentials["expires_at"], 10, 64)
	if err != nil || expiry < 1 || expiry > 253402300799 {
		return nil, ErrGatewayNativeIdentity
	}
	return credentials, nil
}

func gatewayGeminiConnectionCredential(profile, oauthType, authFamily, project, tier, previousRefresh string, token *geminicli.TokenResponse, now time.Time) (GatewayNativeConnectionCredential, error) {
	if token == nil || !gatewayCloudSecretValid(token.AccessToken, 32768, true) || !gatewayCloudSecretValid(token.RefreshToken, 32768, false) ||
		!strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn < 1 || token.ExpiresIn > 86400 || !gatewayConnectionTextValid(token.Scope, 8192, true) {
		return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
	}
	refresh := token.RefreshToken
	if refresh == "" {
		refresh = previousRefresh
	}
	expiry := now.Add(time.Duration(token.ExpiresIn) * time.Second)
	credentials := map[string]string{"access_token": token.AccessToken, "token_type": "Bearer", "expires_at": strconv.FormatInt(expiry.Unix(), 10), "oauth_type": oauthType}
	for key, value := range map[string]string{"refresh_token": refresh, "scope": token.Scope, "project_id": project, "tier_id": tier} {
		if value != "" {
			credentials[key] = value
		}
	}
	raw, err := json.Marshal(credentials)
	if err != nil || len(raw) > GatewayConnectionInputBytes {
		return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
	}
	attestation := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "none"}
	if refresh != "" {
		attestation.RefreshPolicy = "native"
	}
	observations := []GatewayAccountInfoObservation{{Source: "credential", Fields: gatewayOAuthCredentialExpiryInfo(profile, expiry, now)}}
	if fields := GatewayDeclaredSubscriptionInfo(profile, tier, now); fields != nil {
		observations = append(observations, GatewayAccountInfoObservation{Source: "user_declaration", Fields: *fields})
	}
	return GatewayNativeConnectionCredential{Document: raw, Credential: attestation, AuthFamily: authFamily, Observations: observations}, nil
}
