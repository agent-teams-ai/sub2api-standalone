//go:build integration || gatewaycustody

package repository

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nativeConnectionWireCase struct {
	profile, base, model, protocol, endpoint string
	t                                        *testing.T
	secret                                   string
	calls                                    atomic.Int32
	phase                                    atomic.Int32
}

type nativeConnectionTLSUpstream struct{ client *http.Client }

func (u *nativeConnectionTLSUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(request)
}
func (u *nativeConnectionTLSUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, id, concurrency)
}

func nativeConnectionWireCases() []*nativeConnectionWireCase {
	rows := []*nativeConnectionWireCase{
		{profile: "openai-api-responses-apikey-v1", base: "https://api.openai.com", model: "gpt-4.1", protocol: "responses"},
		{profile: "grok-api-responses-apikey-v1", base: "https://api.x.ai", model: "grok-4", protocol: "responses"},
		{profile: service.GatewayAnthropicAPIKeyProfile, base: service.GatewayAnthropicAPIBaseURL, model: service.GatewayAnthropicAPIModel, protocol: "anthropic"},
		{profile: service.GatewayGeminiAPIKeyProfile, base: service.GatewayGeminiAPIBaseURL, model: service.GatewayGeminiAPIModel, protocol: "chat_completions"},
		{profile: service.GatewayMiMoResponsesProfile, base: "https://api.xiaomimimo.com/v1", model: "mimo-fixture", protocol: "responses"},
		{profile: service.GatewayLegacyBridgeProfile, base: "https://api.xiaomimimo.com/v1", model: "mimo-fixture", protocol: "chat_completions"},
		{profile: service.GatewayOpenRouterResponsesProfile, base: service.GatewayOpenRouterBaseURL, model: service.GatewayOpenRouterModel, protocol: "responses"},
		{profile: service.GatewayNVIDIABuildChatProfile, base: service.GatewayNVIDIABuildBaseURL, model: service.GatewayNVIDIABuildAnchorModel, protocol: "chat_completions"},
	}
	for _, definition := range service.GatewayNativeConnectionInventory() {
		native := service.LookupProviderProfile(definition.Platform)
		if native == nil {
			continue
		}
		endpoints := native.Modes[definition.AccountMode]
		models := []struct{ model, protocol string }{{native.DefaultTestModel, service.APIProtocolResponses}}
		if models[0].model == "" {
			models[0].model = "native-fixture-model"
		}
		if !native.SupportsProtocol(definition.AccountMode, service.APIProtocolResponses) {
			models[0].protocol = service.APIProtocolChatCompletions
		}
		if native.Routing == service.ProviderRoutingByModel {
			models = []struct{ model, protocol string }{{"gpt-4.1", service.APIProtocolResponses}, {"qwen-fixture", service.APIProtocolAnthropic}, {"native-fixture-chat", service.APIProtocolChatCompletions}}
			if definition.Platform == service.PlatformCommandCode {
				models[1].model = "claude-sonnet-4-6"
			}
		}
		for _, selected := range models {
			base := endpoints.BaseURLs[selected.protocol]
			switch definition.ProfileID {
			case service.GatewayKimiCodeResponsesProfile:
				base, selected.model = service.GatewayKimiCodeResponsesBaseURL, service.GatewayKimiCodeResponsesModel
			case service.GatewayGLMCodingResponsesProfile:
				base, selected.model = service.GatewayGLMCodingResponsesBaseURL, service.GatewayGLMCodingResponsesModel
			case service.GatewayMiniMaxMPlanResponsesProfile:
				base, selected.model = service.GatewayMiniMaxMPlanResponsesBaseURL, service.GatewayMiniMaxMPlanResponsesModel
			}
			rows = append(rows, &nativeConnectionWireCase{profile: definition.ProfileID, base: base, model: selected.model, protocol: selected.protocol})
		}
	}
	for _, row := range rows {
		base := strings.TrimRight(row.base, "/")
		suffix := "/v1/responses"
		if row.protocol == service.APIProtocolChatCompletions {
			suffix = "/v1/chat/completions"
		} else if row.protocol == service.APIProtocolAnthropic {
			suffix = "/v1/messages"
		}
		if strings.HasSuffix(base, "/v1") || row.profile == service.GatewayGeminiAPIKeyProfile || row.profile == "glm-payg-responses-apikey-v1" {
			suffix = strings.TrimPrefix(suffix, "/v1")
		}
		if row.profile == "deepseek-responses-apikey-v1" {
			suffix = "/responses"
		}
		row.endpoint = base + suffix
	}
	return rows
}

