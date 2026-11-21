//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// These controlled provider replies reuse the earlier joined fixture's safe
// tool selection. They exercise the real RR tools and output schema through
// native codecs; the fixture never manufactures a successful consumer receipt.
type nativeConnectionJoinedTool struct {
	Kind, Name, Namespace, Arguments, Signature string
	Parameters                                  json.RawMessage
}

type nativeConnectionJoinedProtocol struct {
	thinking, thinkingSignature                                            string
	expectedSchema                                                         string
	customDeclarations                                                     int64
	mu                                                                     sync.Mutex
	pending                                                                map[string]nativeConnectionJoinedTool
	textSignature                                                          string
	dispatches, functionCalls, functionOutputs, customCalls, customOutputs int64
	schemaRequests, signatureChecks, thinkingRequests                      int64
	failure                                                                string
}

func (s *nativeConnectionJoinedProtocol) counts() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"dispatches": s.dispatches, "functionCalls": s.functionCalls, "functionOutputs": s.functionOutputs,
		"customCalls": s.customCalls, "customOutputs": s.customOutputs, "customDeclarations": s.customDeclarations, "schemaRequests": s.schemaRequests, "signatureChecks": s.signatureChecks, "thinkingRequests": s.thinkingRequests, "failure": s.failure}
}

// Native Responses may retain namespace declarations. Follow their declared
// structure directly, independently of the native tool mapper under test.
func nativeConnectionJoinedResponsesTools(raw json.RawMessage, namespace string) ([]nativeConnectionJoinedTool, bool) {
	var source []struct {
		Type, Name string
		Parameters json.RawMessage `json:"parameters"`
		Tools      json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &source) != nil {
		return nil, false
	}
	var out []nativeConnectionJoinedTool
	for _, tool := range source {
		if tool.Type == "namespace" {
			if namespace != "" || tool.Name == "" {
				return nil, false
			}
			children, valid := nativeConnectionJoinedResponsesTools(tool.Tools, tool.Name)
			if !valid || len(children) == 0 {
				return nil, false
			}
			out = append(out, children...)
		} else if tool.Type == "function" || tool.Type == "custom" {
			if tool.Name == "" {
				return nil, false
			}
			out = append(out, nativeConnectionJoinedTool{Kind: tool.Type, Name: tool.Name, Namespace: namespace, Parameters: tool.Parameters})
		}
	}
	return out, true
}

func nativeConnectionJoinedToolArguments(tool nativeConnectionJoinedTool) (string, bool) {
	if tool.Kind == "custom" && strings.HasSuffix(tool.Name, "apply_patch") {
		return "*** Begin Patch\n*** End Patch\n", true
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(tool.Parameters, &schema) != nil || schema.Properties == nil {
		return "", false
	}
	args := map[string]any{}
	switch {
	case strings.HasSuffix(tool.Name, "exec_command") && schema.Properties["cmd"] != nil:
		args = map[string]any{"cmd": "pwd", "yield_time_ms": 1000, "max_output_tokens": 1000}
	case strings.HasSuffix(tool.Name, "shell_command") && schema.Properties["command"] != nil:
		args = map[string]any{"command": "pwd", "timeout_ms": 1000}
	case tool.Name == "shell" && schema.Properties["command"] != nil:
		args = map[string]any{"command": []string{"sh", "-lc", "pwd"}}
	case strings.HasSuffix(tool.Name, "apply_patch") && schema.Properties["input"] != nil:
		args = map[string]any{"input": "*** Begin Patch\n*** End Patch\n"}
	default:
		return "", false
	}
	for key := range args {
		if schema.Properties[key] == nil {
			delete(args, key)
		}
	}
	for _, required := range schema.Required {
		if _, exists := args[required]; !exists {
			return "", false
		}
	}
	raw, err := json.Marshal(args)
	return string(raw), err == nil
}

func nativeConnectionJoinedJSONEqual(a, b string) bool {
	var left, right any
	return json.Unmarshal([]byte(a), &left) == nil && json.Unmarshal([]byte(b), &right) == nil && reflect.DeepEqual(left, right)
}

func nativeConnectionJoinedReviewSchema(raw json.RawMessage) bool {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	return json.Unmarshal(raw, &schema) == nil && schema.Properties["findings"] != nil && schema.Properties["revalidations"] != nil
}

func nativeConnectionJoinedSchemaDigest(raw json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var tree any
	if decoder.Decode(&tree) != nil || decoder.Decode(new(any)) != io.EOF {
		return ""
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(tree) != nil {
		return ""
	}
	digest := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte{'\n'}))
	return hex.EncodeToString(digest[:])
}

