package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/gin-gonic/gin"
)

// The established Gemini API-key profile retains its qualified OpenAI Chat
// endpoint. Only native Vertex and OAuth GenerateContent families enter here.
func NewGatewayNativeGeminiDispatchers(gateway *OpenAIGatewayService, transports ...http.RoundTripper) (map[string]GatewayNativeConnectionDispatchAdapter, error) {
	if gateway == nil || gateway.httpUpstream == nil {
		return nil, ErrGatewayNativeIdentity
	}
	client, err := newGatewayNativeCloudTokenClient(transports...)
	if err != nil {
		return nil, err
	}
	adapter := GatewayNativeConnectionDispatchAdapter{Forward: func(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
		return gateway.forwardGatewayNativeGemini(context.WithValue(ctx, gatewayNativeCloudTokenClientKey{}, client), c, account, route, body)
	}, ValidateRequest: gatewayGeminiRequestValid}
	return map[string]GatewayNativeConnectionDispatchAdapter{"vertex_gemini": adapter, "gemini_oauth": adapter}, nil
}

func NewGatewayNativeAntigravityDispatchers(gateway *OpenAIGatewayService) (map[string]GatewayNativeConnectionDispatchAdapter, error) {
	if gateway == nil || gateway.httpUpstream == nil {
		return nil, ErrGatewayNativeIdentity
	}
	return map[string]GatewayNativeConnectionDispatchAdapter{"antigravity": {Forward: gateway.forwardGatewayNativeGemini, ValidateRequest: gatewayGeminiRequestValid}}, nil
}

