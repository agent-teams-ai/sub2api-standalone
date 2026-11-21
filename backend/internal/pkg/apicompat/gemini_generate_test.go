package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiGenerateNativePartsSurviveResponsesToolContinuation(t *testing.T) {
	codec := newGeminiFixtureCodec(t, "native-owner/account/revision/model")
	native := NewGeminiGenerateStream("gemini-fixture", nil)
	state := NewChatCompletionsToResponsesStreamState("gemini-fixture")
	continuation := NewGeminiContinuationStream(state)
	var events []ResponsesStreamEvent
	for _, raw := range []string{
		`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Checking "},{"text":"both paths","thoughtSignature":"native-text-signature"}]}}]}`,
		`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"id":"native-first","name":"lookup","args":{"path":"main.go"}},"thoughtSignature":"native-call-signature"},{"functionCall":{"id":"native-second","name":"lookup","args":{"path":"main.go"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":14,"candidatesTokenCount":6,"thoughtsTokenCount":2,"cachedContentTokenCount":3}}`,
	} {
		chunk, err := native.Push([]byte(raw))
		require.NoError(t, err)
		require.NoError(t, continuation.Observe(chunk))
		events = append(events, ChatCompletionsChunkToResponsesEvents(chunk, state)...)
	}
	require.True(t, native.Finished)
	continuation.SetNativeParts(native.NativeParts())
	final, err := continuation.Finalize(state, codec)
	require.NoError(t, err)
	events = append(events, final...)
	public, err := json.Marshal(events)
	require.NoError(t, err)
	require.NotContains(t, string(public), "native-call-signature")
	require.NotContains(t, string(public), "native-text-signature")
	input := geminiStreamFixtureInput(t, events)
	chat := geminiFixtureRestore(t, input, codec)
	limit := 100
	chat.Stream, chat.MaxTokens = true, &limit
	request, err := ChatCompletionsToGeminiGenerateContent(chat)
	require.NoError(t, err)
	var decoded struct {
		Contents []geminiGenerateContent `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(request, &decoded))
	require.Len(t, decoded.Contents, 3)
	require.Equal(t, "model", decoded.Contents[1].Role)
	require.Equal(t, native.NativeParts(), decoded.Contents[1].Parts, "text boundaries and exact signature association must survive")
	require.Len(t, decoded.Contents[2].Parts, 2)
	require.Equal(t, "native-first", decoded.Contents[2].Parts[0].FunctionResponse.ID)
	require.Equal(t, "native-second", decoded.Contents[2].Parts[1].FunctionResponse.ID, "identical names/arguments must not collapse native call IDs")
	changed := json.RawMessage(strings.Replace(string(input), "Checking ", "Changed ", 1))
	modified, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{Model: "gemini-fixture", Input: changed})
	require.NoError(t, err)
	require.Error(t, RestoreGeminiContinuation(changed, modified, codec))
	foreign, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{Model: "gemini-fixture", Input: input})
	require.NoError(t, err)
	require.Error(t, RestoreGeminiContinuation(input, foreign, newGeminiFixtureCodec(t, "other-owner")))
}

func TestGeminiGenerateLateTextSignatureKeepsItsEmptyNativePart(t *testing.T) {
	native := NewGeminiGenerateStream("gemini-fixture", nil)
	state := NewChatCompletionsToResponsesStreamState("gemini-fixture")
	continuation := NewGeminiContinuationStream(state)
	codec := newGeminiFixtureCodec(t, "native-text")
	var events []ResponsesStreamEvent
	for _, raw := range []string{
		`{"candidates":[{"content":{"parts":[{"text":"One "}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"two"},{"text":"","thoughtSignature":"late-real-signature"}]},"finishReason":"STOP"}]}`,
	} {
		chunk, err := native.Push([]byte(raw))
		require.NoError(t, err)
		require.NoError(t, continuation.Observe(chunk))
		events = append(events, ChatCompletionsChunkToResponsesEvents(chunk, state)...)
	}
	continuation.SetNativeParts(native.NativeParts())
	final, err := continuation.Finalize(state, codec)
	require.NoError(t, err)
	events = append(events, final...)
	chat := geminiFixtureRestore(t, geminiStreamFixtureInput(t, events), codec)
	limit := 100
	chat.Stream, chat.MaxTokens = true, &limit
	request, err := ChatCompletionsToGeminiGenerateContent(chat)
	require.NoError(t, err)
	var decoded struct {
		Contents []geminiGenerateContent `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(request, &decoded))
	require.Equal(t, native.NativeParts(), decoded.Contents[1].Parts)
	require.Equal(t, "", *decoded.Contents[1].Parts[2].Text)
	require.Empty(t, decoded.Contents[1].Parts[0].ThoughtSignature)
}