func (s *nativeConnectionJoinedProtocol) serve(w http.ResponseWriter, r *http.Request, peer *nativeConnectionJoinedPeer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(code string) error { s.failure = code; return ErrDenied }
	if peer == nil || peer.InferenceAuth == nil || !peer.InferenceAuth(r) {
		return fail("authentication")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return fail("body_bound")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return fail("body_json")
	}
	s.dispatches++
	protocol, wrapped := peer.Protocol, false
	if protocol == "bedrock" {
		protocol = "anthropic"
	}
	if protocol == "gemini" && body["request"] != nil {
		var inner map[string]json.RawMessage
		var model, project string
		expectedProject := peer.Row.Fields["project_id"]
		if expectedProject == "" {
			expectedProject = "auto-assigned-project"
		}
		if json.Unmarshal(body["request"], &inner) != nil || json.Unmarshal(body["model"], &model) != nil || model != peer.Row.Model ||
			json.Unmarshal(body["project"], &project) != nil || project != expectedProject {
			return fail("google_envelope")
		}
		body, wrapped = inner, true
	} else if protocol != "gemini" {
		var model string
		// Cloud Anthropic carries the model in its fixed URL, not the body.
		urlSelected := peer.Protocol == "bedrock" || peer.Row.ExecutionFamily == "vertex_messages"
		if !urlSelected && (body["model"] == nil || string(body["stream"]) != "true") {
			return fail("required_model_or_stream")
		}
		if body["model"] != nil && (json.Unmarshal(body["model"], &model) != nil || model != peer.Row.Model) {
			return fail("model")
		}
	}
	thinkingRequested := false
	if peer.Row.ProfileID == "anthropic-claude-oauth-messages-v1" && body["thinking"] != nil {
		var thinking struct {
			Type         string `json:"type"`
			BudgetTokens int    `json:"budget_tokens"`
		}
		var maximum int
		if json.Unmarshal(body["thinking"], &thinking) != nil || json.Unmarshal(body["max_tokens"], &maximum) != nil ||
			thinking.Type != "enabled" || thinking.BudgetTokens != 4096 || maximum <= thinking.BudgetTokens {
			return fail("claude_thinking_budget")
		}
		thinkingRequested = true
		s.thinkingRequests++
	}
	var schema json.RawMessage
	switch protocol {
	case "responses":
		var text struct {
			Format struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"format"`
		}
		_ = json.Unmarshal(body["text"], &text)
		schema = text.Format.Schema
	case "chat_completions":
		var format struct {
			JSONSchema struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"json_schema"`
		}
		_ = json.Unmarshal(body["response_format"], &format)
		schema = format.JSONSchema.Schema
	case "anthropic":
		var config struct {
			Format struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"format"`
		}
		_ = json.Unmarshal(body["output_config"], &config)
		schema = config.Format.Schema
	case "gemini":
		var generation struct {
			Schema json.RawMessage `json:"responseJsonSchema"`
		}
		_ = json.Unmarshal(body["generationConfig"], &generation)
		schema = generation.Schema
	default:
		return fail("protocol")
	}
	if nativeConnectionJoinedReviewSchema(schema) {
		if s.expectedSchema == "" || nativeConnectionJoinedSchemaDigest(schema) != s.expectedSchema {
			return fail("review_schema_identity")
		}
		s.schemaRequests++
	} else if s.pending == nil {
		return fail("review_schema")
	}
	tools, calls, outputs := []nativeConnectionJoinedTool{}, map[string]nativeConnectionJoinedTool{}, map[string]bool{}
	actualTextSignature := ""
	actualThinking, actualThinkingSignature := "", ""
	if protocol == "responses" {
		var valid bool
		tools, valid = nativeConnectionJoinedResponsesTools(body["tools"], "")
		if !valid {
			return fail("responses_tools")
		}
		// call_id has an underscore and cannot use Go's default field matching.
		var rawItems []json.RawMessage
		if len(body["input"]) > 0 && body["input"][0] == '[' {
			if json.Unmarshal(body["input"], &rawItems) != nil {
				return fail("responses_input")
			}
			for _, rawItem := range rawItems {
				var item struct {
					Type, Name, Namespace, Arguments, Input string
					CallID                                  string          `json:"call_id"`
					Output                                  json.RawMessage `json:"output"`
				}
				if json.Unmarshal(rawItem, &item) != nil {
					return fail("responses_item")
				}
				if item.Type == "function_call" || item.Type == "custom_tool_call" {
					kind, args := "function", item.Arguments
					if item.Type == "custom_tool_call" {
						kind, args = "custom", item.Input
					}
					if item.CallID == "" || calls[item.CallID].Name != "" {
						return fail("responses_call")
					}
					calls[item.CallID] = nativeConnectionJoinedTool{Kind: kind, Name: item.Name, Namespace: item.Namespace, Arguments: args}
				} else if item.Type == "function_call_output" || item.Type == "custom_tool_call_output" {
					if item.CallID == "" || outputs[item.CallID] || len(item.Output) == 0 || string(item.Output) == "null" {
						return fail("responses_output")
					}
					outputs[item.CallID] = true
				}
			}
		}
	} else if protocol == "gemini" {
		var sourceTools []struct {
			Declarations []struct {
				Name       string
				Parameters json.RawMessage `json:"parametersJsonSchema"`
			} `json:"functionDeclarations"`
		}
		if json.Unmarshal(body["tools"], &sourceTools) != nil {
			return fail("gemini_tools")
		}
		for _, group := range sourceTools {
			for _, tool := range group.Declarations {
				tools = append(tools, nativeConnectionJoinedTool{Kind: "function", Name: tool.Name, Parameters: tool.Parameters})
			}
		}
		var contents []struct {
			Role  string
			Parts []struct {
				Text      *string `json:"text"`
				Signature string  `json:"thoughtSignature"`
				Call      *struct {
					ID, Name string
					Args     json.RawMessage
				} `json:"functionCall"`
				Result *struct {
					ID, Name string
					Response json.RawMessage
				} `json:"functionResponse"`
			} `json:"parts"`
		}
		if json.Unmarshal(body["contents"], &contents) != nil {
			return fail("gemini_contents")
		}
		for _, content := range contents {
			for _, part := range content.Parts {
				if part.Call != nil {
					if content.Role != "model" || part.Call.ID == "" || calls[part.Call.ID].Name != "" {
						return fail("gemini_call")
					}
					calls[part.Call.ID] = nativeConnectionJoinedTool{Kind: "function", Name: part.Call.Name, Arguments: string(part.Call.Args), Signature: part.Signature}
				} else if part.Result != nil {
					if content.Role != "user" || part.Result.ID == "" || outputs[part.Result.ID] || len(part.Result.Response) == 0 {
						return fail("gemini_output")
					}
					outputs[part.Result.ID] = true
				} else if content.Role == "model" && part.Text != nil && part.Signature != "" {
					actualTextSignature = part.Signature
				}
			}
		}
	} else {
		var sourceTools []struct {
			Name        string
			InputSchema json.RawMessage `json:"input_schema"`
			Function    struct {
				Name       string
				Parameters json.RawMessage
			}
		}
		if json.Unmarshal(body["tools"], &sourceTools) != nil {
			return fail("message_tools")
		}
		for _, tool := range sourceTools {
			candidate := nativeConnectionJoinedTool{Kind: "function", Name: tool.Name, Parameters: tool.InputSchema}
			if protocol == "chat_completions" {
				candidate.Name, candidate.Parameters = tool.Function.Name, tool.Function.Parameters
			}
			tools = append(tools, candidate)
		}
		var messages []struct {
			Role       string
			Content    json.RawMessage
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string
				Function struct{ Name, Arguments string }
				Extra    struct {
					Google struct {
						Signature string `json:"thought_signature"`
					} `json:"google"`
				} `json:"extra_content"`
			} `json:"tool_calls"`
		}
		if json.Unmarshal(body["messages"], &messages) != nil {
			return fail("messages")
		}
		for _, message := range messages {
			if protocol == "chat_completions" {
				for _, call := range message.ToolCalls {
					if message.Role != "assistant" || call.ID == "" || calls[call.ID].Name != "" {
						return fail("chat_call")
					}
					calls[call.ID] = nativeConnectionJoinedTool{Kind: "function", Name: call.Function.Name, Arguments: call.Function.Arguments, Signature: call.Extra.Google.Signature}
				}
				if message.Role == "tool" {
					if message.ToolCallID == "" || outputs[message.ToolCallID] || len(message.Content) == 0 || string(message.Content) == "null" {
						return fail("chat_output")
					}
					outputs[message.ToolCallID] = true
				}
				continue
			}
			var parts []struct {
				Thinking, Signature string
				Type, ID, Name      string
				ToolUseID           string `json:"tool_use_id"`
				Input, Content      json.RawMessage
			}
			if json.Unmarshal(message.Content, &parts) != nil {
				continue
			}
			for _, part := range parts {
				if part.Type == "thinking" {
					if message.Role != "assistant" || actualThinkingSignature != "" || part.Thinking == "" || part.Signature == "" {
						return fail("anthropic_thinking_block")
					}
					actualThinking, actualThinkingSignature = part.Thinking, part.Signature
				}
				if part.Type == "tool_use" {
					if message.Role != "assistant" || part.ID == "" || calls[part.ID].Name != "" {
						return fail("anthropic_call")
					}
					calls[part.ID] = nativeConnectionJoinedTool{Kind: "function", Name: part.Name, Arguments: string(part.Input)}
				}
				if part.Type == "tool_result" {
					if message.Role != "user" || part.ToolUseID == "" || outputs[part.ToolUseID] || len(part.Content) == 0 || string(part.Content) == "null" {
						return fail("anthropic_output")
					}
					outputs[part.ToolUseID] = true
				}
			}
		}
	}
	continuation := s.pending != nil
	selected := map[string]nativeConnectionJoinedTool{}
	if continuation {
		if len(calls) != len(s.pending) || len(outputs) != len(s.pending) {
			return fail("continuation_count")
		}
		for id, expected := range s.pending {
			actual := calls[id]
			match := nativeConnectionJoinedJSONEqual(actual.Arguments, expected.Arguments)
			if expected.Kind == "custom" {
				match = actual.Arguments == expected.Arguments
			}
			if !outputs[id] || actual.Kind != expected.Kind || actual.Name != expected.Name || actual.Namespace != expected.Namespace || actual.Signature != expected.Signature || !match {
				return fail("continuation_binding")
			}
			if strings.HasSuffix(expected.Name, "apply_patch") {
				s.customOutputs++
			} else {
				s.functionOutputs++
			}
		}
		if protocol == "gemini" {
			if actualTextSignature != s.textSignature {
				return fail("gemini_text_signature")
			}
			s.signatureChecks++
		}
		if peer.Row.ProfileID == "gemini-openai-chat-apikey-v1" {
			s.signatureChecks++
		}
		if peer.Row.ProfileID == "anthropic-claude-oauth-messages-v1" && s.thinkingSignature != "" {
			if actualThinking != s.thinking || actualThinkingSignature != s.thinkingSignature {
				return fail("claude_thinking_signature")
			}
			s.signatureChecks++
		}
		s.pending = nil
	} else {
		if len(calls) != 0 || len(outputs) != 0 {
			return fail("unexpected_continuation")
		}
		for _, tool := range tools {
			if tool.Kind == "custom" || strings.HasSuffix(tool.Name, "apply_patch") {
				s.customDeclarations++
			}
			args, valid := nativeConnectionJoinedToolArguments(tool)
			if !valid {
				continue
			}
			tool.Arguments = args
			if strings.HasSuffix(tool.Name, "apply_patch") {
				selected["joined-call-custom"] = tool
			} else if selected["joined-call-1"].Name == "" || strings.HasSuffix(tool.Name, "exec_command") {
				selected["joined-call-1"] = tool
			}
		}
		if selected["joined-call-1"].Name == "" {
			return fail("no_safe_runtime_tool")
		}
		if protocol == "gemini" || peer.Row.ProfileID == "gemini-openai-chat-apikey-v1" {
			for id, tool := range selected {
				tool.Signature = "native-source-signature-" + uuid.NewString()
				selected[id] = tool
			}
			s.textSignature = "native-source-text-signature-" + uuid.NewString()
		}
		s.pending = selected
		s.thinking, s.thinkingSignature = "", ""
		if thinkingRequested {
			s.thinking = "Controlled provider reasoning before the declared tool."
			s.thinkingSignature = base64.StdEncoding.EncodeToString([]byte("native-claude-signature-" + uuid.NewString()))
		}
		s.functionCalls++
		if selected["joined-call-custom"].Name != "" {
			s.customCalls++
		}
	}
	return nativeConnectionJoinedEmitProtocol(w, peer, protocol, wrapped, continuation, selected, s.textSignature, s.thinking, s.thinkingSignature)
}

func nativeConnectionJoinedBedrockFrame(data []byte) []byte {
	var headers bytes.Buffer
	for _, pair := range [][2]string{{":event-type", "chunk"}, {":message-type", "event"}, {":content-type", "application/json"}} {
		_ = headers.WriteByte(byte(len(pair[0])))
		_, _ = headers.WriteString(pair[0])
		_ = headers.WriteByte(7)
		_ = binary.Write(&headers, binary.BigEndian, uint16(len(pair[1])))
		_, _ = headers.WriteString(pair[1])
	}
	payload, _ := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString(data)})
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint32(16+headers.Len()+len(payload)))
	_ = binary.Write(&out, binary.BigEndian, uint32(headers.Len()))
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(out.Bytes()))
	_, _ = out.Write(headers.Bytes())
	_, _ = out.Write(payload)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(out.Bytes()))
	return out.Bytes()
}

func nativeConnectionJoinedEmitProtocol(w http.ResponseWriter, peer *nativeConnectionJoinedPeer, protocol string, wrapped, continuation bool, selected map[string]nativeConnectionJoinedTool, textSignature, thinking, thinkingSignature string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	if peer.Protocol == "bedrock" {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	}
	sequence := 0
	emit := func(kind string, event map[string]any) error {
		if protocol == "responses" {
			event["sequence_number"] = sequence
			sequence++
		}
		if wrapped {
			event = map[string]any{"response": event}
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if peer.Protocol == "bedrock" {
			_, err = w.Write(nativeConnectionJoinedBedrockFrame(raw))
			return err
		}
		if kind != "" {
			if _, err = fmt.Fprintf(w, "event: %s\n", kind); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", raw)
		return err
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	const text = `{"findings":[],"revalidations":[]}`
	switch protocol {
	case "responses":
		response := map[string]any{"id": "joined-response", "object": "response", "model": peer.Row.Model, "status": "in_progress", "output": []any{}}
		if err := emit("response.created", map[string]any{"type": "response.created", "response": response}); err != nil {
			return err
		}
		output := []any{}
		if continuation {
			part := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
			item := map[string]any{"id": "joined-message", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
			_ = emit("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
			_ = emit("response.content_part.added", map[string]any{"type": "response.content_part.added", "output_index": 0, "content_index": 0, "item_id": "joined-message", "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
			_ = emit("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": "joined-message", "delta": text})
			_ = emit("response.output_text.done", map[string]any{"type": "response.output_text.done", "output_index": 0, "content_index": 0, "item_id": "joined-message", "text": text})
			item["status"], item["content"] = "completed", []any{part}
			_ = emit("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
			output = append(output, item)
		} else {
			for index, id := range ids {
				tool := selected[id]
				kind, field, event := "function_call", "arguments", "response.function_call_arguments"
				if tool.Kind == "custom" {
					kind, field, event = "custom_tool_call", "input", "response.custom_tool_call_input"
				}
				item := map[string]any{"id": "item-" + id, "type": kind, "call_id": id, "name": tool.Name, "status": "in_progress", field: ""}
				if tool.Namespace != "" {
					item["namespace"] = tool.Namespace
				}
				_ = emit("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": index, "item": item})
				_ = emit(event+".delta", map[string]any{"type": event + ".delta", "output_index": index, "item_id": "item-" + id, "delta": tool.Arguments})
				_ = emit(event+".done", map[string]any{"type": event + ".done", "output_index": index, "item_id": "item-" + id, field: tool.Arguments})
				item["status"], item[field] = "completed", tool.Arguments
				_ = emit("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": index, "item": item})
				output = append(output, item)
			}
		}
		return emit("response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"id": "joined-response", "object": "response", "status": "completed", "model": peer.Row.Model, "output": output, "usage": map[string]int{"input_tokens": 7, "output_tokens": 3, "total_tokens": 10}}})
	case "anthropic":
		_ = emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "joined-message", "type": "message", "role": "assistant", "model": peer.Row.Model, "content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": 7, "output_tokens": 0}}})
		if continuation {
			_ = emit("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			_ = emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
			_ = emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		} else {
			offset := 0
			if thinkingSignature != "" {
				offset = 1
				_ = emit("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
				_ = emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": thinking}})
				_ = emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": thinkingSignature}})
				_ = emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
			}
			for index, id := range ids {
				index += offset
				tool := selected[id]
				_ = emit("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "tool_use", "id": id, "name": tool.Name, "input": map[string]any{}}})
				_ = emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": tool.Arguments}})
				_ = emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
			}
		}
		reason := "tool_use"
		if continuation {
			reason = "end_turn"
		}
		_ = emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason}, "usage": map[string]int{"output_tokens": 3}})
		return emit("message_stop", map[string]any{"type": "message_stop"})
	case "chat_completions":
		chunk := func(delta any, finish any) map[string]any {
			return map[string]any{"id": "joined-chat", "object": "chat.completion.chunk", "created": 1, "model": peer.Row.Model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
		}
		if continuation {
			_ = emit("", chunk(map[string]any{"role": "assistant", "content": text}, nil))
		} else {
			for index, id := range ids {
				tool := selected[id]
				_ = emit("", chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": index, "id": id, "type": "function", "function": map[string]string{"name": tool.Name, "arguments": tool.Arguments}}}}, nil))
				if tool.Signature != "" {
					_ = emit("", chunk(map[string]any{"tool_calls": []any{map[string]any{"index": index, "extra_content": map[string]any{"google": map[string]string{"thought_signature": tool.Signature}}}}}, nil))
				}
			}
		}
		reason := "tool_calls"
		if continuation {
			reason = "stop"
		}
		last := chunk(map[string]any{}, reason)
		last["usage"] = map[string]int{"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10}
		if err := emit("", last); err != nil {
			return err
		}
		_, err := io.WriteString(w, "data: [DONE]\n\n")
		return err
	case "gemini":
		parts := []any{}
		if continuation {
			parts = append(parts, map[string]any{"text": text}, map[string]any{"text": "", "thoughtSignature": "native-final-signature-" + uuid.NewString()})
		} else {
			parts = append(parts, map[string]any{"text": "Checking the source.", "thoughtSignature": textSignature})
			for _, id := range ids {
				tool := selected[id]
				parts = append(parts, map[string]any{"functionCall": map[string]any{"id": id, "name": tool.Name, "args": json.RawMessage(tool.Arguments)}, "thoughtSignature": tool.Signature})
			}
		}
		return emit("", map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}}, "usageMetadata": map[string]int{"promptTokenCount": 7, "candidatesTokenCount": 3, "totalTokenCount": 10}})
	}
	return ErrDenied
}
