package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiGenerateNativePartsSurviveResponsesToolContinuation(t *testing.T) {
	codec := newGeminiFixtureCodec(t, "native-owner/account/revision/model")
	native := NewGeminiGenerateStream("gemini-fixture")
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
	native := NewGeminiGenerateStream("gemini-fixture")
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
			_, err := NewGeminiGenerateStream("gemini-fixture").Push([]byte(raw))
			require.Error(t, err)
		})
	}
}

func TestGeminiGenerateKeepsSchemaAndActualZeroUsage(t *testing.T) {
	limit := 30
	chat := &ChatCompletionsRequest{Stream: true, MaxTokens: &limit, Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"Review"`)}},
		Tools: []ChatTool{{Type: "function", Function: &ChatFunction{Name: "lookup", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false,"required":["path"]}`)}}}}
	wire, err := ChatCompletionsToGeminiGenerateContent(chat)
	require.NoError(t, err)
	require.Contains(t, string(wire), `"parametersJsonSchema"`)
	require.Contains(t, string(wire), `"additionalProperties":false`)
	chunk, err := NewGeminiGenerateStream("gemini-fixture").Push([]byte(`{"candidates":[{"content":{"parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0}}`))
	require.NoError(t, err)
	require.NotNil(t, chunk.Usage)
	require.Equal(t, 0, chunk.Usage.PromptTokens)
	unknown, err := NewGeminiGenerateStream("gemini-fixture").Push([]byte(`{"candidates":[{"content":{"parts":[{"text":"OK"}]},"finishReason":"STOP"}]}`))
	require.NoError(t, err)
	require.Nil(t, unknown.Usage, "absence must never become measured zero")
}

func TestGeminiGenerateCombinedTextAndFunctionCompletedArrayContinuation(t *testing.T) {
	codec := newGeminiFixtureCodec(t, "native-completed-array")
	native := NewGeminiGenerateStream("gemini-fixture")
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
