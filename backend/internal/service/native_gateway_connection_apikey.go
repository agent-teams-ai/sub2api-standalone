package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func gatewayConnectionAPIKeyMountValid(definition GatewayConnectionProfileDefinition, mount GatewayConnectionMount) bool {
	u, err := url.Parse(mount.BaseURL)
	if err != nil || len(mount.BaseURL) > 2048 || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !GatewayAccountModelIDValid(mount.Model) ||
		!gatewayConnectionIdentifier.MatchString(mount.QualificationRef) {
		return false
	}
	if GatewayNativeAPIKeyProfileValid(definition.ProfileID, mount.BaseURL, mount.Model) {
		return true
	}
	if native := LookupProviderProfile(definition.Platform); native != nil {
		endpoints, exactMode := native.Modes[definition.AccountMode]
		if !exactMode {
			return false
		}
		for _, base := range endpoints.BaseURLs {
			if mount.BaseURL == base {
				return true
			}
		}
		return false
	}
	account := &Account{Platform: definition.Platform, Type: AccountTypeAPIKey}
	switch definition.DispatchFamily {
	case "openai_responses":
		return strings.TrimSuffix(mount.BaseURL, "/v1") == account.GetOpenAIBaseURL()
	case "grok_responses":
		return xai.IsOfficialBaseURL(mount.BaseURL)
	case "typesafe_systemone":
		return mount.BaseURL == account.GetTypeSafeBaseURL()
	}
	return false
}

func gatewayConnectionModelPrincipal(scope GatewayNativeCredentialScope, selection GatewayConnectionSelection) string {
	raw, _ := json.Marshal(struct {
		Scope      GatewayNativeCredentialScope
		Connection GatewayConnectionSelection
	}{scope, selection})
	digest := sha256.Sum256(raw)
	return "native-models-" + hex.EncodeToString(digest[:])
}

// NewGatewayNativeAPIKeyConnectionMethod reuses native platform/product modes
// and defaults. Submitted input can select neither an endpoint nor a protocol.
// Structured cloud and configured-target methods have their own actual native
// adapters; they cannot enter this plain-key path merely by rendering a secret.
func NewGatewayNativeAPIKeyConnectionMethod(profileID, generation string, mount GatewayConnectionMount, gateway *OpenAIGatewayService) (GatewayNativeConnectionMethodAdapter, error) {
	var empty GatewayNativeConnectionMethodAdapter
	definition, found := LookupGatewayConnectionProfile(profileID)
	if !found || !gatewayConnectionAPIKeyMountValid(definition, mount) {
		return empty, ErrGatewayNativeIdentity
	}
	var method GatewayConnectionMethodDefinition
	for _, candidate := range definition.Methods {
		if candidate.MethodID == "api_key" && candidate.Interaction.Kind == "credential_form" && len(candidate.ConfigurationRequirements) == 0 &&
			len(candidate.AllowedCredentialPolicies) == 1 && candidate.AllowedCredentialPolicies[0] == "provider_key" {
			method = candidate
		}
	}
	selection := GatewayConnectionSelection{ProfileID: profileID, MethodID: method.MethodID, MethodRevision: method.Revision, ConfigurationGeneration: generation}
	if !GatewayConnectionSelectionValid(selection) || profileID == GatewayOpenRouterResponsesProfile && gateway == nil {
		return empty, ErrGatewayNativeIdentity
	}
	attestation := GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"}
	prepare := func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
		if ValidateGatewayConnectionInput(method.Interaction.InputFields, input) != nil || !GatewayNativeIngressKeyValid(input["api_key"]) {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		document := map[string]any{"api_key": input["api_key"]}
		if native := LookupProviderProfile(definition.Platform); native != nil {
			// These values come from the same provider profile the normal native
			// frontend and endpoint builders use. No consumer-side protocol map.
			document["account_mode"] = definition.AccountMode
			document["api_protocol"] = APIProtocolAdaptive
			bases := make(map[string]string)
			for protocol, base := range native.Modes[definition.AccountMode].BaseURLs {
				bases[protocol] = base
			}
			if GatewayNativeCodingPlanProfileValid(profileID, mount.BaseURL, mount.Model) {
				// Existing qualified legacy profiles keep their exact endpoint.
				// Their public IDs must never move an existing key to a newer
				// native default merely because the inventory was extended.
				selected := APIProtocolResponses
				if !native.SupportsProtocol(definition.AccountMode, APIProtocolResponses) {
					selected = APIProtocolChatCompletions
				}
				bases[selected] = mount.BaseURL
			}
			document["api_base_urls"] = bases
		} else {
			document["base_url"] = mount.BaseURL
		}
		if definition.Platform == PlatformZhipu {
			if value := strings.TrimSpace(input["organization"]); value != "" {
				document["zhipu_organization"] = value
			}
			if value := strings.TrimSpace(input["project"]); value != "" {
				document["zhipu_project"] = value
			}
		}
		raw, err := json.Marshal(document)
		if err != nil || len(raw) > GatewayConnectionInputBytes {
			return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
		}
		return GatewayNativeConnectionPrepared{Material: raw, EntryClaim: []byte(input["api_key"]), Credential: attestation, AuthFamily: "provider_api_key"}, nil
	}
	return GatewayNativeConnectionMethodAdapter{ProfileID: profileID, MethodID: method.MethodID, MethodRevision: method.Revision,
		ConfigurationGeneration: generation, DispatchFamily: definition.DispatchFamily, AccountType: AccountTypeAPIKey, Mount: mount, Prepare: prepare,
		Complete: func(ctx context.Context, scope GatewayNativeConnectionScope, material json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
			if ctx == nil || ctx.Err() != nil || scope.Connection != selection || len(completion) != 0 || !GatewayConnectionScopeAuthorized(ctx, scope.Scope) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			fields, err := gatewayOAuthJSON(material)
			key, valid := gatewayOAuthString(fields, "api_key")
			if err != nil || !valid || !GatewayNativeIngressKeyValid(key) {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			if profileID == GatewayOpenRouterResponsesProfile && gateway.qualifyGatewayOpenRouterCatalog(ctx, gatewayConnectionModelPrincipal(scope.Scope, selection), mount.Model) != nil {
				return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
			}
			return GatewayNativeConnectionCredential{Document: append(json.RawMessage(nil), material...), Credential: attestation, AuthFamily: "provider_api_key"}, nil
		},
	}, nil
}