func TestGeminiGenerateMalformedOrUnsignedProviderOutputFails(t *testing.T) {
	for name, raw := range map[string]string{
		"unsigned first function":  `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{}}}]},"finishReason":"STOP"}]}`,
		"dummy signature":          `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{}},"thoughtSignature":"skip_thought_signature_validator"}]},"finishReason":"STOP"}]}`,
		"unsupported image":        `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]},"finishReason":"STOP"}]}`,
		"unrequested partial args": `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{},"partialArgs":[]},"thoughtSignature":"real-signature"}]},"finishReason":"STOP"}]}`,
		"blocked response":         `{"candidates":[{"finishReason":"SAFETY"}]}`,
		"duplicate fields":         `{"candidates":[],"candidates":[]}`,
		"fractional usage":         `{"usageMetadata":{"promptTokenCount":1.5,"candidatesTokenCount":2}}`,
		"negative usage":           `{"usageMetadata":{"promptTokenCount":-1,"candidatesTokenCount":2}}`,
		"multiple candidates":      `{"candidates":[{},{}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewGeminiGenerateStream("gemini-fixture", nil).Push([]byte(raw))
			require.Error(t, err)
		})
	}
}

func TestGeminiGenerateKeepsSchemaAndActualZeroUsage(t *testing.T) {
	limit := 30
	parallel := false
	chat := &ChatCompletionsRequest{Stream: true, MaxTokens: &limit, ParallelToolCalls: &parallel, Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"Review"`)}},
		Tools: []ChatTool{{Type: "function", Function: &ChatFunction{Name: "lookup", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false,"required":["path"]}`)}}}}
	wire, err := ChatCompletionsToGeminiGenerateContent(chat)
	require.NoError(t, err)
	require.Contains(t, string(wire), `"parametersJsonSchema"`)
	require.Contains(t, string(wire), `"additionalProperties":false`)
	chunk, err := NewGeminiGenerateStream("gemini-fixture", nil).Push([]byte(`{"candidates":[{"content":{"parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0}}`))
	require.NoError(t, err)
	require.NotNil(t, chunk.Usage)
	require.Equal(t, 0, chunk.Usage.PromptTokens)
	unknown, err := NewGeminiGenerateStream("gemini-fixture", nil).Push([]byte(`{"candidates":[{"content":{"parts":[{"text":"OK"}]},"finishReason":"STOP"}]}`))
	require.NoError(t, err)
	require.Nil(t, unknown.Usage, "absence must never become measured zero")
}

func TestGeminiGenerateSerialToolCallsRejectASecondCallBeforePublication(t *testing.T) {
	first := `{"functionCall":{"id":"native-first","name":"lookup","args":{"path":"main.go"}},"thoughtSignature":"native-first-signature"}`
	second := `{"functionCall":{"id":"native-second","name":"lookup","args":{"path":"other.go"}}}`
	frame := func(parts string, finish bool) []byte {
		end := ""
		if finish {
			end = `,"finishReason":"STOP"`
		}
		return []byte(`{"candidates":[{"content":{"role":"model","parts":[` + parts + `]}` + end + `}]}`)
	}
	for _, split := range []bool{false, true} {
		name := "same frame"
		if split {
			name = "later frame"
		}
		t.Run(name, func(t *testing.T) {
			parallel := false
			native := NewGeminiGenerateStream("gemini-fixture", &parallel)
			parts := first + "," + second
			if split {
				chunk, err := native.Push(frame(first, false))
				require.NoError(t, err)
				require.Len(t, chunk.Choices[0].Delta.ToolCalls, 1)
				require.Nil(t, chunk.Choices[0].FinishReason)
				parts = second
			}
			chunk, err := native.Push(frame(parts, true))
			require.ErrorIs(t, err, ErrGeminiContinuation)
			require.Nil(t, chunk, "a forbidden second call must never reach the Responses reducer")
			require.False(t, native.Finished, "the rejected provider turn cannot become successful completion")
		})
	}
	for _, parallel := range []bool{false, true} {
		native := NewGeminiGenerateStream("gemini-fixture", &parallel)
		parts := first
		expected := 1
		if parallel {
			parts += "," + second
			expected = 2
		}
		chunk, err := native.Push(frame(parts, true))
		require.NoError(t, err)
		require.Len(t, chunk.Choices[0].Delta.ToolCalls, expected)
		require.True(t, native.Finished)
	}
}

func TestGeminiGenerateSerialFunctionAndCustomTurnsKeepIndependentSignedHistory(t *testing.T) {
	codec := newGeminiFixtureCodec(t, "native-serial-turns")
	parallel, limit := false, 100
	var outputs []*ResponsesResponse
	for _, turn := range []struct {
		id, name, kind, args, signature string
	}{
		{"call-function", "lookup", "function_call", `{"path":"main.go"}`, "native-function-turn-signature"},
		{"call-custom", "exec", "custom_tool_call", `{"input":"rg TODO\n"}`, "native-custom-turn-signature"},
	} {
		input := geminiFixtureInput(t, outputs...)
		chat, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{
			Model: "gemini-fixture", Input: input, Stream: true, MaxOutputTokens: &limit, ParallelToolCalls: &parallel,
			Tools: []ResponsesTool{{Type: "function", Name: "lookup", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}, {Type: "custom", Name: "exec"}},
		})
		require.NoError(t, err)
		require.NoError(t, RestoreGeminiContinuation(input, chat, codec))
		_, err = ChatCompletionsToGeminiGenerateContent(chat)
		require.NoError(t, err)
		native := NewGeminiGenerateStream("gemini-fixture", &parallel)
		frame, err := json.Marshal(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": []any{map[string]any{
				"functionCall":     map[string]any{"id": turn.id, "name": turn.name, "args": json.RawMessage(turn.args)},
				"thoughtSignature": turn.signature,
			}}}, "finishReason": "STOP",
		}}})
		require.NoError(t, err)
		chunk, err := native.Push(frame)
		require.NoError(t, err)
		state := NewChatCompletionsToResponsesStreamState("gemini-fixture")
		state.CustomTools, state.FunctionTools = map[string]bool{"exec": true}, map[string]bool{"lookup": true}
		continuation := NewGeminiContinuationStream(state)
		require.NoError(t, continuation.Observe(chunk))
		_ = ChatCompletionsChunkToResponsesEvents(chunk, state)
		continuation.SetNativeParts(native.NativeParts())
		events, err := continuation.Finalize(state, codec)
		require.NoError(t, err)
		response := events[len(events)-1].Response
		require.NotNil(t, response)
		calls := 0
		for _, item := range response.Output {
			if item.Type == "function_call" || item.Type == "custom_tool_call" {
				calls++
				require.Equal(t, turn.kind, item.Type)
				require.Equal(t, turn.id, item.CallID)
				if turn.kind == "custom_tool_call" {
					require.Equal(t, "rg TODO\n", item.Input)
				}
			}
		}
		require.Equal(t, 1, calls)
		public, err := json.Marshal(response)
		require.NoError(t, err)
		require.NotContains(t, string(public), turn.signature)
		outputs = append(outputs, response)
	}
	chat := geminiFixtureRestore(t, geminiFixtureInput(t, outputs...), codec)
	chat.Stream, chat.MaxTokens, chat.ParallelToolCalls = true, &limit, &parallel
	wire, err := ChatCompletionsToGeminiGenerateContent(chat)
	require.NoError(t, err, "false constrains each new provider turn, not prior signed tool history")
	require.Contains(t, string(wire), "native-function-turn-signature")
	require.Contains(t, string(wire), "native-custom-turn-signature")
	require.Contains(t, string(wire), `"input":"rg TODO\n"`)
}