func gatewayGeminiTarget(account *Account, native GatewayNativePublishedRoute, model string) (string, bool, error) {
	if account == nil || account.Platform != PlatformGemini && account.Platform != PlatformAntigravity || model != native.Model || !GatewayAccountModelIDValid(model) {
		return "", false, ErrGatewayNativeIdentity
	}
	switch native.Publication.DispatchFamily {
	case "antigravity":
		if account.Platform != PlatformAntigravity {
			return "", false, ErrGatewayNativeIdentity
		}
		if account.Type == AccountTypeAPIKey {
			if native.Profile != "antigravity-target-responses-apikey-v1" || native.Publication.AuthFamily != "antigravity_target_api_key" ||
				native.Publication.Credential != (GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"}) ||
				account.GetCredential("base_url") != native.BaseURL || !GatewayNativeIngressKeyValid(account.GetCredential("api_key")) {
				return "", false, ErrGatewayNativeIdentity
			}
			target, err := buildGeminiAIStudioModelActionURL(account.GetGeminiBaseURL(""), model, "streamGenerateContent", true)
			return target, false, err
		}
		if native.Profile != "antigravity-responses-oauth-v1" || account.Type != AccountTypeOAuth || native.Publication.AuthFamily != "antigravity_oauth" ||
			native.Publication.Credential.CredentialKind != "oauth" || native.Publication.Credential.CredentialPolicy != "subscription_session" ||
			!antigravity.OfficialBaseURL(native.BaseURL) || !gatewayCloudProject.MatchString(account.GetCredential("project_id")) || account.GetCredential("oauth_type") != "antigravity" {
			return "", false, ErrGatewayNativeIdentity
		}
		return native.BaseURL + "/v1internal:streamGenerateContent?alt=sse", true, nil
	case "vertex_gemini":
		if account.Platform != PlatformGemini || !account.IsVertexServiceAccount() || !gatewayCloudMountValid("vertex_gemini", GatewayConnectionMount{BaseURL: native.BaseURL, Model: native.Model, QualificationRef: native.Publication.Connection.ConfigurationGeneration}) {
			return "", false, ErrGatewayNativeIdentity
		}
		target, err := buildVertexGeminiAccountURL(account, model, "streamGenerateContent", true, "")
		return target, false, err
	case "gemini_oauth":
		oauthType := gatewayGeminiOAuthType(native.Profile)
		if account.Platform != PlatformGemini || account.Type != AccountTypeOAuth || oauthType == "" || account.GetCredential("oauth_type") != oauthType ||
			native.Publication.AuthFamily != "gemini_"+oauthType+"_oauth" || native.Publication.Credential.CredentialKind != "oauth" ||
			native.Publication.Credential.CredentialPolicy != "subscription_session" {
			return "", false, ErrGatewayNativeIdentity
		}
		project := account.GetCredential("project_id")
		if project != "" {
			if !gatewayCloudProject.MatchString(project) || native.BaseURL != geminicli.GeminiCliBaseURL {
				return "", false, ErrGatewayNativeIdentity
			}
			return geminicli.GeminiCliBaseURL + "/v1internal:streamGenerateContent?alt=sse", true, nil
		}
		if oauthType != "ai_studio" || native.BaseURL != geminicli.AIStudioBaseURL {
			return "", false, ErrGatewayNativeIdentity
		}
		target, err := buildGeminiAIStudioModelActionURL(geminicli.AIStudioBaseURL, model, "streamGenerateContent", true)
		return target, false, err
	}
	return "", false, ErrGatewayNativeIdentity
}

func gatewayGeminiRequestValid(request *http.Request, account *Account, native GatewayNativePublishedRoute, model string) bool {
	if request == nil || request.URL == nil || request.Method != http.MethodPost || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Fragment != "" ||
		request.Header.Get("Cookie") != "" || request.Header.Get("x-api-key") != "" {
		return false
	}
	target, _, err := gatewayGeminiTarget(account, native, model)
	if err != nil || request.URL.String() != target {
		return false
	}
	if native.Publication.DispatchFamily == "antigravity" && account.Type == AccountTypeAPIKey {
		return request.Header.Get("x-goog-api-key") == account.GetCredential("api_key") && request.Header.Get("Authorization") == ""
	}
	if native.Publication.DispatchFamily == "gemini_oauth" || native.Publication.DispatchFamily == "antigravity" {
		return gatewayCloudSecretValid(account.GetCredential("access_token"), 32768, true) && request.Header.Get("Authorization") == "Bearer "+account.GetCredential("access_token") && request.Header.Get("x-goog-api-key") == ""
	}
	if native.Publication.Credential.CredentialPolicy != "cloud_identity" {
		return false
	}
	if account.IsVertexAPIKey() {
		return native.Publication.AuthFamily == "vertex_api_key" && native.Publication.Credential.CredentialKind == "api_key" &&
			GatewayNativeIngressKeyValid(account.GetCredential("api_key")) && request.Header.Get("x-goog-api-key") == account.GetCredential("api_key") && request.Header.Get("Authorization") == ""
	}
	bearer := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	return native.Publication.AuthFamily == "google_service_account" && native.Publication.Credential.CredentialKind == "native" &&
		request.Header.Get("x-goog-api-key") == "" && strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") && gatewayCloudSecretValid(bearer, 32768, true)
}

func (s *OpenAIGatewayService) forwardGatewayNativeGemini(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || c == nil || c.Request == nil {
		return nil, ErrGatewayNativeIdentity
	}
	binding, valid := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	if !valid {
		return nil, ErrGatewayNativeIdentity
	}
	if _, _, err := gatewayNativePrepareProviderResponses(route.Profile, body, binding.requestBytes); err != nil {
		return nil, err
	}
	var input apicompat.ResponsesRequest
	if json.Unmarshal(body, &input) != nil {
		return nil, ErrGatewayNativeIdentity
	}
	tools, err := apicompat.EffectiveResponsesTools(&input)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	chat, err := apicompat.ResponsesToChatCompletionsRequestWithOptions(&input, &apicompat.ResponsesToChatOptions{})
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	codec, err := gatewayNativeGeminiContinuation(c)
	if err != nil || codec == nil || apicompat.RestoreGeminiContinuation(input.Input, chat, codec) != nil {
		return nil, ErrGatewayNativeIdentity
	}
	wire, err := apicompat.ChatCompletionsToGeminiGenerateContent(chat)
	if err != nil || int64(len(wire)) > binding.requestBytes {
		return nil, ErrGatewayNativeIdentity
	}
	target, wrapped, err := gatewayGeminiTarget(account, binding.native, input.Model)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	if wrapped {
		if account.Platform == PlatformAntigravity {
			wire, err = antigravity.WrapGenerateContent(wire, account.GetCredential("project_id"), input.Model)
		} else {
			wire, err = json.Marshal(map[string]any{"model": input.Model, "project": account.GetCredential("project_id"), "request": json.RawMessage(wire)})
		}
		if err != nil || int64(len(wire)) > binding.requestBytes {
			return nil, ErrGatewayNativeIdentity
		}
	}
	upstreamCtx, cancel := context.WithCancel(WithHTTPUpstreamRedirectsDisabled(ctx))
	defer cancel()
	request, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, target, bytes.NewReader(wire))
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Accept-Encoding", "identity")
	if account.IsVertexAPIKey() || account.Platform == PlatformAntigravity && account.Type == AccountTypeAPIKey {
		request.Header.Set("x-goog-api-key", account.GetCredential("api_key"))
	} else {
		bearer := account.GetCredential("access_token")
		if account.IsVertexServiceAccount() {
			bearer, err = getVertexServiceAccountAccessToken(upstreamCtx, nil, account)
		} else {
			expiry, parseErr := strconv.ParseInt(account.GetCredential("expires_at"), 10, 64)
			if parseErr != nil || !time.Now().Before(time.Unix(expiry, 0)) {
				return nil, ErrGatewayNativeIdentity
			}
		}
		if err != nil || !gatewayCloudSecretValid(bearer, 32768, true) {
			return nil, ErrGatewayNativeIdentity
		}
		request.Header.Set("Authorization", "Bearer "+bearer)
		if wrapped {
			if account.Platform == PlatformAntigravity {
				request.Header.Set("User-Agent", antigravity.GetUserAgentForContext(ctx))
			} else {
				request.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)
			}
		}
	}
	start := time.Now()
	response, err := s.doOpenAIUpstream(request, "", account)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil || response.Body == nil || response.StatusCode != http.StatusOK ||
		strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]) != "text/event-stream" ||
		(response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		return nil, ErrGatewayNativeEffectUnknown
	}
	response.Body = newGatewayGeminiGenerateBody(response.Body, input.Model, wrapped, cancel)
	result, err := s.streamChatCompletionsAsResponses(c, response, input.Model, apicompat.CustomToolNames(tools), apicompat.FunctionToolNames(tools),
		apicompat.HasToolSearchTool(tools), apicompat.NamespaceToolNames(tools), input.Model, input.Model, nil, nil, start)
	if result != nil {
		result.UpstreamEndpoint = request.URL.Path
	}
	return result, err
}

