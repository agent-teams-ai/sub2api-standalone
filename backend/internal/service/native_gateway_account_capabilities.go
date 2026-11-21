package service

// Connection discovery is a safe projection of deployed native profiles. Model
// membership/entitlement remains with the existing account-scoped catalog.
type GatewayAccountCapabilityLabel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type GatewayAccountConnectionMethod struct {
	Kind                string `json:"kind"`
	Create              bool   `json:"create"`
	Reconnect           bool   `json:"reconnect"`
	CompletionInputKind string `json:"completionInputKind,omitempty"`
	CallbackMode        string `json:"callbackMode,omitempty"`
	Method              string `json:"method,omitempty"`
	MaxBytes            int    `json:"maxBytes,omitempty"`
}
type GatewayAccountCapability struct {
	ProfileID      string                           `json:"profileId"`
	Provider       GatewayAccountCapabilityLabel    `json:"provider"`
	Product        GatewayAccountCapabilityLabel    `json:"product"`
	Region         GatewayAccountCapabilityLabel    `json:"region"`
	Protocol       string                           `json:"protocol"`
	Models         []GatewayAccountModel            `json:"models"`
	Methods        []GatewayAccountConnectionMethod `json:"methods"`
	AccountInfo    bool                             `json:"accountInfo"`
	LimitsContract string                           `json:"limitsContract"`
	Availability   string                           `json:"availability"`
}
type GatewayAccountCapabilities struct {
	Contract string                     `json:"contract"`
	Profiles []GatewayAccountCapability `json:"profiles"`
}

// The caller supplies only an actually mounted profile/anchor. This pure
// definition never enumerates the broad admin catalog or requests provider HTTP.
func GatewayNativeAccountCapability(profile, anchor string, info, imports bool) (GatewayAccountCapability, bool) {
	definition, ok := LookupGatewayConnectionProfile(profile)
	if !ok || !GatewayAccountModelIDValid(anchor) {
		return GatewayAccountCapability{}, false
	}
	out := GatewayAccountCapability{
		ProfileID: profile, Provider: definition.Provider, Product: definition.Product,
		Region: definition.Region, Protocol: definition.Protocol,
		Models:  GatewayNativeConnectionModels(profile, anchor),
		Methods: []GatewayAccountConnectionMethod{}, AccountInfo: info,
		LimitsContract: definition.LimitsContract, Availability: "enabled",
	}
	if out.LimitsContract == "unsupported" {
		out.LimitsContract = "none"
	}
	for _, method := range definition.Methods {
		legacy, ok := gatewayLegacyConnectionMethod(method, imports)
		if ok {
			out.Methods = append(out.Methods, legacy)
		}
	}
	if len(out.Methods) == 0 {
		return GatewayAccountCapability{}, false
	}
	return out, true
}

// Both ABI projections use the same native-qualified model anchors. Mutable
// entitlement is resolved separately by the account-scoped model reader.
func GatewayNativeConnectionModels(profile, anchor string) []GatewayAccountModel {
	if _, ok := LookupGatewayConnectionProfile(profile); !ok || !GatewayAccountModelIDValid(anchor) {
		return nil
	}
	models := []GatewayAccountModel{{ModelID: anchor, Tools: GatewayAccountModelChoices{Allowed: []string{"function"}, Default: "function"}}}
	switch profile {
	case GatewayCodexOAuthResponsesProfile:
		models[0].Reasoning = &GatewayAccountModelChoices{Allowed: []string{"minimal", "low", "medium", "high", "xhigh"}, Default: "high"}
		models[0].Fast = &GatewayAccountModelChoices{Allowed: []string{"balanced"}, Default: "balanced"}
	case GatewayClaudeOAuthMessagesProfile:
		models[0].Reasoning = &GatewayAccountModelChoices{Allowed: []string{"low", "medium", "high"}, Default: "medium"}
		models[0].Fast = &GatewayAccountModelChoices{Allowed: []string{"balanced"}, Default: "balanced"}
	}
	if GatewayModelCatalogProfile(profile) {
		models = gatewayCatalogAccountModels(profile, anchor, nil)
	}
	return models
}

// Old ingress is an exact semantic alias, never the first similar form. Only
// the legacy credential policies fit this ABI; new methods have no implicit
// alias even when they also render a secret or a document input.
func gatewayLegacyConnectionMethod(method GatewayConnectionMethodDefinition, imports bool) (GatewayAccountConnectionMethod, bool) {
	out := GatewayAccountConnectionMethod{Create: true, Reconnect: true}
	if len(method.AllowedCredentialPolicies) != 1 || len(method.ConfigurationRequirements) != 0 {
		return out, false
	}
	switch method.LegacyKind {
	case "api_key":
		if method.AllowedCredentialPolicies[0] != "provider_key" {
			return out, false
		}
		out.Kind = "api_key"
	case "browser":
		if method.AllowedCredentialPolicies[0] != "subscription_session" || len(method.Interaction.PreflightFields) != 0 {
			return out, false
		}
		out.Kind, out.CompletionInputKind = "browser", method.Interaction.CompletionInputKind
		out.CallbackMode = "automatic"
		if method.Interaction.CallbackMode == "manual" {
			out.CallbackMode = "manual_callback_url"
		}
	case "device_code":
		if method.AllowedCredentialPolicies[0] != "subscription_session" || len(method.Interaction.PreflightFields) != 0 {
			return out, false
		}
		out.Kind = "device_code"
	case "credential_import:refresh_token", "credential_import:oauth_auth_json":
		if !imports || method.AllowedCredentialPolicies[0] != "subscription_session" {
			return out, false
		}
		out.Kind, out.MaxBytes = "credential_import", GatewayConnectionInputBytes
		out.Method = "refresh_token"
		if method.LegacyKind == "credential_import:oauth_auth_json" {
			out.Method = "oauth_auth_json"
		}
	default:
		return out, false
	}
	return out, true
}
