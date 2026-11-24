package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientToolSearchPreservesExplicitContractAcrossNativeCodecs(t *testing.T) {
	const schema = `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":7}},"required":["query","limit"]}`
	const description = "Find only the caller's configured tools."
	declaration := map[string]any{"type": "tool_search", "execution": "client", "description": description, "parameters": json.RawMessage(schema)}
	original, err := json.Marshal(map[string]any{"model": "fixture", "input": "Inspect source", "tools": []any{declaration}})
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, json.Unmarshal(original, &request))
	mapping, changed, err := AdaptResponsesClientTools(request)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, mapping.ToolSearch)
	tools := requireResponsesClientToolValue[[]any](t, request["tools"])
	require.Len(t, tools, 1)
	assertContract := func(tools []any) {
		t.Helper()
		tool := requireResponsesClientToolValue[map[string]any](t, tools[0])
		require.Equal(t, "function", tool["type"])
		require.Equal(t, "tool_search", tool["name"])
		require.Equal(t, description, tool["description"])
		parameters, err := json.Marshal(tool["parameters"])
		require.NoError(t, err)
		require.JSONEq(t, schema, string(parameters))
	}
	assertContract(tools)
	continuation := map[string]any{"input": []any{map[string]any{"type": "tool_search_call", "call_id": "search", "execution": "client", "arguments": map[string]any{"query": "source", "limit": 2}}}}
	_, changed, err = AdaptResponsesClientToolsWithInheritedMapping(continuation, mapping, tools)
	require.NoError(t, err)
	require.True(t, changed)
	assertContract(requireResponsesClientToolValue[[]any](t, continuation["tools"]))
	var typed ResponsesRequest
	require.NoError(t, json.Unmarshal(original, &typed))
	chat, err := ResponsesToChatCompletionsRequestWithOptions(&typed, &ResponsesToChatOptions{})
	require.NoError(t, err)
	require.Len(t, chat.Tools, 1)
	require.Equal(t, description, chat.Tools[0].Function.Description)
	require.JSONEq(t, schema, string(chat.Tools[0].Function.Parameters))
	delete(declaration, "description")
	withoutDescription := map[string]any{"tools": []any{declaration}}
	_, _, err = AdaptResponsesClientTools(withoutDescription)
	require.NoError(t, err)
	tool := requireResponsesClientToolValue[map[string]any](t, requireResponsesClientToolValue[[]any](t, withoutDescription["tools"])[0])
	require.NotContains(t, tool, "description", "explicit client tools do not inherit a fabricated search description")
}
