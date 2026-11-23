package service

// GatewayConnectionAdminAction binds a native method to an existing admin form.
// Surface names distinguish forms with different implemented handlers. These
// declarations never enable a protected adapter or an unimplemented UI action.
type GatewayConnectionAdminAction struct {
	Label                     string   `json:"label,omitempty"`
	Category                  string   `json:"category"`
	AddMethod                 string   `json:"add_method,omitempty"`
	OAuthType                 string   `json:"oauth_type,omitempty"`
	InputMethod               string   `json:"input_method,omitempty"`
	CredentialMode            string   `json:"credential_mode,omitempty"`
	Surfaces                  []string `json:"surfaces"`
	ConfigurationRequirements []string `json:"configuration_requirements"`
}

type PlatformAdminConnectionMethod struct {
	ProfileID                 string                        `json:"profile_id"`
	MethodID                  string                        `json:"method_id"`
	MethodRevision            int                           `json:"method_revision"`
	Platform                  string                        `json:"platform"`
	AccountMode               string                        `json:"account_mode,omitempty"`
	Label                     string                        `json:"label"`
	Provider                  GatewayAccountCapabilityLabel `json:"provider"`
	Product                   GatewayAccountCapabilityLabel `json:"product"`
	Category                  string                        `json:"category"`
	AddMethod                 string                        `json:"add_method,omitempty"`
	OAuthType                 string                        `json:"oauth_type,omitempty"`
	InputMethod               string                        `json:"input_method,omitempty"`
	CredentialMode            string                        `json:"credential_mode,omitempty"`
	Surfaces                  []string                      `json:"surfaces"`
	ConfigurationRequirements []string                      `json:"configuration_requirements"`
}

func buildPlatformAdminConnectionMethods(profiles []GatewayConnectionProfileDefinition) []PlatformAdminConnectionMethod {
	result := make([]PlatformAdminConnectionMethod, 0)
	for _, profile := range profiles {
		for _, method := range profile.Methods {
			for _, action := range method.AdminActions {
				label := method.Label
				if action.Label != "" {
					label = action.Label
				}
				result = append(result, PlatformAdminConnectionMethod{
					ProfileID: profile.ProfileID, MethodID: method.MethodID, MethodRevision: method.Revision,
					Platform: profile.Platform, AccountMode: profile.AccountMode, Label: label,
					Provider: profile.Provider, Product: profile.Product, Category: action.Category,
					AddMethod: action.AddMethod, OAuthType: action.OAuthType, InputMethod: action.InputMethod,
					CredentialMode: action.CredentialMode, Surfaces: append([]string{}, action.Surfaces...),
					ConfigurationRequirements: append([]string{}, action.ConfigurationRequirements...),
				})
			}
		}
	}
	return result
}

// This admin adapter metadata belongs to the same native method declaration as
// its credential grammar. It names existing UI actions, not provider transport
// handlers; the frontend has only a finite supported-action check.
func declareGatewayConnectionAdminActions(profile *GatewayConnectionProfileDefinition) {
	for index := range profile.Methods {
		method := &profile.Methods[index]
		if len(method.AdminActions) != 0 {
			continue
		}
		var action GatewayConnectionAdminAction
		switch method.MethodID {
		case "api_key":
			action = GatewayConnectionAdminAction{Category: "apikey", Surfaces: []string{"create"}}
		case "aws_sigv4", "bedrock_api_key":
			mode := "sigv4"
			if method.MethodID == "bedrock_api_key" {
				mode = "apikey"
			}
			action = GatewayConnectionAdminAction{Category: "bedrock", CredentialMode: mode, Surfaces: []string{"create"}}
		case "vertex_service_account", "vertex_api_key":
			mode := "service_account"
			if method.MethodID == "vertex_api_key" {
				mode = "apikey"
			}
			action = GatewayConnectionAdminAction{Category: "service_account", CredentialMode: mode, Surfaces: []string{"create"}}
		case "browser", "cookie_oauth", "setup_token_code", "setup_token_cookie":
			action = GatewayConnectionAdminAction{
				Category: "oauth-based", AddMethod: "oauth", InputMethod: "manual",
				Surfaces: []string{"create", "reauth", "flow"},
			}
			if profile.Platform != PlatformGrok {
				action.Surfaces = append(action.Surfaces, "legacy_reauth")
			}
			if method.MethodID == "cookie_oauth" || method.MethodID == "setup_token_cookie" {
				action.InputMethod = "cookie"
			}
			if method.MethodID == "setup_token_code" || method.MethodID == "setup_token_cookie" {
				action.AddMethod = "setup-token"
			}
			if profile.Platform == PlatformGemini {
				action.OAuthType = profile.Product.ID
				if profile.Product.ID == "ai_studio" {
					action.ConfigurationRequirements = []string{"custom_oauth_client"}
				}
			}
		case "refresh_token", "sso":
			action = GatewayConnectionAdminAction{
				Category: "oauth-based", AddMethod: "oauth", InputMethod: method.MethodID,
				Surfaces: []string{"create", "reauth", "flow"},
			}
			if method.MethodID == "sso" {
				action.InputMethod = "sso_cookie"
			}
		case "mobile_refresh_token", "codex_session", "agent_identity", "codex_pat":
			action = GatewayConnectionAdminAction{
				Category: "oauth-based", AddMethod: "oauth", InputMethod: method.MethodID,
				Surfaces: []string{"create", "flow"},
			}
		default:
			// Device and the legacy auth.json method have no separate implemented
			// admin button. Codex session already accepts the supported documents.
			continue
		}
		action.ConfigurationRequirements = append([]string{}, action.ConfigurationRequirements...)
		method.AdminActions = []GatewayConnectionAdminAction{action}
		if profile.Platform == PlatformGrok && method.MethodID == "sso" {
			// The existing optional login helper produces SSO. It is not exposed
			// by Create/ReAuth and does not add a protected password method.
			method.AdminActions = append(method.AdminActions, GatewayConnectionAdminAction{
				Label: "Email and password", Category: "oauth-based", AddMethod: "oauth", InputMethod: "email_password", Surfaces: []string{"flow"},
				ConfigurationRequirements: []string{"grok_password_login"},
			})
		}
	}
}
