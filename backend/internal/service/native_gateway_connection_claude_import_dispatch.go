package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
)

func NewGatewayNativeClaudeImportDispatcher(gateway *OpenAIGatewayService) (GatewayNativeConnectionDispatchAdapter, error) {
	if gateway == nil || gateway.httpUpstream == nil {
		return GatewayNativeConnectionDispatchAdapter{}, ErrGatewayNativeIdentity
	}
	return GatewayNativeConnectionDispatchAdapter{Forward: gateway.forwardGatewayNativeClaudeImport, ValidateRequest: gatewayClaudeImportRequestValid}, nil
}

func gatewayClaudeImportAccount(account *Account, selection GatewayConnectionSelection) (gatewayClaudeImportCredential, error) {
	var empty gatewayClaudeImportCredential
	if account == nil || account.Platform != PlatformAnthropic || selection.ProfileID != GatewayClaudeOAuthMessagesProfile {
		return empty, ErrGatewayNativeIdentity
	}
	expectedType := AccountTypeSetupToken
	switch selection.MethodID {
	case "cookie_oauth":
		expectedType = AccountTypeOAuth
	case "setup_token_code", "setup_token_cookie":
	default:
		return empty, ErrGatewayNativeIdentity
	}
	if account.Type != expectedType {
		return empty, ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(account.Credentials)
	if err != nil {
		return empty, ErrGatewayNativeIdentity
	}
	defer clear(raw)
	credential, err := gatewayClaudeImportDecode(raw, selection)
	if err != nil || !credential.ExpiresAt.After(time.Now()) {
		return empty, ErrGatewayNativeIdentity
	}
	return credential, nil
}

func gatewayClaudeImportRequestValid(request *http.Request, account *Account, native GatewayNativePublishedRoute, model string) bool {
	if request == nil || request.URL == nil || request.Method != http.MethodPost || native.Profile != GatewayClaudeOAuthMessagesProfile ||
		native.Publication.DispatchFamily != "claude_messages" || model != GatewayClaudeOAuthModel || request.URL.String() != gatewayClaudeMessagesURL {
		return false
	}
	credential, err := gatewayClaudeImportAccount(account, native.Publication.Connection)
	if err != nil {
		return false
	}
	attestation := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "none"}
	if credential.RefreshToken != "" {
		attestation.RefreshPolicy = "native"
	}
	return native.Publication.Credential == attestation && native.Publication.AuthFamily == "claude_oauth" &&
		request.Header.Get("Authorization") == "Bearer "+credential.AccessToken && request.Header.Get("Cookie") == "" && request.Header.Get("x-api-key") == "" &&
		request.Header.Get("anthropic-version") == "2023-06-01" && request.Header.Get("anthropic-beta") == strings.Join(claude.FullClaudeCodeMimicryBetas(), ",") &&
		claude.IsSupportedCLIVersion(ExtractCLIVersion(request.Header.Get("User-Agent")))
}

// The same bounded native Claude codec owns mimicry and response translation.
// The common upstream call supplies the final physical-row/entered fence.
func (s *OpenAIGatewayService) forwardGatewayNativeClaudeImport(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || c == nil || c.Request == nil || c.Writer == nil {
		return nil, ErrGatewayNativeIdentity
	}
	binding, valid := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	if !valid || route.Profile != GatewayClaudeOAuthMessagesProfile {
		return nil, ErrGatewayNativeIdentity
	}
	credential, err := gatewayClaudeImportAccount(account, binding.native.Publication.Connection)
	if err != nil {
		return nil, err
	}
	wire, headers, err := gatewayClaudePrepareResponses(body)
	if err != nil || int64(len(wire)) > binding.requestBytes {
		return nil, ErrGatewayNativeIdentity
	}
	upstreamCtx, cancel := context.WithCancel(WithHTTPUpstreamRedirectsDisabled(ctx))
	defer cancel()
	request, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, gatewayClaudeMessagesURL, bytes.NewReader(wire))
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", strings.Join(claude.FullClaudeCodeMimicryBetas(), ","))
	request.GetBody = nil
	observation := GatewayNativeUsage{Source: "native_provider_response", State: "unknown", CostState: "unknown"}
	defer func() {
		observation.Finalized = true
		if observe, ok := ctx.Value(gatewayNativeUsageObserverKey{}).(func(GatewayNativeUsage)); ok {
			observe(observation)
		}
	}()
	start := time.Now()
	response, err := s.doOpenAIUpstream(request, "", account)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil || response == nil || response.Body == nil {
		return nil, ErrGatewayNativeEffectUnknown
	}
	return gatewayNativeAnthropicResponse(ctx, c, response, GatewayClaudeOAuthModel, start, cancel, &observation, nil)
}