func nativeConnectionWriteProtocol(t *testing.T, w http.ResponseWriter, row *nativeConnectionWireCase, second bool) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	switch row.protocol {
	case service.APIProtocolResponses:
		var output any = []any{map[string]any{"type": "function_call", "id": "fc_review", "status": "completed", "call_id": "call_review", "name": "read_source", "arguments": `{"path":"file.go"}`}}
		if second {
			output = []any{map[string]any{"type": "message", "id": "msg_review", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": `{"findings":[]}`, "annotations": []any{}}}}}
		}
		payload, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_review", "status": "completed", "model": row.model, "output": output, "usage": map[string]int{"input_tokens": 5, "output_tokens": 3}}})
		require.NoError(t, err)
		_, _ = fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", payload)
	case service.APIProtocolChatCompletions:
		if !second {
			_, _ = io.WriteString(w, "data: {\"id\":\"first\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_review\",\"type\":\"function\",\"function\":{\"name\":\"read_source\",\"arguments\":\"{\\\"path\\\":\\\"file.go\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
			if row.profile == service.GatewayGeminiAPIKeyProfile {
				_, _ = io.WriteString(w, "data: {\"id\":\"first\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"extra_content\":{\"google\":{\"thought_signature\":\"fixture-google-signature\"}}}]},\"finish_reason\":null}]}\n\n")
			}
			_, _ = io.WriteString(w, "data: {\"id\":\"first\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			_, _ = io.WriteString(w, "data: {\"id\":\"second\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"{\\\"findings\\\":[]}\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"second\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"usage\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\ndata: [DONE]\n\n")
	case service.APIProtocolAnthropic:
		_, _ = fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_review\",\"type\":\"message\",\"role\":\"assistant\",\"model\":%q,\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n", row.model)
		if !second {
			_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_review\",\"name\":\"read_source\",\"input\":{}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"file.go\\\"}\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":3}}\n\n")
		} else {
			_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"{\\\"findings\\\":[]}\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n")
		}
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	default:
		t.Fatal("fixture requires an actual native protocol")
	}
}

func nativeConnectionCompletedOutput(t *testing.T, wire string) []json.RawMessage {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(wire))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			Response struct {
				Output []json.RawMessage `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event) == nil && event.Type == "response.completed" {
			return event.Response.Output
		}
	}
	require.NoError(t, scanner.Err())
	t.Fatal("native stream has no successful terminal Responses event")
	return nil
}

// Real migrated PostgreSQL and real TLS exercise native parsing, sealed births,
// exact retained account custody, the common final effect fence, native protocol
// requests, streamed tool continuation and credential erasure. The
// provider endpoints are controlled fixture peers; no paid provider is probed.
func TestGatewayNativeConnectionPostgresProtocolMatrix(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_DISPATCH_TEST_DSN", "native_dispatch_test_")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("wire-fixture", map[string][]byte{"wire-fixture": bytes.Repeat([]byte{0x72}, 32)})
	require.NoError(t, err)
	var current atomic.Pointer[nativeConnectionWireCase]
	rows := nativeConnectionWireCases()
	allowedHosts := map[string]bool{}
	for _, row := range rows {
		u, err := url.Parse(row.endpoint)
		require.NoError(t, err)
		allowedHosts[u.Hostname()+":443"] = true
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		row := current.Load()
		if row == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if request.Host == "openrouter.ai" && request.URL.Path == "/api/v1/models" {
			assert.Empty(row.t, request.Header.Get("Authorization"), "public model discovery receives no credential")
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools","tool_choice"]}]}`, row.model)
			return
		}
		row.calls.Add(1)
		assert.Equal(row.t, http.MethodPost, request.Method)
		assert.Equal(row.t, row.endpoint, "https://"+request.Host+request.URL.RequestURI())
		assert.Empty(row.t, request.Header.Get("Cookie"))
		assert.Empty(row.t, request.Header.Get("X-Codex-Turn-State"))
		if row.protocol == service.APIProtocolAnthropic {
			assert.Equal(row.t, row.secret, request.Header.Get("x-api-key"))
			assert.Empty(row.t, request.Header.Get("Authorization"))
			assert.Equal(row.t, "2023-06-01", request.Header.Get("anthropic-version"))
		} else {
			assert.Equal(row.t, "Bearer "+row.secret, request.Header.Get("Authorization"))
		}
		raw, err := io.ReadAll(request.Body)
		require.NoError(row.t, err)
		assert.NotContains(row.t, string(raw), row.secret)
		var body map[string]json.RawMessage
		require.NoError(row.t, json.Unmarshal(raw, &body))
		assert.JSONEq(row.t, fmt.Sprintf("%q", row.model), string(body["model"]))
		assert.Equal(row.t, "true", string(body["stream"]))
		if row.phase.Load() == 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"provider-private-body-must-not-leak"}}`)
			return
		}
		second := row.phase.Load() == 2
		if second {
			assert.Contains(row.t, string(raw), "synthetic file contents")
			if row.profile == service.GatewayGeminiAPIKeyProfile {
				assert.Contains(row.t, string(raw), "fixture-google-signature")
			}
		}
		nativeConnectionWriteProtocol(row.t, w, row, second)
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if !allowedHosts[address] {
			return nil, service.ErrGatewayNativeIdentity
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionTLSUpstream{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, gateway.SetGatewayOpenRouterModels(service.NewGatewayOpenRouterModels(transport)))
	dispatchers, err := service.NewGatewayNativeAPIKeyDispatchers(gateway)
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "wire-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "wire-owner")
	require.NoError(t, err)
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if ctx.Err() != nil || !service.GatewayConnectionScopeAuthorized(ctx, scope) || scope.Owner != "wire-owner" {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	for _, row := range rows {
		t.Run(row.profile+"/"+row.protocol, func(t *testing.T) {
			row.t, row.secret = t, "wire-fixture-key-"+uuid.NewString()
			current.Store(row)
			method, err := service.NewGatewayNativeAPIKeyConnectionMethod(row.profile, "wire-"+uuid.NewString(), service.GatewayConnectionMount{BaseURL: row.base, Model: row.model, QualificationRef: "wire-fixture"}, gateway)
			require.NoError(t, err)
			var route *service.GatewayNativeRoute
			cleanup := func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
				return service.GatewayNativeOAuthCleanupAuthority{Owner: "wire-owner", LeaseExpiresAt: time.Now().Add(time.Minute), Native: route}, nil
			}
			dependencies := service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: bytes.Repeat([]byte{0x74}, 32),
				Methods: []service.GatewayNativeConnectionMethodAdapter{method}, Connect: owner, Refresh: owner, Cleanup: cleanup, Dispatchers: dispatchers, Gateway: gateway}
			native, err := service.NewGatewayNativeConnections(dependencies)
			require.NoError(t, err)
			in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "wire-consumer", Owner: "wire-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "wire-" + uuid.NewString(), Connection: method.Selection()}
			fields := map[string]string{"api_key": row.secret}
			created, err := native.Begin(ownerCtx, in, fields)
			require.NoError(t, err)
			require.Equal(t, "completed", created.State)
			published, err := native.Describe(ownerCtx, in)
			require.NoError(t, err)
			require.NotNil(t, published.Native)
			route = &published.Native.GatewayNativeRoute
			if row.profile == service.GatewayOpenRouterResponsesProfile {
				models, err := service.NewGatewayAccountModelsReaderV2(native, nil, func(context.Context, service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
					return *route, nil
				})
				require.NoError(t, err)
				result := models.Read(ownerCtx, service.GatewayAccountModelsInput{GatewayAccountLimitsInput: service.GatewayAccountLimitsInput{OwnerRef: in.Scope.Owner, AccountRef: in.Scope.Account, ExpectedAuthorizationEpoch: 1, ProfileID: row.profile}})
				require.Equal(t, "snapshot", result.Status)
			}
			call := func(scope service.GatewayNativeConnectionScope, selected service.GatewayNativeRoute, body []byte) (*httptest.ResponseRecorder, bool, error) {
				callCtx, stop := context.WithTimeout(ownerCtx, 10*time.Second)
				defer stop()
				life, err := service.NewGatewayNativeLifetime(callCtx, stop, func() {}, 1<<20)
				require.NoError(t, err)
				life.BindAccount(scope.Scope.Consumer, scope.Scope.Account, scope.Scope.Generation)
				require.True(t, life.Admit(time.Now().Add(5*time.Second)))
				callCtx = service.WithGatewayNativeLifetime(service.WithGatewayNativeCustody(callCtx, custody), life)
				bound, handled, err := native.BindRoute(callCtx, selected, scope.Scope.Account, 1, 1<<20, 512)
				if err != nil {
					life.NoForward()
					return httptest.NewRecorder(), false, err
				}
				require.True(t, handled)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("Authorization", "Bearer consumer-credential-must-not-leak")
				c.Request.Header.Set("X-Codex-Turn-State", "foreign")
				_, entered, callErr := gateway.ForwardGatewayRoute(bound, c, selected, body)
				require.True(t, life.Snapshot().ForwardingReturned)
				return recorder, entered, callErr
			}
			request := map[string]any{"model": row.model, "input": "Review file.go", "store": false, "stream": true, "max_output_tokens": 512, "tools": []any{map[string]any{"type": "function", "name": "read_source", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}}}}}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			row.phase.Store(1)
			first, entered, err := call(in, *route, body)
			require.NoError(t, err)
			require.True(t, entered)
			output := nativeConnectionCompletedOutput(t, first.Body.String())
			require.NotContains(t, first.Body.String(), "fixture-google-signature")
			var callID string
			for _, item := range output {
				var itemFields struct{ Type, CallID string }
				var actual map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(item, &actual))
				_ = json.Unmarshal(actual["type"], &itemFields.Type)
				if itemFields.Type == "function_call" {
					require.NoError(t, json.Unmarshal(actual["call_id"], &callID))
				}
			}
			require.Equal(t, "call_review", callID)
			toolResult, err := json.Marshal(map[string]string{"type": "function_call_output", "call_id": callID, "output": "synthetic file contents"})
			require.NoError(t, err)
			request["input"] = append(output, toolResult)
			body, err = json.Marshal(request)
			require.NoError(t, err)
			row.phase.Store(2)
			second, entered, err := call(in, *route, body)
			require.NoError(t, err)
			require.True(t, entered)
			require.NotEmpty(t, nativeConnectionCompletedOutput(t, second.Body.String()))
			require.EqualValues(t, 2, row.calls.Load())
			row.phase.Store(3)
			failure, entered, err := call(in, *route, body)
			require.ErrorIs(t, err, service.ErrGatewayNativeEffectUnknown)
			require.True(t, entered)
			require.NotContains(t, failure.Body.String(), "provider-private-body-must-not-leak")
			require.EqualValues(t, 3, row.calls.Load(), "there is no automatic provider/protocol fallback after effect entry")
			foreign := in
			foreign.Scope.Owner = "foreign-owner"
			_, err = native.Custody(ownerCtx, foreign, *published.Native)
			require.Error(t, err)
			_, err = native.Cleanup(ownerCtx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "wire-cleanup", CleanupToken: "wire-cleanup-token"})
			require.NoError(t, err)
			_, entered, err = call(in, *route, body)
			require.Error(t, err)
			require.False(t, entered)
			require.EqualValues(t, 3, row.calls.Load(), "an erased physical birth cannot dispatch")
			var durable string
			require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_build_object('account',(SELECT to_jsonb(a) FROM accounts a WHERE id=$1),'intent',(SELECT to_jsonb(i) FROM gateway_oauth_connect_intents i WHERE operation_ref=$2))::text`, route.AccountID, in.Operation).Scan(&durable))
			require.NotContains(t, durable, row.secret)
		})
	}
}
