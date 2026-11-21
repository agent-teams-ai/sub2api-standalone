package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// These adapters reuse the native signing, cloud request shaping, EventStream
// decoder and strict Messages-to-Responses consumer. Only the existing owned
// upstream can make the inference request, through its final physical fence.
func NewGatewayNativeCloudDispatchers(gateway *OpenAIGatewayService) (map[string]GatewayNativeConnectionDispatchAdapter, error) {
	if gateway == nil || gateway.httpUpstream == nil {
		return nil, ErrGatewayNativeIdentity
	}
	return map[string]GatewayNativeConnectionDispatchAdapter{
		"bedrock_messages": {Forward: gateway.forwardGatewayNativeCloud, ValidateRequest: gatewayCloudRequestValid},
		"vertex_messages":  {Forward: gateway.forwardGatewayNativeCloud, ValidateRequest: gatewayCloudRequestValid},
	}, nil
}

func gatewayCloudRequestValid(request *http.Request, account *Account, native GatewayNativePublishedRoute, model string) bool {
	if request == nil || request.URL == nil || account == nil || request.Method != http.MethodPost ||
		request.URL.User != nil || request.URL.Fragment != "" || request.URL.Scheme != "https" || request.URL.Port() != "" ||
		native.Publication.Credential.CredentialPolicy != "cloud_identity" || model != native.Model {
		return false
	}
	switch native.Publication.DispatchFamily {
	case "bedrock_messages":
		if !account.IsBedrock() || !gatewayCloudAWSRegion.MatchString(bedrockRuntimeRegion(account)) {
			return false
		}
		upstreamModel, valid := ResolveBedrockModelID(account, model)
		if !valid || request.URL.String() != BuildBedrockURL(bedrockRuntimeRegion(account), upstreamModel, true) {
			return false
		}
		if account.IsBedrockAPIKey() {
			return native.Publication.AuthFamily == "bedrock_api_key" && request.Header.Get("Authorization") == "Bearer "+account.GetCredential("api_key")
		}
		return native.Publication.AuthFamily == "aws_sigv4" &&
			strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential="+account.GetCredential("aws_access_key_id")+"/") &&
			request.Header.Get("X-Amz-Security-Token") == account.GetCredential("aws_session_token")
	case "vertex_messages":
		if account.Platform != PlatformAnthropic || !account.IsVertexServiceAccount() || native.Publication.AuthFamily != "google_service_account" {
			return false
		}
		upstreamModel := normalizeVertexAnthropicModelID(model)
		endpoint, err := buildVertexAnthropicURL(account.VertexProjectID(), account.VertexLocation(upstreamModel), upstreamModel, true)
		bearer := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		return err == nil && request.URL.String() == endpoint &&
			strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") && gatewayCloudSecretValid(bearer, 32768, true)
	}
	return false
}

