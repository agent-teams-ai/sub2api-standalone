package service

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

// The private connection chooses the same native protocol resolver and request
// codecs as a normal account. Only its credential lifetime and effect fence
// differ; a provider declaration alone never grants dispatch authority.
func NewGatewayNativeAPIKeyDispatchers(gateway *OpenAIGatewayService) (map[string]GatewayNativeConnectionDispatchAdapter, error) {
	if gateway == nil || gateway.httpUpstream == nil {
		return nil, ErrGatewayNativeIdentity
	}
	out := make(map[string]GatewayNativeConnectionDispatchAdapter)
	for _, family := range []string{"anthropic_messages", "gemini_chat", "openai_responses", "grok_responses", "provider_by_inbound", "provider_by_model", "mimo_responses", "mimo_chat_bridge", "openrouter_responses", "nvidia_chat"} {
		out[family] = GatewayNativeConnectionDispatchAdapter{Forward: gateway.forwardGatewayNativeAPIKeyConnection, ValidateRequest: gateway.gatewayNativeAPIKeyConnectionRequestValid}
	}
	return out, nil
}

func (s *OpenAIGatewayService) gatewayNativeAPIKeyConnectionTarget(account *Account, profile, model string) (string, string, error) {
	definition, found := LookupGatewayConnectionProfile(profile)
	if !found || account == nil || account.Type != AccountTypeAPIKey || account.Platform != definition.Platform || !GatewayAccountModelIDValid(model) {
		return "", "", ErrGatewayNativeIdentity
	}
	protocol := APIProtocolResponses
	switch definition.DispatchFamily {
	case "anthropic_messages":
		return APIProtocolAnthropic, GatewayAnthropicAPIBaseURL + "/messages", nil
	case "gemini_chat":
		return APIProtocolChatCompletions, GatewayGeminiAPIBaseURL + "/chat/completions", nil
	case "provider_by_inbound", "provider_by_model":
		// The exact native provider mode and protocol bases were sealed during
		// import. Unknown catalogue protocols keep native's static resolution;
		// no global mutable cache or detached credential request is consulted.
		protocol = resolveUpstreamProtocol(account, APIProtocolResponses, model, nil)
	case "mimo_chat_bridge", "nvidia_chat":
		protocol = APIProtocolChatCompletions
	case "openai_responses", "grok_responses", "mimo_responses", "openrouter_responses":
	default:
		return "", "", ErrGatewayNativeIdentity
	}
	if protocol == APIProtocolAnthropic {
		target, err := s.nativeAnthropicTargetURL(account)
		return protocol, target, err
	}
	base := account.GetOpenAIBaseURL()
	if account.IsMultiProtocolAPIKey() {
		base = account.GetCNProtocolBaseURL(protocol)
	} else if account.Platform == PlatformGrok {
		base = account.GetGrokBaseURL()
	}
	validated, err := s.validateUpstreamBaseURL(base)
	if err != nil {
		return "", "", ErrGatewayNativeIdentity
	}
	if protocol == APIProtocolChatCompletions {
		return protocol, buildOpenAIChatCompletionsURL(validated), nil
	}
	return protocol, buildOpenAIResponsesURLForPlatform(account.Platform, validated), nil
}

func (s *OpenAIGatewayService) gatewayNativeAPIKeyConnectionRequestValid(request *http.Request, account *Account, native GatewayNativePublishedRoute, model string) bool {
	if request == nil || request.URL == nil || request.Method != http.MethodPost || account == nil ||
		native.Publication.Credential != (GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"}) ||
		native.Publication.AuthFamily != "provider_api_key" || !GatewayNativeIngressKeyValid(account.GetCredential("api_key")) ||
		request.Header.Get("Cookie") != "" || request.Header.Get("x-goog-api-key") != "" {
		return false
	}
	protocol, target, err := s.gatewayNativeAPIKeyConnectionTarget(account, native.Profile, model)
	if err != nil || request.URL.String() != target {
		return false
	}
	if protocol == APIProtocolAnthropic {
		// Use the existing native choice of bearer versus x-api-key. The fresh
		// publication check owns credentials and metadata before this boundary.
		headers := make(http.Header)
		setAnthropicAPIKeyAuthHeader(headers, account, account.GetCredential("api_key"), account.GetAnthropicProtocolBaseURL())
		return request.Header.Get("Authorization") == headers.Get("Authorization") && request.Header.Get("x-api-key") == headers.Get("x-api-key") &&
			request.Header.Get("anthropic-version") == "2023-06-01" && request.Header.Get("anthropic-beta") == ""
	}
	return request.Header.Get("Authorization") == "Bearer "+account.GetCredential("api_key") && request.Header.Get("x-api-key") == ""
}

func (s *OpenAIGatewayService) forwardGatewayNativeAPIKeyConnection(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || c == nil || c.Request == nil {
		return nil, ErrGatewayNativeIdentity
	}
	binding, valid := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	fields, structural := GatewayNativePayloadFields(body, "model")
	var model string
	if !valid || !structural || json.Unmarshal(fields["model"], &model) != nil {
		return nil, ErrGatewayNativeIdentity
	}
	protocol, target, err := s.gatewayNativeAPIKeyConnectionTarget(account, route.Profile, model)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	canonical, mapping, err := gatewayNativeAPIKeyConnectionBody(route.Profile, body, binding.requestBytes)
	if err != nil {
		return nil, err
	}
	if route.Profile == GatewayOpenRouterResponsesProfile {
		state, _ := ctx.Value(gatewayConnectionDispatchStateKey{}).(*gatewayConnectionDispatchState)
		if state == nil || s.gatewayOpenRouterModels == nil || !gatewayOpenRouterModelsAllow(s.gatewayOpenRouterModels.read(ctx,
			gatewayConnectionOpenRouterPrincipal(state.physical), false), route.Model, model, canonical) {
			return nil, ErrGatewayNativeIdentity
		}
	}
	if protocol == APIProtocolChatCompletions {
		// The Chat codec owns client-tool lowering and reverse names. Applying
		// the lowering twice loses custom-tool and namespace identity.
		return s.forwardResponsesViaRawChatCompletions(ctx, c, account, body)
	}
	setGrokResponsesClientToolMapping(c, mapping)
	defer clearGrokResponsesClientToolMapping(c)
	if protocol == APIProtocolAnthropic {
		return s.forwardGatewayNativeAnthropicAPI(ctx, c, account, canonical, target, model, true)
	}
	return s.forwardGatewayNativeResponses(ctx, c, account, canonical)
}

func gatewayNativeAPIKeyConnectionBody(profile string, body []byte, limit int64) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	if profile != GatewayOpenRouterResponsesProfile {
		return gatewayNativePrepareProviderResponses(profile, body, limit)
	}
	fields, valid := gatewayOpenRouterResponsesFields(body)
	if !valid || int64(len(body)) > limit || !gatewayNativeProviderTextInput(profile, fields["input"]) || fields["tools"] != nil && !gatewayNativeProviderTools(fields["tools"]) {
		return nil, apicompat.ResponsesClientToolMapping{}, ErrGatewayNativeIdentity
	}
	canonical, mapping, err := adaptResponsesClientToolsForFunctionUpstream(body, "OpenRouter")
	if err != nil || int64(len(canonical)) > limit || !gatewayNativeProviderNamespaceReferences(fields["input"], fields["tool_choice"], mapping) {
		return nil, apicompat.ResponsesClientToolMapping{}, ErrGatewayNativeIdentity
	}
	return canonical, mapping, nil
}
