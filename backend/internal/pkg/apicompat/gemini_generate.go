package apicompat

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"
)

// This is the text/client-function GenerateContent codec. Provider parts are
// retained verbatim in the existing encrypted continuation: a text signature
// belongs to its original part, not to concatenated Responses output text.
type GeminiGeneratePart struct {
	Text             *string                       `json:"text,omitempty"`
	Thought          bool                          `json:"thought,omitempty"`
	ThoughtSignature string                        `json:"thoughtSignature,omitempty"`
	FunctionCall     *GeminiGenerateFunctionCall   `json:"functionCall,omitempty"`
	FunctionResponse *GeminiGenerateFunctionResult `json:"functionResponse,omitempty"`
}

type GeminiGenerateFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type GeminiGenerateFunctionResult struct {
	ID       string            `json:"id,omitempty"`
	Name     string            `json:"name"`
	Response map[string]string `json:"response"`
}

type geminiGenerateContent struct {
	Role  string               `json:"role,omitempty"`
	Parts []GeminiGeneratePart `json:"parts"`
}

func geminiGeneratePartsSigned(parts []GeminiGeneratePart) bool {
	for _, part := range parts {
		if part.ThoughtSignature != "" {
			return true
		}
	}
	return false
}

func geminiGeneratePartValid(part GeminiGeneratePart) bool {
	if part.ThoughtSignature != "" && (!GeminiThoughtSignatureValid(part.ThoughtSignature) || part.ThoughtSignature == "skip_thought_signature_validator") || part.FunctionResponse != nil {
		return false
	}
	if call := part.FunctionCall; call != nil {
		return part.Text == nil && !part.Thought && call.Name != "" && len(call.Name) <= 256 && len(call.ID) <= 512 && geminiGenerateJSONObject(call.Args)
	}
	return part.Text != nil || part.ThoughtSignature != ""
}

func geminiGenerateJSONObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 1 && trimmed[0] == '{' && json.Valid(trimmed)
}

func geminiGenerateJSONEqual(left, right []byte) bool {
	decode := func(raw []byte) (any, bool) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var out any
		err := decoder.Decode(&out)
		return out, err == nil
	}
	a, aOK := decode(left)
	b, bOK := decode(right)
	if !aOK || !bOK {
		return false
	}
	aRaw, _ := json.Marshal(a)
	bRaw, _ := json.Marshal(b)
	return bytes.Equal(aRaw, bRaw)
}

func restoreGeminiNativeParts(chat *ChatCompletionsRequest, parts []GeminiGeneratePart, indices map[int]bool) error {
	if len(parts) == 0 || len(parts) > 4096 || len(indices) == 0 {
		return ErrGeminiContinuation
	}
	order := make([]int, 0, len(indices))
	for index := range indices {
		if index < 0 || index >= len(chat.Messages) || chat.Messages[index].Role != "assistant" || chat.Messages[index].GeminiNativeParts != nil || chat.Messages[index].GeminiNativeSkip {
			return ErrGeminiContinuation
		}
		order = append(order, index)
	}
	sort.Ints(order)
	var text, actualText strings.Builder
	var calls []ChatToolCall
	for _, index := range order {
		text.WriteString(chatMessageContentText(chat.Messages[index].Content))
		calls = append(calls, chat.Messages[index].ToolCalls...)
	}
	callIndex := 0
	nativeCallIDs := make(map[string]string)
	for _, part := range parts {
		if !geminiGeneratePartValid(part) {
			return ErrGeminiContinuation
		}
		if part.Text != nil && !part.Thought {
			actualText.WriteString(*part.Text)
		}
		if call := part.FunctionCall; call != nil {
			if callIndex >= len(calls) || call.Name != calls[callIndex].Function.Name || !geminiGenerateJSONEqual(call.Args, []byte(calls[callIndex].Function.Arguments)) {
				return ErrGeminiContinuation
			}
			signature, err := geminiSignature(calls[callIndex].ExtraContent)
			if err != nil || signature != part.ThoughtSignature {
				return ErrGeminiContinuation
			}
			nativeCallIDs[calls[callIndex].ID] = call.ID
			callIndex++
		}
	}
	if callIndex != len(calls) || text.String() != actualText.String() {
		return ErrGeminiContinuation
	}
	chat.Messages[order[0]].GeminiNativeParts = parts
	chat.Messages[order[0]].GeminiNativeCallIDs = nativeCallIDs
	for _, index := range order[1:] {
		chat.Messages[index].GeminiNativeSkip = true
	}
	return nil
}

