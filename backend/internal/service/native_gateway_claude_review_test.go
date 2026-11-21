//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestGatewayNativeClaudeReviewPreservesToolsSchemaAndOriginalOutputConstraint(t *testing.T) {
	const parameters = `{"type":"object","additionalProperties":false,"properties":{"cmd":{"type":"string"}},"required":["cmd"]}`
	const outputSchema = `{"type":"object","additionalProperties":false,"properties":{"confidence":{"type":"number","minimum":0,"maximum":1}},"required":["confidence"]}`
	body := []byte(`{"model":"claude-sonnet-4-6","input":"Review the source","stream":true,"store":false,"max_output_tokens":512,"reasoning":null,
		"prompt_cache_key":"33333333-3333-4333-8333-333333333333","client_metadata":{"x-codex-installation-id":"44444444-4444-4444-8444-444444444444"},
		"text":{"format":{"type":"json_schema","name":"codex_output_schema","strict":true,"schema":` + outputSchema + `}},
		"tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"exec_command","strict":true,"parameters":` + parameters + `}]},
		{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]}`)
	original := string(body)
	wire, headers, mapping, validate, err := gatewayClaudePrepareBoundedResponses(body, 1<<20)
	require.NoError(t, err)
	require.Equal(t, original, string(body), "admitted request bytes must remain unchanged")
	require.NotEmpty(t, headers["User-Agent"])
	require.True(t, mapping.CustomTools["apply_patch"])
	require.Len(t, mapping.NamespaceTools, 1)
	require.NotNil(t, validate)
	var request struct {
		Tools []struct {
			Name   string          `json:"name"`
			Schema json.RawMessage `json:"input_schema"`
			Strict *bool           `json:"strict"`
		} `json:"tools"`
		Output struct {
			Format struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"format"`
		} `json:"output_config"`
	}
	require.NoError(t, json.Unmarshal(wire, &request))
	require.Len(t, request.Tools, 2)
	for _, tool := range request.Tools {
		if tool.Name != "apply_patch" {
			require.NotNil(t, tool.Strict)
			require.True(t, *tool.Strict)
			require.JSONEq(t, parameters, string(tool.Schema), "the original function input schema must not be normalized")
		}
	}
	require.Contains(t, string(request.Output.Format.Schema), "less than or equal to 1")
	output := func(value string) []apicompat.ResponsesOutput {
		return []apicompat.ResponsesOutput{{Type: "message", Role: "assistant", Content: []apicompat.ResponsesContentPart{{Type: "output_text", Text: value}}}}
	}
	require.True(t, validate(output(`{"confidence":0.5}`)))
	require.False(t, validate(output(`{"confidence":1.1}`)), "the original numeric bound is enforced before response.completed")
	require.ErrorIs(t, ValidateGatewayNativeClaudeResponsesBounded(body, int64(len(body))), ErrGatewayNativeIdentity,
		"native attribution and lowered tool bytes belong to the original admission budget")
}

func TestGatewayNativeAnthropicRefusesLossyRootToolUnion(t *testing.T) {
	input := &apicompat.ResponsesRequest{Model: GatewayAnthropicAPIModel, Input: json.RawMessage(`"Review"`), Tools: []apicompat.ResponsesTool{{
		Type: "function", Name: "read", Parameters: json.RawMessage(`{"type":"object","anyOf":[{"required":["path"]},{"required":["id"]}]}`),
	}}}
	converted, err := apicompat.ResponsesToAnthropicRequest(input)
	require.NoError(t, err)
	_, err = gatewayNativeMarshalAnthropicRequest(converted, input.Tools)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity)
}