func gatewayCloudAnthropicRequest(ctx context.Context, account *Account, wire []byte, model string) (*http.Request, string, error) {
	var endpoint, upstreamModel, bearer string
	var signer *BedrockSigner
	var err error
	if account.IsBedrock() {
		var valid bool
		upstreamModel, valid = ResolveBedrockModelID(account, model)
		if !valid || !gatewayCloudAWSRegion.MatchString(bedrockRuntimeRegion(account)) {
			return nil, "", ErrGatewayNativeIdentity
		}
		wire, err = PrepareBedrockRequestBody(wire, upstreamModel, "")
		endpoint = BuildBedrockURL(bedrockRuntimeRegion(account), upstreamModel, true)
		if account.IsBedrockAPIKey() {
			bearer = account.GetCredential("api_key")
		} else if err == nil {
			signer, err = NewBedrockSignerFromAccount(account)
		}
	} else if account.Platform == PlatformAnthropic && account.IsVertexServiceAccount() {
		upstreamModel = normalizeVertexAnthropicModelID(model)
		endpoint, err = buildVertexAnthropicURL(account.VertexProjectID(), account.VertexLocation(upstreamModel), upstreamModel, true)
		if err == nil {
			wire, err = buildVertexAnthropicRequestBody(wire)
		}
		if err == nil {
			bearer, err = getVertexServiceAccountAccessToken(ctx, nil, account)
		}
	} else {
		return nil, "", ErrGatewayNativeIdentity
	}
	binding, valid := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	if err != nil || !valid || int64(len(wire)) > binding.requestBytes || signer == nil && !gatewayCloudSecretValid(bearer, 32768, true) {
		return nil, "", ErrGatewayNativeIdentity
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(wire))
	if err != nil {
		return nil, "", ErrGatewayNativeIdentity
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	if signer != nil {
		request.Header.Set("Accept", "application/json")
		if signer.SignRequest(ctx, request, wire) != nil {
			return nil, "", ErrGatewayNativeIdentity
		}
	} else {
		request.Header.Set("Accept", "text/event-stream")
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	return request, upstreamModel, nil
}

func (s *OpenAIGatewayService) forwardGatewayNativeCloud(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || c == nil || c.Request == nil {
		return nil, ErrGatewayNativeIdentity
	}
	binding, valid := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	if !valid {
		return nil, ErrGatewayNativeIdentity
	}
	fields, valid := GatewayNativePayloadFields(body, "model")
	var requestedModel string
	if !valid || json.Unmarshal(fields["model"], &requestedModel) != nil || requestedModel != route.Model {
		return nil, ErrGatewayNativeIdentity
	}
	canonical, mapping, err := gatewayNativePrepareProviderResponses(route.Profile, body, binding.requestBytes)
	if err != nil {
		return nil, err
	}
	wire, validate, err := gatewayNativePrepareAnthropicAPI(canonical)
	if err != nil {
		return nil, err
	}
	setGrokResponsesClientToolMapping(c, mapping)
	upstreamCtx, cancel := context.WithCancel(WithHTTPUpstreamRedirectsDisabled(ctx))
	defer cancel()
	request, upstreamModel, err := gatewayCloudAnthropicRequest(upstreamCtx, account, wire, route.Model)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	observation := GatewayNativeUsage{Source: "native_provider_response", State: "unknown", CostState: "unknown"}
	defer func() {
		observation.Finalized = true
		if observe, ok := ctx.Value(gatewayNativeUsageObserverKey{}).(func(GatewayNativeUsage)); ok {
			observe(observation)
		}
	}()
	response, err := s.doOpenAIUpstream(request, "", account)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil || response == nil || response.Body == nil {
		return nil, ErrGatewayNativeEffectUnknown
	}
	responseModel := upstreamModel
	if account.IsBedrock() {
		if response.StatusCode != http.StatusOK || strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]) != "application/vnd.amazon.eventstream" ||
			response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
			return nil, ErrGatewayNativeEffectUnknown
		}
		idle := &gatewayNativeIdleReader{reader: response.Body, idle: GatewayNativeProviderReadIdle, cancel: cancel}
		response.Body = &gatewayCloudBedrockBody{source: response.Body, decoder: newBedrockEventStreamDecoder(io.LimitReader(idle, gatewayNativeResponseLimit+1))}
		response.Header = response.Header.Clone()
		response.Header.Set("Content-Type", "text/event-stream")
		// Bedrock's message model is the Claude model, not its AWS inference
		// profile ID. The resolved AWS model remains separate usage context.
		responseModel = route.Model
	}
	result, err := gatewayNativeAnthropicResponse(ctx, c, response, responseModel, start, cancel, &observation, validate)
	if result != nil {
		result.Model, result.UpstreamModel, result.UpstreamEndpoint = route.Model, upstreamModel, request.URL.Path
	}
	return result, err
}

// The existing AWS decoder validates CRCs and frame bounds. This small reader
// only supplies its Messages JSON to the same strict SSE consumer as Claude.
type gatewayCloudBedrockBody struct {
	source  io.ReadCloser
	decoder *bedrockEventStreamDecoder
	pending *bytes.Reader
}

func (r *gatewayCloudBedrockBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.pending == nil || r.pending.Len() == 0 {
		payload, err := r.decoder.Decode()
		if err != nil {
			if err == io.EOF {
				return 0, io.EOF
			}
			return 0, ErrGatewayNativeEffectUnknown
		}
		decoded := extractBedrockChunkData(payload)
		if decoded == nil || len(decoded) > gatewayNativeSSEFrameLimit {
			return 0, ErrGatewayNativeEffectUnknown
		}
		decoded = transformBedrockInvocationMetrics(decoded)
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(decoded, &event) != nil || event.Type == "" || strings.ContainsAny(event.Type, "\r\n") {
			return 0, ErrGatewayNativeEffectUnknown
		}
		frame := append([]byte("event: "+event.Type+"\ndata: "), decoded...)
		frame = append(frame, '\n', '\n')
		r.pending = bytes.NewReader(frame)
	}
	return r.pending.Read(p)
}

func (r *gatewayCloudBedrockBody) Close() error { return r.source.Close() }