// ChatCompletionsToGeminiGenerateContent runs only after the common Responses
// grammar and authenticated Gemini continuation restore. It preserves JSON
// schemas and rejects an unsupported option instead of silently dropping it.
func ChatCompletionsToGeminiGenerateContent(chat *ChatCompletionsRequest) ([]byte, error) {
	if chat == nil || len(chat.Messages) == 0 || !chat.Stream || chat.ReasoningEffort != "" || chat.ParallelToolCalls != nil && !*chat.ParallelToolCalls {
		return nil, ErrGeminiContinuation
	}
	var contents []geminiGenerateContent
	var system []GeminiGeneratePart
	calls := make(map[string]GeminiGenerateFunctionCall)
	for _, message := range chat.Messages {
		for _, call := range message.ToolCalls {
			if call.ID == "" || call.Function.Name == "" || !geminiGenerateJSONObject([]byte(call.Function.Arguments)) {
				return nil, ErrGeminiContinuation
			}
			if _, duplicate := calls[call.ID]; duplicate {
				return nil, ErrGeminiContinuation
			}
			calls[call.ID] = GeminiGenerateFunctionCall{Name: call.Function.Name, Args: json.RawMessage(call.Function.Arguments)}
		}
	}
	for _, message := range chat.Messages {
		for id, nativeID := range message.GeminiNativeCallIDs {
			call, found := calls[id]
			if !found {
				return nil, ErrGeminiContinuation
			}
			call.ID = nativeID
			calls[id] = call
		}
	}
	for _, message := range chat.Messages {
		if message.GeminiNativeSkip {
			continue
		}
		if len(message.GeminiNativeParts) > 0 {
			contents = append(contents, geminiGenerateContent{Role: "model", Parts: message.GeminiNativeParts})
			continue
		}
		var parts []GeminiGeneratePart
		text := chatMessageContentText(message.Content)
		if message.Role == "tool" {
			call, found := calls[message.ToolCallID]
			if !found {
				return nil, ErrGeminiContinuation
			}
			parts = append(parts, GeminiGeneratePart{FunctionResponse: &GeminiGenerateFunctionResult{ID: call.ID, Name: call.Name, Response: map[string]string{"content": text}}})
		} else if text != "" {
			signature, err := geminiSignature(message.ExtraContent)
			if err != nil {
				return nil, err
			}
			parts = append(parts, GeminiGeneratePart{Text: &text, ThoughtSignature: signature})
		}
		for i, call := range message.ToolCalls {
			signature, err := geminiSignature(call.ExtraContent)
			if err != nil || i == 0 && signature == "" {
				return nil, ErrGeminiContinuation
			}
			native := calls[call.ID]
			parts = append(parts, GeminiGeneratePart{FunctionCall: &native, ThoughtSignature: signature})
		}
		if message.Role == "system" || message.Role == "developer" {
			system = append(system, parts...)
			continue
		}
		if len(parts) == 0 {
			continue
		}
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		} else if message.Role != "user" && message.Role != "tool" {
			return nil, ErrGeminiContinuation
		}
		if message.Role == "tool" && len(contents) > 0 && contents[len(contents)-1].Role == "user" {
			contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, parts...)
		} else {
			contents = append(contents, geminiGenerateContent{Role: role, Parts: parts})
		}
	}
	if len(contents) == 0 {
		return nil, ErrGeminiContinuation
	}
	request := map[string]any{"contents": contents}
	if len(system) > 0 {
		request["systemInstruction"] = geminiGenerateContent{Parts: system}
	}
	generation := map[string]any{}
	tokens := chat.MaxCompletionTokens
	if tokens == nil {
		tokens = chat.MaxTokens
	}
	if tokens == nil || *tokens < 1 || *tokens > 65536 {
		return nil, ErrGeminiContinuation
	}
	generation["maxOutputTokens"] = *tokens
	if chat.Temperature != nil {
		generation["temperature"] = *chat.Temperature
	}
	if chat.TopP != nil {
		generation["topP"] = *chat.TopP
	}
	if len(chat.ResponseFormat) > 0 {
		var format struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"json_schema"`
		}
		if json.Unmarshal(chat.ResponseFormat, &format) != nil {
			return nil, ErrGeminiContinuation
		}
		switch format.Type {
		case "text":
		case "json_object":
			generation["responseMimeType"] = "application/json"
		case "json_schema":
			if !geminiGenerateJSONObject(format.JSONSchema.Schema) {
				return nil, ErrGeminiContinuation
			}
			generation["responseMimeType"], generation["responseJsonSchema"] = "application/json", format.JSONSchema.Schema
		default:
			return nil, ErrGeminiContinuation
		}
	}
	request["generationConfig"] = generation
	declarations := []map[string]any{}
	for _, tool := range chat.Tools {
		if tool.Type != "function" || tool.Function == nil || !geminiGenerateJSONObject(tool.Function.Parameters) {
			return nil, ErrGeminiContinuation
		}
		declarations = append(declarations, map[string]any{"name": tool.Function.Name, "description": tool.Function.Description, "parametersJsonSchema": tool.Function.Parameters})
	}
	if len(declarations) > 0 {
		request["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
	}
	if len(chat.ToolChoice) > 0 {
		var choice string
		config := map[string]any{}
		if json.Unmarshal(chat.ToolChoice, &choice) == nil {
			mode, valid := map[string]string{"auto": "AUTO", "none": "NONE", "required": "ANY"}[choice]
			if !valid {
				return nil, ErrGeminiContinuation
			}
			config["mode"] = mode
		} else {
			var named struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if json.Unmarshal(chat.ToolChoice, &named) != nil || named.Type != "function" || named.Function.Name == "" {
				return nil, ErrGeminiContinuation
			}
			config["mode"], config["allowedFunctionNames"] = "ANY", []string{named.Function.Name}
		}
		request["toolConfig"] = map[string]any{"functionCallingConfig": config}
	}
	return json.Marshal(request)
}

// GeminiGenerateStream converts native stream objects into the existing Chat
// reducer's events. It does not synthesize provider success: EOF is accepted by
// the transport only after Finished, and unsupported finish reasons fail.
type GeminiGenerateStream struct {
	ID       string
	Model    string
	Created  int64
	Finished bool
	parts    []GeminiGeneratePart
	calls    int
	callIDs  map[string]bool
}

func NewGeminiGenerateStream(model string) *GeminiGenerateStream {
	return &GeminiGenerateStream{ID: generateChatCmplID(), Model: model, Created: time.Now().Unix(), callIDs: map[string]bool{}}
}

func (s *GeminiGenerateStream) NativeParts() []GeminiGeneratePart { return s.parts }

func geminiGenerateObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrGeminiContinuation
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		name, valid := token.(string)
		if err != nil || !valid || fields[name] != nil {
			return nil, ErrGeminiContinuation
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, ErrGeminiContinuation
		}
		fields[name] = value
	}
	if last, err := decoder.Token(); err != nil || last != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrGeminiContinuation
	}
	return fields, nil
}

func (s *GeminiGenerateStream) Push(raw []byte) (*ChatCompletionsChunk, error) {
	if s == nil || len(raw) > 1<<20 {
		return nil, ErrGeminiContinuation
	}
	fields, err := geminiGenerateObject(raw)
	if err != nil || fields["error"] != nil {
		return nil, ErrGeminiContinuation
	}
	var candidates []json.RawMessage
	if fields["candidates"] != nil && json.Unmarshal(fields["candidates"], &candidates) != nil || len(candidates) > 1 || s.Finished && len(candidates) > 0 {
		return nil, ErrGeminiContinuation
	}
	chunk := &ChatCompletionsChunk{ID: s.ID, Object: "chat.completion.chunk", Model: s.Model, Created: s.Created, Choices: []ChatChunkChoice{}}
	if len(candidates) == 1 {
		candidate, err := geminiGenerateObject(candidates[0])
		var index int
		if err != nil || candidate["index"] != nil && json.Unmarshal(candidate["index"], &index) != nil || index != 0 {
			return nil, ErrGeminiContinuation
		}
		choice := ChatChunkChoice{Index: 0, Delta: ChatDelta{Role: "assistant"}}
		if candidate["content"] != nil {
			content, err := geminiGenerateObject(candidate["content"])
			var parts []json.RawMessage
			var role string
			if err != nil || json.Unmarshal(content["parts"], &parts) != nil || len(parts) > 4096 || content["role"] != nil && json.Unmarshal(content["role"], &role) != nil || role != "" && role != "model" {
				return nil, ErrGeminiContinuation
			}
			for _, rawPart := range parts {
				partFields, err := geminiGenerateObject(rawPart)
				if err != nil {
					return nil, ErrGeminiContinuation
				}
				for name := range partFields {
					if name != "text" && name != "thought" && name != "thoughtSignature" && name != "functionCall" {
						return nil, ErrGeminiContinuation
					}
				}
				var part GeminiGeneratePart
				if json.Unmarshal(rawPart, &part) != nil || !geminiGeneratePartValid(part) {
					return nil, ErrGeminiContinuation
				}
				if part.FunctionCall != nil {
					callFields, err := geminiGenerateObject(partFields["functionCall"])
					if err != nil {
						return nil, ErrGeminiContinuation
					}
					for name := range callFields {
						if name != "id" && name != "name" && name != "args" {
							return nil, ErrGeminiContinuation
						}
					}
					if s.calls >= geminiContinuationMaxCalls || s.calls == 0 && part.ThoughtSignature == "" {
						return nil, ErrGeminiContinuation
					}
					id := part.FunctionCall.ID
					if id == "" {
						id = "call_" + generateItemID()
					}
					if s.callIDs[id] {
						return nil, ErrGeminiContinuation
					}
					s.callIDs[id] = true
					callIndex := s.calls
					choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, ChatToolCall{Index: &callIndex, ID: id, Type: "function", Function: ChatFunctionCall{Name: part.FunctionCall.Name, Arguments: string(part.FunctionCall.Args)}, ExtraContent: geminiExtra(part.ThoughtSignature)})
					s.calls++
				} else if part.Text != nil {
					field := &choice.Delta.Content
					if part.Thought {
						field = &choice.Delta.ReasoningContent
					}
					if *field == nil {
						value := ""
						*field = &value
					}
					**field += *part.Text
				}
				s.parts = append(s.parts, part)
			}
		}
		if candidate["finishReason"] != nil {
			var reason string
			if json.Unmarshal(candidate["finishReason"], &reason) != nil {
				return nil, ErrGeminiContinuation
			}
			switch reason {
			case "STOP":
				reason = "stop"
				if s.calls > 0 {
					reason = "tool_calls"
				}
			case "MAX_TOKENS":
				reason = "length"
			default:
				return nil, ErrGeminiContinuation
			}
			choice.FinishReason, s.Finished = &reason, true
		}
		chunk.Choices = append(chunk.Choices, choice)
	}
	if usage := fields["usageMetadata"]; usage != nil {
		var counts struct {
			Prompt   *int `json:"promptTokenCount"`
			Output   *int `json:"candidatesTokenCount"`
			Cached   int  `json:"cachedContentTokenCount"`
			Thoughts int  `json:"thoughtsTokenCount"`
		}
		if _, err := geminiGenerateObject(usage); err != nil || json.Unmarshal(usage, &counts) != nil || counts.Cached < 0 || counts.Thoughts < 0 {
			return nil, ErrGeminiContinuation
		}
		if counts.Prompt != nil && counts.Output != nil {
			if *counts.Prompt < counts.Cached || *counts.Output < 0 || *counts.Prompt > 1<<30 || *counts.Output > 1<<30 || counts.Thoughts > 1<<30 {
				return nil, ErrGeminiContinuation
			}
			chunk.Usage = &ChatUsage{PromptTokens: *counts.Prompt, CompletionTokens: *counts.Output + counts.Thoughts, TotalTokens: *counts.Prompt + *counts.Output + counts.Thoughts,
				PromptTokensDetails: &ChatTokenDetails{CachedTokens: counts.Cached}, CompletionTokensDetails: &ChatTokenDetails{ReasoningTokens: counts.Thoughts}}
		}
	}
	if rawParts, err := json.Marshal(s.parts); err != nil || len(rawParts) > GeminiContinuationMaxBytes || len(s.parts) > 4096 {
		return nil, ErrGeminiContinuation
	}
	return chunk, nil
}
