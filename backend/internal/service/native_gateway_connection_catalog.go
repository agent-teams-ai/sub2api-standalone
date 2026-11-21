package service

import "github.com/Wei-Shaw/sub2api/internal/domain"

// Definitions extend the existing platform/product authorities with connection
// semantics. They are an inventory, never evidence that a private adapter is
// mounted. Bootstrap can expose a method only through a qualified binding.
type GatewayConnectionMethodDefinition struct {
	MethodID                  string
	Revision                  int
	Label                     string
	Interaction               GatewayConnectionInteraction
	AllowedCredentialPolicies []string
	ConfigurationRequirements []string
	LegacyKind                string
	LegacyInputFieldID        string
}

type GatewayConnectionProfileDefinition struct {
	ProfileID           string
	Platform            string
	AccountMode         string
	Provider            GatewayAccountCapabilityLabel
	Product             GatewayAccountCapabilityLabel
	Region              GatewayAccountCapabilityLabel
	Protocol            string
	DispatchFamily      string
	MetadataSources     []string
	LimitsContract      string
	UnavailableFallback string
	Methods             []GatewayConnectionMethodDefinition
}

// LookupGatewayConnectionProfile is the common native provider/product lookup.
// Its existence grants neither credential authority nor model admission.
func LookupGatewayConnectionProfile(profileID string) (GatewayConnectionProfileDefinition, bool) {
	for _, profile := range GatewayNativeConnectionInventory() {
		if profile.ProfileID == profileID {
			return profile, true
		}
	}
	return GatewayConnectionProfileDefinition{}, false
}

func (m GatewayConnectionMethodDefinition) Descriptor(generation string, enabled bool) GatewayConnectionMethod {
	availability, reason := "enabled", ""
	if !enabled {
		availability, reason = "deployment_configuration_required", "native_binding_unavailable"
	}
	return GatewayConnectionMethod{
		MethodID: m.MethodID, Revision: m.Revision, ConfigurationGeneration: generation,
		Label: m.Label, Create: true, Reconnect: true,
		AllowedCredentialPolicies: append([]string(nil), m.AllowedCredentialPolicies...),
		Requirements:              []string{"owner_scoped_custody/v1", "effect_fence/v1", "native_dispatch/v1"},
		Availability:              availability, Reason: reason, Interaction: m.Interaction,
	}
}

// LegacyAlias declares the exact old-value projection. Required native input
// outside that projection cannot be silently discarded by a legacy client.
func (m GatewayConnectionMethodDefinition) LegacyAlias(connection GatewayConnectionSelection) (GatewayConnectionLegacyAlias, bool) {
	if !GatewayConnectionSelectionValid(connection) || connection.MethodID != m.MethodID || connection.MethodRevision != m.Revision {
		return GatewayConnectionLegacyAlias{}, false
	}
	if _, ok := gatewayLegacyConnectionMethod(m, true); !ok {
		return GatewayConnectionLegacyAlias{}, false
	}
	alias := GatewayConnectionLegacyAlias{Kind: m.LegacyKind, Connection: connection}
	switch m.LegacyKind {
	case "api_key", "credential_import:refresh_token", "credential_import:oauth_auth_json":
		if m.Interaction.Kind != "credential_form" || !gatewayConnectionSemanticID.MatchString(m.LegacyInputFieldID) {
			return GatewayConnectionLegacyAlias{}, false
		}
		found := false
		for _, field := range m.Interaction.InputFields {
			if field.FieldID == m.LegacyInputFieldID {
				found = field.Kind == "secret" || field.Kind == "document"
			} else if field.Required {
				return GatewayConnectionLegacyAlias{}, false
			}
		}
		if !found {
			return GatewayConnectionLegacyAlias{}, false
		}
		alias.InputFieldID = m.LegacyInputFieldID
	case "browser", "device_code":
		if m.LegacyInputFieldID != "" {
			return GatewayConnectionLegacyAlias{}, false
		}
		for _, field := range m.Interaction.PreflightFields {
			if field.Required {
				return GatewayConnectionLegacyAlias{}, false
			}
		}
	default:
		return GatewayConnectionLegacyAlias{}, false
	}
	return alias, true
}

func gatewayConnectionField(id, label, kind string, required bool, maxBytes int) GatewayConnectionField {
	return GatewayConnectionField{FieldID: id, Label: label, Kind: kind, Required: required, MaxBytes: maxBytes}
}

