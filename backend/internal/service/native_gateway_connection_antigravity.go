package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
)

func gatewayAntigravityMountValid(profile string, mount GatewayConnectionMount) bool {
	if !GatewayConnectionMountValid(mount) || !GatewayAccountModelIDValid(mount.Model) {
		return false
	}
	if profile == "antigravity-responses-oauth-v1" {
		return antigravity.OfficialBaseURL(mount.BaseURL)
	}
	u, err := url.Parse(mount.BaseURL)
	return profile == "antigravity-target-responses-apikey-v1" && err == nil && u.Scheme == "https" && u.Host != "" &&
		u.User == nil && u.RawPath == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

// Native Antigravity browser/import reuse its Google OAuth client. Only its
// ordinary session store, endpoint fallback and retry loop are bypassed: the
// existing durable private journal already owns those decisions.
func NewGatewayNativeAntigravityConnectionMethod(profile, methodID, generation string, mount GatewayConnectionMount, transports ...http.RoundTripper) (GatewayNativeConnectionMethodAdapter, error) {
	var empty GatewayNativeConnectionMethodAdapter
	definition, found := LookupGatewayConnectionProfile(profile)
	if !found || definition.DispatchFamily != "antigravity" || !gatewayAntigravityMountValid(profile, mount) {
		return empty, ErrGatewayNativeIdentity
	}
	var method GatewayConnectionMethodDefinition
	for _, candidate := range definition.Methods {
		if candidate.MethodID == methodID {
			method = candidate
		}
	}
	selection := GatewayConnectionSelection{ProfileID: profile, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation}
	if !GatewayConnectionSelectionValid(selection) || methodID != "browser" && methodID != "refresh_token" && methodID != "api_key" {
		return empty, ErrGatewayNativeIdentity
	}
	adapter := GatewayNativeConnectionMethodAdapter{ProfileID: profile, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation,
		DispatchFamily: definition.DispatchFamily, AccountType: AccountTypeOAuth, Mount: mount}
	if methodID == "api_key" {
		method.Interaction.InputFields = append([]GatewayConnectionField(nil), method.Interaction.InputFields...)
		for i := range method.Interaction.InputFields {
			if method.Interaction.InputFields[i].FieldID == "target_id" {
				method.Interaction.InputFields[i].Choices = []GatewayConnectionFieldChoice{{Value: mount.QualificationRef, Label: "Configured upstream"}}
			}
		}
		adapter.Definition, adapter.AccountType = &method, AccountTypeAPIKey
		attestation := GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"}
		adapter.Prepare = func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
			project := strings.TrimSpace(input["project_id"])
			if ValidateGatewayConnectionInput(method.Interaction.InputFields, input) != nil || input["target_id"] != mount.QualificationRef ||
				!GatewayNativeIngressKeyValid(input["api_key"]) || project != "" && !gatewayCloudProject.MatchString(project) {
				return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
			}
			document := map[string]string{"api_key": input["api_key"], "base_url": mount.BaseURL, "target_id": mount.QualificationRef}
			if project != "" {
				document["project_id"] = project
			}
			raw, err := json.Marshal(document)
			if err != nil {
				return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
			}
			return GatewayNativeConnectionPrepared{Material: raw, EntryClaim: []byte(input["api_key"]), Credential: attestation, AuthFamily: "antigravity_target_api_key"}, nil
		}
		adapter.Complete = func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
			var document map[string]string
			if ctx == nil || ctx.Err() != nil || in.Connection != selection || !GatewayConnectionScopeAuthorized(ctx, in.Scope) || !GatewayConnectionSelectionAuthorized(ctx, &in.Connection) ||
				len(completion) != 0 || json.Unmarshal(raw, &document) != nil || document["base_url"] != mount.BaseURL || document["target_id"] != mount.QualificationRef || !GatewayNativeIngressKeyValid(document["api_key"]) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			return GatewayNativeConnectionCredential{Document: append(json.RawMessage(nil), raw...), Credential: attestation, AuthFamily: "antigravity_target_api_key"}, nil
		}
		return adapter, nil
	}
	client, err := antigravity.NewBoundedOAuthClient(transports...)
	fingerprint, fingerprintErr := antigravity.OAuthConfigurationFingerprint()
	if err != nil || fingerprintErr != nil {
		return empty, ErrGatewayNativeIdentity
	}
	authorized := func(ctx context.Context, in GatewayNativeConnectionScope) bool {
		current, err := antigravity.OAuthConfigurationFingerprint()
		return err == nil && current == fingerprint && ctx != nil && ctx.Err() == nil && in.Connection == selection && GatewayConnectionScopeAuthorized(ctx, in.Scope) && GatewayConnectionSelectionAuthorized(ctx, &in.Connection)
	}
	attestation := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native"}
	type material struct {
		State       string `json:"state,omitempty"`
		Verifier    string `json:"verifier,omitempty"`
		Fingerprint string `json:"fingerprint"`
		Project     string `json:"project_id,omitempty"`
		Refresh     string `json:"refresh_token,omitempty"`
	}
	adapter.Prepare = func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
		fields := method.Interaction.InputFields
		if methodID == "browser" {
			fields = method.Interaction.PreflightFields
		}
		project := strings.TrimSpace(input["project_id"])
		if ValidateGatewayConnectionInput(fields, input) != nil || project != "" && !gatewayCloudProject.MatchString(project) {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		state := material{Fingerprint: fingerprint, Project: project}
		out := GatewayNativeConnectionPrepared{Credential: attestation, AuthFamily: "antigravity_oauth"}
		if methodID == "refresh_token" {
			if !gatewayCloudSecretValid(input["refresh_token"], 32768, true) {
				return out, ErrGatewayNativeIdentity
			}
			state.Refresh, out.EntryClaim = input["refresh_token"], []byte(input["refresh_token"])
		} else {
			var err error
			state.State, err = antigravity.GenerateState()
			if err != nil {
				return out, ErrGatewayNativeIdentity
			}
			state.Verifier, err = antigravity.GenerateCodeVerifier()
			if err != nil {
				return out, ErrGatewayNativeIdentity
			}
			out.AuthorizeURL = antigravity.BuildAuthorizationURL(state.State, antigravity.GenerateCodeChallenge(state.Verifier))
		}
		raw, err := json.Marshal(state)
		if err != nil {
			return out, ErrGatewayNativeIdentity
		}
		out.Material = raw
		return out, nil
	}
	credential := func(ctx context.Context, project, refresh string, token *antigravity.TokenResponse) (GatewayNativeConnectionCredential, error) {
		if token == nil || !gatewayCloudSecretValid(token.AccessToken, 32768, true) {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
		}
		var info *GatewayAccountInfoFields
		if project == "" {
			loaded, err := client.LoadCodeAssistFromURL(ctx, mount.BaseURL, token.AccessToken)
			if err != nil || loaded == nil {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
			}
			info = gatewayAntigravityAccountInfo(loaded, time.Now().UTC())
			project = loaded.CloudAICompanionProject
			if project == "" {
				tier := loaded.GetTier()
				if tier == "" {
					tier = "free-tier"
				}
				onboarded, err := client.OnboardUserFromURL(ctx, mount.BaseURL, token.AccessToken, tier)
				if err != nil || onboarded == nil || !onboarded.Done {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
				}
				switch value := onboarded.Response["cloudaicompanionProject"].(type) {
				case string:
					project = value
				case map[string]any:
					project, _ = value["id"].(string)
				}
			}
		} else if deadline, bounded := ctx.Deadline(); !bounded || time.Until(deadline) > 4*time.Second {
			// A known project does not imply a known subscription. This optional
			// bounded observation must not jeopardize publishing a rotated token.
			readCtx, stopRead := context.WithTimeout(ctx, 2*time.Second)
			if loaded, err := client.LoadCodeAssistFromURL(readCtx, mount.BaseURL, token.AccessToken); err == nil {
				info = gatewayAntigravityAccountInfo(loaded, time.Now().UTC())
			}
			stopRead()
		}
		if !gatewayCloudProject.MatchString(project) {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
		}
		result, err := gatewayGeminiConnectionCredential(profile, "antigravity", "antigravity_oauth", project, "", refresh,
			&geminicli.TokenResponse{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, TokenType: token.TokenType, ExpiresIn: token.ExpiresIn, Scope: token.Scope}, time.Now())
		if err == nil && info != nil {
			result.Observations = append(result.Observations, GatewayAccountInfoObservation{Source: "plan_quota", Fields: *info})
		}
		return result, err
	}
	adapter.Complete = func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
		var state material
		if !authorized(ctx, in) || json.Unmarshal(raw, &state) != nil || state.Fingerprint != fingerprint {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
		}
		var token *antigravity.TokenResponse
		var err error
		if methodID == "refresh_token" {
			if len(completion) != 0 || !gatewayCloudSecretValid(state.Refresh, 32768, true) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			token, err = client.RefreshToken(ctx, state.Refresh)
		} else {
			callbackState, code, callbackErr := gatewayGeminiConnectionCallback(completion["completion"], antigravity.RedirectURI)
			if len(completion) != 1 || callbackErr != nil || callbackState != state.State || state.Verifier == "" {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			token, err = client.ExchangeCode(ctx, code, state.Verifier)
		}
		if err != nil {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
		}
		return credential(ctx, state.Project, state.Refresh, token)
	}
	adapter.RefreshNeeded = func(raw json.RawMessage, now time.Time) (bool, error) {
		document, err := gatewayGeminiCredentialDocument(raw, "antigravity")
		if err != nil {
			return false, err
		}
		if document["refresh_token"] == "" {
			return false, nil
		}
		expiry, err := strconv.ParseInt(document["expires_at"], 10, 64)
		return !now.Add(5 * time.Minute).Before(time.Unix(expiry, 0)), err
	}
	adapter.Refresh = func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage) (GatewayNativeConnectionCredential, error) {
		document, err := gatewayGeminiCredentialDocument(raw, "antigravity")
		if err != nil || !authorized(ctx, in) || document["refresh_token"] == "" {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
		}
		token, err := client.RefreshToken(ctx, document["refresh_token"])
		if err != nil {
			return GatewayNativeConnectionCredential{}, ErrGatewayNativeEffectUnknown
		}
		return credential(ctx, document["project_id"], document["refresh_token"], token)
	}
	return adapter, nil
}
