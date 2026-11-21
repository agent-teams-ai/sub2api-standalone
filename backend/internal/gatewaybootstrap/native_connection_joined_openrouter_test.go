//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The controlled source must refuse a missing or substituted carrier even if
// the client returns a valid tool result. This tests the oracle, not a consumer
// review; the joined runner still requires the real CLI and sandbox receipt.
func TestNativeConnectionJoinedOpenRouterReasoningReplay(t *testing.T) {
	for _, variant := range []string{"exact", "missing", "substituted", "duplicated"} {
		t.Run(variant, func(t *testing.T) {
			schema := json.RawMessage(`{"type":"object","properties":{"findings":{"type":"array"},"revalidations":{"type":"array"}}}`)
			protocol := &nativeConnectionJoinedProtocol{expectedSchema: nativeConnectionJoinedSchemaDigest(schema)}
			peer := &nativeConnectionJoinedPeer{Protocol: "responses", Row: nativeConnectionJoinedRow{
				Model: "openai/gpt-5.4-mini", ReviewProof: &nativeConnectionJoinedReviewProof{ReasoningEffort: "medium", MaxOutputTokens: 8192},
			}, InferenceAuth: func(r *http.Request) bool { return r.Method == http.MethodPost }}
			body := map[string]any{
				"model": peer.Row.Model, "stream": true, "input": "review",
				"reasoning": map[string]string{"effort": "medium"}, "max_output_tokens": 8192,
				"include": []string{"reasoning.encrypted_content"},
				"text":    map[string]any{"format": map[string]any{"type": "json_schema", "schema": schema}},
				"tools": []any{map[string]any{"type": "function", "name": "exec_command", "parameters": map[string]any{
					"type": "object", "properties": map[string]any{"cmd": map[string]string{"type": "string"}}, "required": []string{"cmd"},
				}}},
			}
			serve := func() (*httptest.ResponseRecorder, error) {
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				response := httptest.NewRecorder()
				return response, protocol.serve(response, httptest.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/responses", bytes.NewReader(raw)), peer)
			}
			first, err := serve()
			require.NoError(t, err)
			var output []map[string]any
			for _, line := range strings.Split(first.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				var event struct {
					Type     string
					Response struct{ Output []map[string]any }
				}
				require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
				if event.Type == "response.completed" {
					output = event.Response.Output
				}
			}
			require.Len(t, output, 2)
			require.Equal(t, "reasoning", output[0]["type"])
			carrier, ok := output[0]["encrypted_content"].(string)
			require.True(t, ok && carrier != "")
			require.Equal(t, "function_call", output[1]["type"])
			input := []map[string]any{output[0], output[1], {"type": "function_call_output", "call_id": output[1]["call_id"], "output": "owned-checkout"}}
			switch variant {
			case "missing":
				input = input[1:]
			case "substituted":
				input[0]["encrypted_content"] = "substituted-carrier"
			case "duplicated":
				input = append(input, output[0])
			}
			body["input"] = input
			_, err = serve()
			counts := protocol.counts()
			if variant == "exact" {
				require.NoError(t, err)
				require.Equal(t, int64(2), counts["thinkingRequests"])
				require.Equal(t, int64(1), counts["signatureChecks"])
				require.Equal(t, int64(1), counts["functionOutputs"])
			} else {
				require.ErrorIs(t, err, ErrDenied)
				require.Zero(t, counts["signatureChecks"])
				require.Zero(t, counts["functionOutputs"])
			}
		})
	}
}