func gatewayConnectionForm(id, label, policy string, fields ...GatewayConnectionField) GatewayConnectionMethodDefinition {
	return GatewayConnectionMethodDefinition{MethodID: id, Revision: 1, Label: label,
		AllowedCredentialPolicies: []string{policy},
		Interaction:               GatewayConnectionInteraction{Kind: "credential_form", InputFields: fields}}
}

func gatewayConnectionKey(id, label, policy string) GatewayConnectionMethodDefinition {
	return gatewayConnectionForm(id, label, policy, gatewayConnectionField("api_key", "API key", "secret", true, 32768))
}

func gatewayConnectionBrowser(id, label, callback, completion string, preflight ...GatewayConnectionField) GatewayConnectionMethodDefinition {
	return GatewayConnectionMethodDefinition{MethodID: id, Revision: 1, Label: label,
		AllowedCredentialPolicies: []string{"subscription_session"},
		Interaction: GatewayConnectionInteraction{Kind: "browser", CallbackMode: callback,
			CompletionInputKind: completion, PreflightFields: preflight}}
}

func gatewayConnectionRefreshToken() GatewayConnectionMethodDefinition {
	return gatewayConnectionForm("refresh_token", "Refresh token", "subscription_session",
		gatewayConnectionField("refresh_token", "Refresh token", "secret", true, 32768))
}

func gatewayConnectionProfile(id, platform, providerID, providerLabel, productID, productLabel, family string, methods ...GatewayConnectionMethodDefinition) GatewayConnectionProfileDefinition {
	return GatewayConnectionProfileDefinition{ProfileID: id, Platform: platform,
		Provider: GatewayAccountCapabilityLabel{ID: providerID, Label: providerLabel},
		Product:  GatewayAccountCapabilityLabel{ID: productID, Label: productLabel},
		Region:   GatewayAccountCapabilityLabel{ID: "global", Label: "Global"},
		Protocol: "openai-responses", DispatchFamily: family, LimitsContract: "unsupported",
		UnavailableFallback: "deny", Methods: methods}
}

