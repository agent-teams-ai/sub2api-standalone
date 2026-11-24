package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The protected bridge authenticates the opaque carrier before calling this
// converter. Its selected older Claude model still needs the exact signed
// native block returned by the maintained stream codec on the next tool turn.
func TestResponsesToAnthropicProtectedThinkingPreservesNativeStreamRoundtrip(t *testing.T) {
	for _, block := range []AnthropicContentBlock{
		{Type: "thinking", Thinking: "Inspect the original source before deciding.", Signature: "source-issued-signature"},
		{Type: "redacted_thinking", Data: "source-issued-redacted-data"},
	} {
		t.Run(block.Type, func(t *testing.T) {
			state := NewAnthropicEventToResponsesState()
			state.PreserveThinkingSignatures = true
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "message_start", Message: &AnthropicResponse{ID: "msg_protected", Model: "claude-sonnet-4-6"}}, state)
			index := 0
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_start", Index: &index, ContentBlock: &block}, state)
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop", Index: &index}, state)
			index++
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_start", Index: &index, ContentBlock: &AnthropicContentBlock{Type: "tool_use", ID: "call_source", Name: "read_source", Input: json.RawMessage(`{"path":"file.go"}`)}}, state)
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop", Index: &index}, state)
			events := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "message_stop"}, state)
			var output []ResponsesOutput
			for _, event := range events {
				if event.Type == "response.completed" {
					output = event.Response.Output
				}
			}
			require.Len(t, output, 2)
			require.Equal(t, "reasoning", output[0].Type)
			require.NotEmpty(t, output[0].EncryptedContent)
			input := []any{output[0], output[1], map[string]string{"type": "function_call_output", "call_id": "call_source", "output": "original source"}}
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			request := &ResponsesRequest{Model: "claude-sonnet-4-6", Input: raw, Reasoning: &ResponsesReasoning{Effort: "medium"}}
			converted, err := ResponsesToAnthropicRequestPreservingThinking(request)
			require.NoError(t, err)
			require.NotEmpty(t, converted.Messages)
			var assistant []AnthropicContentBlock
			require.NoError(t, json.Unmarshal(converted.Messages[0].Content, &assistant))
			require.Len(t, assistant, 2)
			require.Equal(t, block, assistant[0], "thinking text, signature or redacted data must remain exact")
			require.Equal(t, "tool_use", assistant[1].Type)
			require.Equal(t, "call_source", assistant[1].ID)
			require.Equal(t, 4096, converted.Thinking.BudgetTokens)
			ordinary, err := ResponsesToAnthropicRequest(request)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(ordinary.Messages[0].Content, &assistant))
			require.Len(t, assistant, 1, "the existing ordinary bridge's older-model policy remains unchanged")
			require.Equal(t, "tool_use", assistant[0].Type)
		})
	}
}