func TestGeminiGenerateCombinedTextAndFunctionCompletedArrayContinuation(t *testing.T) {
	codec := newGeminiFixtureCodec(t, "native-completed-array")
	native := NewGeminiGenerateStream("gemini-fixture", nil)
	state := NewChatCompletionsToResponsesStreamState("gemini-fixture")
	continuation := NewGeminiContinuationStream(state)
	chunk, err := native.Push([]byte(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Checking the file."},{"functionCall":{"id":"native_review","name":"read_source","args":{"path":"file.go"}},"thoughtSignature":"real-native-function-signature"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`))
	require.NoError(t, err)
	wire, err := json.Marshal(chunk)
	require.NoError(t, err)
	var decoded ChatCompletionsChunk
	require.NoError(t, json.Unmarshal(wire, &decoded))
	require.NoError(t, continuation.Observe(&decoded))
	_ = ChatCompletionsChunkToResponsesEvents(&decoded, state)
	continuation.SetNativeParts(native.NativeParts())
	final, err := continuation.Finalize(state, codec)
	require.NoError(t, err)
	raw, err := json.Marshal(final[len(final)-1].Response.Output)
	require.NoError(t, err)
	var input []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &input))
	input = append(input, json.RawMessage(`{"type":"function_call_output","call_id":"native_review","output":"synthetic file contents"}`))
	raw, err = json.Marshal(input)
	require.NoError(t, err)
	limit := 512
	chat, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{Model: "gemini-fixture", Input: raw, Stream: true, MaxOutputTokens: &limit})
	require.NoError(t, err)
	require.NoError(t, RestoreGeminiContinuation(raw, chat, codec))
	request, err := ChatCompletionsToGeminiGenerateContent(chat)
	require.NoError(t, err)
	require.Contains(t, string(request), "real-native-function-signature")
}