// GatewayNativeConnectionInventory returns fresh definitions. Provider endpoint
// and product modes remain in providerProfiles/domain.Platforms; these narrow
// declarations own the connection methods and links to native execution family.
func GatewayNativeConnectionInventory() []GatewayConnectionProfileDefinition {
	key := gatewayConnectionKey("api_key", "API key", "provider_key")
	key.LegacyKind = "api_key"
	key.LegacyInputFieldID = "api_key"
	manual := gatewayConnectionBrowser("browser", "Sign in", "manual", "authorization_code")
	manual.LegacyKind = "browser"
	automatic := gatewayConnectionBrowser("browser", "Sign in", "automatic", "callback_url")
	automatic.LegacyKind = "browser"
	refresh := gatewayConnectionRefreshToken()
	refresh.LegacyKind = "credential_import:refresh_token"
	refresh.LegacyInputFieldID = "refresh_token"
	authJSON := gatewayConnectionForm("auth_json", "Codex auth.json (refreshable)", "subscription_session",
		gatewayConnectionField("content", "auth.json", "document", true, GatewayConnectionInputBytes))
	authJSON.LegacyKind = "credential_import:oauth_auth_json"
	authJSON.LegacyInputFieldID = "content"
	mobile := gatewayConnectionRefreshToken()
	mobile.MethodID, mobile.Label = "mobile_refresh_token", "Mobile refresh token"
	session := gatewayConnectionForm("codex_session", "Codex session or auth.json", "subscription_session",
		gatewayConnectionField("content", "Session credential", "document", true, GatewayConnectionInputBytes))
	session.AllowedCredentialPolicies = []string{"subscription_session", "delegated_identity"}
	agent := gatewayConnectionForm("agent_identity", "Agent Identity", "delegated_identity",
		gatewayConnectionField("content", "Agent Identity auth.json", "document", true, GatewayConnectionInputBytes))
	pat := gatewayConnectionForm("codex_pat", "Codex personal access token", "subscription_session",
		gatewayConnectionField("access_token", "Personal access token", "secret", true, 32768))
	device := GatewayConnectionMethodDefinition{MethodID: "device_code", Revision: 1, Label: "Device sign in",
		AllowedCredentialPolicies: []string{"subscription_session"}, LegacyKind: "device_code",
		Interaction: GatewayConnectionInteraction{Kind: "device"}}
	cookie := gatewayConnectionForm("cookie_oauth", "Sign in with session key", "subscription_session",
		gatewayConnectionField("session_key", "Session key", "secret", true, 32768))
	setupCode := gatewayConnectionBrowser("setup_token_code", "Setup token", "manual", "authorization_code")
	setupCookie := cookie
	setupCookie.MethodID, setupCookie.Label = "setup_token_cookie", "Setup token with session key"
	serviceAccount := gatewayConnectionForm("vertex_service_account", "Service account", "cloud_identity",
		gatewayConnectionField("service_account_json", "Service account JSON", "document", true, 48*1024),
		gatewayConnectionField("project_id", "Project", "text", false, 128),
		gatewayConnectionField("location", "Location", "text", false, 64))
	vertexKey := gatewayConnectionKey("vertex_api_key", "Vertex API key", "cloud_identity")
	vertexKey.Interaction.InputFields = append(vertexKey.Interaction.InputFields,
		gatewayConnectionField("project_id", "Project (optional for Express)", "text", false, 128),
		gatewayConnectionField("location", "Location", "text", false, 64))
	aws := gatewayConnectionForm("aws_sigv4", "AWS credentials", "cloud_identity",
		gatewayConnectionField("access_key_id", "Access key ID", "secret", true, 256),
		gatewayConnectionField("secret_access_key", "Secret access key", "secret", true, 4096),
		gatewayConnectionField("session_token", "Session token", "secret", false, 16384),
		gatewayConnectionField("region", "AWS region", "text", true, 64))
	bedrockKey := gatewayConnectionKey("bedrock_api_key", "Bedrock API key", "cloud_identity")
	bedrockKey.Interaction.InputFields = append(bedrockKey.Interaction.InputFields, gatewayConnectionField("region", "AWS region", "text", true, 64))
	profiles := []GatewayConnectionProfileDefinition{
		gatewayConnectionProfile(GatewayCodexOAuthResponsesProfile, PlatformOpenAI, "openai", "OpenAI Codex", "subscription", "ChatGPT subscription", "codex_responses", automatic, device, refresh, authJSON, mobile, session, agent, pat),
		gatewayConnectionProfile(GatewayClaudeOAuthMessagesProfile, PlatformAnthropic, "anthropic", "Anthropic Claude", "subscription", "Claude subscription", "claude_messages", manual, cookie, setupCode, setupCookie),
		gatewayConnectionProfile(GatewayAnthropicAPIKeyProfile, PlatformAnthropic, "anthropic", "Anthropic", "pay_as_you_go", "Anthropic API", "anthropic_messages", key),
		gatewayConnectionProfile("anthropic-bedrock-messages-v1", PlatformAnthropic, "anthropic", "Anthropic", "bedrock", "Amazon Bedrock", "bedrock_messages", aws, bedrockKey),
		gatewayConnectionProfile("anthropic-vertex-messages-v1", PlatformAnthropic, "anthropic", "Anthropic", "vertex", "Vertex AI", "vertex_messages", serviceAccount),
		gatewayConnectionProfile(GatewayGeminiAPIKeyProfile, PlatformGemini, "gemini", "Gemini", "pay_as_you_go", "Gemini API", "gemini_chat", key),
		gatewayConnectionProfile("gemini-vertex-chat-v1", PlatformGemini, "gemini", "Gemini", "vertex", "Vertex AI", "vertex_gemini", serviceAccount, vertexKey),
		gatewayConnectionProfile("openai-api-responses-apikey-v1", PlatformOpenAI, "openai", "OpenAI", "pay_as_you_go", "OpenAI API", "openai_responses", key),
		gatewayConnectionProfile("grok-api-responses-apikey-v1", PlatformGrok, "grok", "Grok", "pay_as_you_go", "Grok API", "grok_responses", key),
		gatewayConnectionProfile(GatewayGrokOAuthResponsesProfile, PlatformGrok, "grok", "Grok Subscription", "subscription", "Grok subscription", "grok_responses", manual, gatewayConnectionRefreshToken(),
			gatewayConnectionForm("sso", "Grok SSO", "subscription_session", gatewayConnectionField("sso_token", "SSO token", "secret", true, 32768))),
	}
	for _, product := range []struct{ id, label string }{{"google_one", "Google One"}, {"code_assist", "Code Assist"}, {"ai_studio", "AI Studio (custom OAuth client)"}} {
		browser := gatewayConnectionBrowser("browser", "Sign in with Google", "manual", "callback_url",
			gatewayConnectionField("project_id", "Google Cloud project", "text", product.id == "code_assist", 128),
			gatewayConnectionField("tier_id", "Declared tier (optional)", "text", false, 128))
		if product.id == "ai_studio" {
			browser.ConfigurationRequirements = []string{"custom_oauth_client"}
		}
		id := map[string]string{"google_one": "gemini-google-one-responses-oauth-v1", "code_assist": "gemini-code-assist-responses-oauth-v1", "ai_studio": "gemini-ai-studio-responses-oauth-v1"}[product.id]
		profiles = append(profiles, gatewayConnectionProfile(id, PlatformGemini, "gemini", "Gemini", product.id, product.label, "gemini_oauth", browser))
	}
	antiBrowser := gatewayConnectionBrowser("browser", "Sign in", "manual", "callback_url", gatewayConnectionField("project_id", "Project (optional)", "text", false, 128))
	antiRT := gatewayConnectionRefreshToken()
	antiRT.Interaction.InputFields = append(antiRT.Interaction.InputFields, gatewayConnectionField("project_id", "Project (optional)", "text", false, 128))
	antiKey := gatewayConnectionKey("api_key", "Upstream API key", "provider_key")
	antiKey.ConfigurationRequirements = []string{"qualified_server_target"}
	// Composition replaces target choices with its qualified IDs. An empty
	// inventory choice set is not a valid mounted method and cannot be submitted.
	antiKey.Interaction.InputFields = append(antiKey.Interaction.InputFields,
		GatewayConnectionField{FieldID: "target_id", Label: "Target", Kind: "select", Required: true, MaxBytes: 128},
		gatewayConnectionField("project_id", "Project (optional)", "text", false, 128))
	profiles = append(profiles,
		gatewayConnectionProfile("antigravity-responses-oauth-v1", PlatformAntigravity, "antigravity", "Antigravity", "subscription", "Antigravity subscription", "antigravity", antiBrowser, antiRT),
		gatewayConnectionProfile("antigravity-target-responses-apikey-v1", PlatformAntigravity, "antigravity", "Antigravity", "upstream", "Configured upstream", "antigravity", antiKey))
	for _, spec := range []struct{ platform, mode, id, provider, label, product, productLabel string }{
		{PlatformKimi, AccountModePayG, "kimi-payg-responses-apikey-v1", "kimi", "Kimi", "pay_as_you_go", "Kimi API"},
		{PlatformKimi, AccountModeCoding, GatewayKimiCodeResponsesProfile, "kimi", "Kimi Code", "coding_plan", "Kimi Coding Plan"},
		{PlatformZhipu, AccountModePayG, "glm-payg-responses-apikey-v1", "glm", "GLM", "pay_as_you_go", "GLM API"},
		{PlatformZhipu, AccountModeCoding, GatewayGLMCodingResponsesProfile, "glm", "GLM Coding Plan", "coding_plan", "GLM Coding Plan"},
		{PlatformDeepseek, AccountModePayG, "deepseek-responses-apikey-v1", "deepseek", "DeepSeek", "pay_as_you_go", "DeepSeek API"},
		{PlatformMiniMax, AccountModePayG, "minimax-payg-responses-apikey-v1", "minimax", "MiniMax", "pay_as_you_go", "MiniMax API"},
		{PlatformMiniMax, AccountModeCoding, GatewayMiniMaxMPlanResponsesProfile, "minimax", "MiniMax M Plan", "m_plan", "MiniMax M Plan"},
		{PlatformOpenCodeGo, AccountModeGo, "opencode-go-responses-apikey-v1", "opencode", "OpenCode", "go", "OpenCode Go"},
		{PlatformOpenCodeGo, AccountModeZen, "opencode-zen-responses-apikey-v1", "opencode", "OpenCode", "zen", "OpenCode Zen"},
		{PlatformCommandCode, AccountModePayG, "command-code-responses-apikey-v1", "commandcode", "Command Code", "pay_as_you_go", "Command Code"},
		{PlatformCline, AccountModePayG, "cline-chat-apikey-v1", "cline", "Cline", "credits_pass_free", "Cline credits and ClinePass"},
	} {
		// A declaration cannot invent a platform or mode absent from native.
		p := LookupProviderProfile(spec.platform)
		if p == nil || !domain.IsConcretePlatform(spec.platform) {
			continue
		}
		if _, ok := p.Modes[spec.mode]; !ok {
			continue
		}
		method := gatewayConnectionKey("api_key", "API key", "provider_key")
		method.LegacyKind = "api_key"
		method.LegacyInputFieldID = "api_key"
		if spec.platform == PlatformZhipu {
			method.Interaction.InputFields = append(method.Interaction.InputFields,
				gatewayConnectionField("organization", "Organization (optional)", "text", false, 128),
				gatewayConnectionField("project", "Project (optional)", "text", false, 128))
		}
		routing := p.Routing
		if routing == "" {
			routing = ProviderRoutingByInbound
		}
		profile := gatewayConnectionProfile(spec.id, spec.platform, spec.provider, spec.label, spec.product, spec.productLabel, "provider_"+string(routing), method)
		profile.AccountMode = spec.mode
		profiles = append(profiles, profile)
	}
	profiles = append(profiles,
		gatewayConnectionProfile("typesafe-systemone-apikey-v1", PlatformTypeSafe, "typesafe", "TypeSafe / Jev", "pay_as_you_go", "TypeSafe API", "typesafe_systemone", key),
		gatewayConnectionProfile(GatewayMiMoResponsesProfile, PlatformOpenAI, "mimo", "MiMo", "unknown", "MiMo", "mimo_responses", key),
		gatewayConnectionProfile(GatewayLegacyBridgeProfile, PlatformOpenAI, "mimo", "MiMo", "unknown", "MiMo", "mimo_chat_bridge", key),
		gatewayConnectionProfile(GatewayOpenRouterResponsesProfile, PlatformOpenAI, "openrouter", "OpenRouter", "pay_as_you_go", "OpenRouter credits", "openrouter_responses", key),
		gatewayConnectionProfile(GatewayNVIDIABuildChatProfile, PlatformOpenAI, "nvidia", "NVIDIA Build", "unknown", "NVIDIA API trial (evaluation only)", "nvidia_chat", key))
	for i := range profiles {
		p := &profiles[i]
		switch p.ProfileID {
		case GatewayCodexOAuthResponsesProfile:
			p.MetadataSources = []string{"verified_claims", "account_check", "credential", "training_settings", "subscription_status"}
		case GatewayClaudeOAuthMessagesProfile:
			p.MetadataSources = []string{"account_check", "credential"}
		case GatewayGrokOAuthResponsesProfile:
			p.MetadataSources = []string{"account_check", "credential", "plan_quota"}
		case GatewayGLMCodingResponsesProfile, GatewayMiniMaxMPlanResponsesProfile:
			p.MetadataSources = []string{"plan_quota"}
		case "gemini-google-one-responses-oauth-v1", "gemini-code-assist-responses-oauth-v1", "gemini-ai-studio-responses-oauth-v1":
			p.MetadataSources = []string{"account_check", "credential", "user_declaration"}
		}
		switch p.ProfileID {
		case GatewayCodexOAuthResponsesProfile, GatewayClaudeOAuthMessagesProfile:
			p.LimitsContract = GatewayAccountLimitsContract
		case GatewayGrokOAuthResponsesProfile, GatewayKimiCodeResponsesProfile, GatewayGLMCodingResponsesProfile, GatewayMiniMaxMPlanResponsesProfile:
			p.LimitsContract = GatewayAccountLimitsContractV2
		}
		switch p.ProfileID {
		case GatewayMiMoResponsesProfile, GatewayLegacyBridgeProfile:
			p.Region = GatewayAccountCapabilityLabel{ID: "unknown", Label: "Default"}
			p.UnavailableFallback = "qualified_anchor"
		case GatewayAnthropicAPIKeyProfile, GatewayGeminiAPIKeyProfile, GatewayNVIDIABuildChatProfile, GatewayCodexOAuthResponsesProfile, GatewayClaudeOAuthMessagesProfile, GatewayGrokOAuthResponsesProfile:
			p.UnavailableFallback = "qualified_anchor"
		}
	}
	return profiles
}