// GenerateContent signals completion with its native finishReason and clean
// EOF. A bounded reader supplies the existing strict Chat reducer only after
// every original frame has passed parsing; malformed/truncated tails never
// become a fabricated Chat [DONE]. No goroutine buffers the provider response.
type gatewayGeminiGenerateBody struct {
	source  io.ReadCloser
	scanner *bufio.Scanner
	state   *apicompat.GeminiGenerateStream
	wrapped bool
	pending *bytes.Reader
	data    strings.Builder
	bytes   int
	frame   int
	done    bool
}

func newGatewayGeminiGenerateBody(source io.ReadCloser, model string, wrapped bool, cancel func()) *gatewayGeminiGenerateBody {
	idle := &gatewayNativeIdleReader{reader: source, idle: GatewayNativeProviderReadIdle, cancel: cancel}
	scanner := bufio.NewScanner(idle)
	scanner.Split(gatewayNativeRawSSELine)
	scanner.Buffer(make([]byte, 4096), gatewayNativeSSEFrameLimit+1)
	return &gatewayGeminiGenerateBody{source: source, scanner: scanner, state: apicompat.NewGeminiGenerateStream(model), wrapped: wrapped}
}

func (r *gatewayGeminiGenerateBody) GeminiNativeParts() []apicompat.GeminiGeneratePart {
	return r.state.NativeParts()
}
func (r *gatewayGeminiGenerateBody) Close() error { return r.source.Close() }

func (r *gatewayGeminiGenerateBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.pending != nil && r.pending.Len() > 0 {
		return r.pending.Read(p)
	}
	if r.done {
		return 0, io.EOF
	}
	for r.scanner.Scan() {
		line := r.scanner.Text()
		r.bytes += len(line)
		r.frame += len(line)
		if r.bytes > gatewayNativeResponseLimit || r.frame > gatewayNativeSSEFrameLimit || !utf8.ValidString(line) || !strings.HasSuffix(line, "\n") {
			return 0, ErrGatewayNativeEffectUnknown
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line != "" {
			if strings.HasPrefix(line, "data:") {
				if r.data.Len() > 0 {
					r.data.WriteByte('\n')
				}
				r.data.WriteString(strings.TrimPrefix(line[5:], " "))
			}
			continue
		}
		r.frame = 0
		if r.data.Len() == 0 {
			continue
		}
		raw := []byte(r.data.String())
		r.data.Reset()
		if r.wrapped {
			fields, valid := GatewayNativePayloadFields(raw, "response")
			if !valid || fields["response"] == nil {
				return 0, ErrGatewayNativeEffectUnknown
			}
			raw = fields["response"]
		}
		chunk, err := r.state.Push(raw)
		if err != nil {
			return 0, ErrGatewayNativeEffectUnknown
		}
		if len(chunk.Choices) == 0 && chunk.Usage == nil {
			continue
		}
		encoded, err := json.Marshal(chunk)
		if err != nil {
			return 0, ErrGatewayNativeEffectUnknown
		}
		r.pending = bytes.NewReader(append(append([]byte("data: "), encoded...), '\n', '\n'))
		return r.pending.Read(p)
	}
	if r.scanner.Err() != nil || r.data.Len() != 0 || r.frame != 0 || !r.state.Finished {
		return 0, ErrGatewayNativeEffectUnknown
	}
	r.done = true
	r.pending = bytes.NewReader([]byte("data: [DONE]\n\n"))
	return r.pending.Read(p)
}
