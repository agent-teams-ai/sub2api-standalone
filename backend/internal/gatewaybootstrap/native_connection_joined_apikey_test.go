//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeConnectionJoinedProfile(profile, base, model string) gatewaytransport.QualifiedProfile {
	return gatewaytransport.QualifiedProfile{Profile: profile, BaseURL: base, Model: model, QualificationRef: "joined-connections",
		RequestBytes: 1 << 20, OutputBytes: 1 << 20, Tokens: 16384, ProviderTokenUpperBound: 16384}
}

// The cases select actual native defaults or the existing qualified anchor.
// Runtime native code, not these expected source paths, selects the protocol.
func newNativeConnectionJoinedAPIKeyFamily(t *testing.T, _ *Config) nativeConnectionJoinedFamily {
	t.Helper()
	type specification struct{ profile, base, model, protocol string }
	specs := []specification{
		{service.GatewayAnthropicAPIKeyProfile, service.GatewayAnthropicAPIBaseURL, service.GatewayAnthropicAPIModel, "anthropic"},
		{service.GatewayGeminiAPIKeyProfile, service.GatewayGeminiAPIBaseURL, service.GatewayGeminiAPIModel, "chat_completions"},
		{"openai-api-responses-apikey-v1", "https://api.openai.com", "gpt-4.1", "responses"},
		{"grok-api-responses-apikey-v1", "https://api.x.ai", "grok-4", "responses"},
		{"kimi-payg-responses-apikey-v1", service.DefaultKimiPayGBaseURL, "k3-256k", "responses"},
		{service.GatewayKimiCodeResponsesProfile, service.GatewayKimiCodeResponsesBaseURL, service.GatewayKimiCodeResponsesModel, "responses"},
		{"glm-payg-responses-apikey-v1", service.DefaultZhipuPayGBaseURL, "glm-5.3-flash", "chat_completions"},
		{service.GatewayGLMCodingResponsesProfile, service.GatewayGLMCodingResponsesBaseURL, service.GatewayGLMCodingResponsesModel, "chat_completions"},
		{"deepseek-responses-apikey-v1", service.DefaultDeepseekBaseURL, "deepseek-chat", "responses"},
		{"minimax-payg-responses-apikey-v1", service.DefaultMiniMaxBaseURL, "MiniMax-M3.1-Flash", "responses"},
		{service.GatewayMiniMaxMPlanResponsesProfile, service.GatewayMiniMaxMPlanResponsesBaseURL, service.GatewayMiniMaxMPlanResponsesModel, "responses"},
		{"opencode-go-responses-apikey-v1", service.DefaultOpenCodeGoBaseURL, "gpt-4.1", "responses"},
		{"opencode-zen-responses-apikey-v1", service.DefaultOpenCodeZenBaseURL, "gpt-4.1", "responses"},
		{"command-code-responses-apikey-v1", service.DefaultCommandCodeBaseURL, "gpt-4.1", "responses"},
		{"cline-chat-apikey-v1", service.DefaultClineBaseURL, service.DefaultClineTestModel, "chat_completions"},
		{service.GatewayMiMoResponsesProfile, "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "responses"},
		{service.GatewayOpenRouterResponsesProfile, service.GatewayOpenRouterBaseURL, service.GatewayOpenRouterModel, "responses"},
		{service.GatewayNVIDIABuildChatProfile, service.GatewayNVIDIABuildBaseURL, service.GatewayNVIDIABuildAnchorModel, "chat_completions"},
		// This row is retained as a required, typed blocker. Its source protocol
		// cannot be qualified as Responses simply by mounting a credential form.
		{"typesafe-systemone-apikey-v1", typesafe.DefaultBaseURL, typesafe.JevLatestModel, "systemone"},
	}
	family := nativeConnectionJoinedFamily{}
	for _, spec := range specs {
		definition, ok := service.LookupGatewayConnectionProfile(spec.profile)
		require.True(t, ok)
		profile := nativeConnectionJoinedProfile(spec.profile, spec.base, spec.model)
		family.Profiles = append(family.Profiles, profile)
		u, err := url.Parse(spec.base)
		require.NoError(t, err)
		family.Hosts = append(family.Hosts, u.Hostname()+":443")
		secret, replacement := "joined-key-"+uuid.NewString(), "joined-key-"+uuid.NewString()
		fields, reconnect := map[string]string{"api_key": secret}, map[string]string{"api_key": replacement}
		if definition.Platform == service.PlatformZhipu {
			fields["organization"], fields["project"] = "joined-organization", "joined-project"
			reconnect["organization"], reconnect["project"] = fields["organization"], fields["project"]
		}
		base := strings.TrimRight(spec.base, "/")
		suffix := "/v1/responses"
		if spec.protocol == "chat_completions" {
			suffix = "/v1/chat/completions"
		} else if spec.protocol == "anthropic" {
			suffix = "/v1/messages"
		}
		if strings.HasSuffix(base, "/v1") || spec.profile == service.GatewayGeminiAPIKeyProfile || spec.profile == "glm-payg-responses-apikey-v1" {
			suffix = strings.TrimPrefix(suffix, "/v1")
		}
		if spec.profile == "deepseek-responses-apikey-v1" {
			suffix = "/responses"
		}
		endpoint := base + suffix
		modelsURL := base + "/models"
		var catalogs atomic.Int64
		peer := &nativeConnectionJoinedPeer{Protocol: spec.protocol, Row: nativeConnectionJoinedRow{
			ID: nativeConnectionJoinedRowID(spec.profile, "api_key", "provider_api_key"), ProfileID: spec.profile, MethodID: "api_key", Variant: "provider_api_key",
			Fields: fields, ReconnectFields: reconnect, CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none",
			Provider: definition.Provider.ID, Model: spec.model, ExecutionFamily: definition.DispatchFamily, Execute: spec.protocol != "systemone",
		}}
		if spec.protocol == "systemone" {
			peer.Row.Blocker = "native_systemone_protocol"
		}
		if spec.profile == service.GatewayOpenRouterResponsesProfile {
			peer.Row.ReviewProof = &nativeConnectionJoinedReviewProof{ReasoningEffort: "medium", MaxOutputTokens: 8192}
		}
		peer.InferenceAuth = func(r *http.Request) bool {
			if r.Method != http.MethodPost || "https://"+r.Host+r.URL.RequestURI() != endpoint || r.Header.Get("Cookie") != "" || r.Header.Get("X-Codex-Turn-State") != "" {
				return false
			}
			if spec.protocol == "anthropic" {
				key := r.Header.Get("x-api-key")
				return (key == secret || key == replacement) && r.Header.Get("Authorization") == "" && r.Header.Get("anthropic-version") == "2023-06-01"
			}
			auth := r.Header.Get("Authorization")
			return (auth == "Bearer "+secret || auth == "Bearer "+replacement) && r.Header.Get("x-api-key") == ""
		}
		peer.Handle = func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method != http.MethodGet || "https://"+r.Host+r.URL.RequestURI() != modelsURL {
				return false
			}
			public := spec.profile == service.GatewayOpenRouterResponsesProfile || spec.profile == service.GatewayNVIDIABuildChatProfile
			auth := r.Header.Get("Authorization")
			if public && auth != "" || !public && auth != "Bearer "+secret && auth != "Bearer "+replacement {
				w.WriteHeader(http.StatusForbidden)
				return true
			}
			catalogs.Add(1)
			w.Header().Set("Content-Type", "application/json")
			model := map[string]any{"id": spec.model,
				"architecture":         map[string]any{"input_modalities": []string{"text"}, "output_modalities": []string{"text"}},
				"supported_parameters": []string{"tools", "tool_choice", "response_format", "structured_outputs"}}
			if spec.profile == service.GatewayOpenRouterResponsesProfile {
				// Exact reasoning controls advertised for this anchor by
				// https://openrouter.ai/api/v1/models on 2026-11-21. The native
				// model reader still owns the projection and request admission.
				model["supported_parameters"] = []string{"include_reasoning", "max_completion_tokens", "max_tokens", "reasoning", "reasoning_effort", "response_format", "seed", "structured_outputs", "tool_choice", "tools", "verbosity"}
				model["reasoning"] = map[string]any{"mandatory": false, "default_enabled": false,
					"supported_efforts": []string{"xhigh", "high", "medium", "low", "none"}, "default_effort": "medium"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{model}})
			return true
		}
		peer.Counters = func() map[string]int64 { return map[string]int64{"catalogs": catalogs.Load()} }
		peer.Verify = func() {
			switch spec.profile {
			case service.GatewayOpenRouterResponsesProfile, service.GatewayNVIDIABuildChatProfile,
				service.GatewayKimiCodeResponsesProfile, service.GatewayGLMCodingResponsesProfile, service.GatewayMiniMaxMPlanResponsesProfile:
				require.Positive(t, catalogs.Load(), "native source catalog must be observed for %s", spec.profile)
			}
		}
		family.Peers = append(family.Peers, peer)
	}
	family.Compose = func(b nativeConnectionJoinedBuild) ([]service.GatewayNativeConnectionMethodAdapter, map[string]service.GatewayNativeConnectionDispatchAdapter, error) {
		dispatchers, err := service.NewGatewayNativeAPIKeyDispatchers(b.Gateway)
		if err != nil {
			return nil, nil, err
		}
		// The subscription family wraps both native Grok credential branches.
		delete(dispatchers, "grok_responses")
		methods := []service.GatewayNativeConnectionMethodAdapter{}
		for _, p := range family.Profiles {
			if p.Profile == "typesafe-systemone-apikey-v1" {
				continue
			}
			generation, err := connectionConfigurationGeneration(*b.Config, p)
			if err != nil {
				return nil, nil, err
			}
			method, err := service.NewGatewayNativeAPIKeyConnectionMethod(p.Profile, generation, service.GatewayConnectionMount{BaseURL: p.BaseURL, Model: p.Model, QualificationRef: p.QualificationRef}, b.Gateway)
			if err != nil {
				return nil, nil, err
			}
			methods = append(methods, method)
		}
		return methods, dispatchers, nil
	}
	return family
}
