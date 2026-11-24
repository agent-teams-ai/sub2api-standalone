package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

const gatewayClaudeContinuationPrefix = "anthropic-thoughts-v1."
const gatewayClaudeSignedEnvelopePrefix = "anthropic-thinking-v1:"

type gatewayClaudeThinkingPayload struct {
	Version  int                          `json:"version"`
	Envelope string                       `json:"envelope"`
	Summary  []apicompat.ResponsesSummary `json:"summary"`
}

// Both legacy browser custody and generic imported custody use the same native
// AEAD keyring. A distinct domain prevents interpretation as Gemini reasoning or
// as authorization. The original physical birth and credential revision remain
// part of the binding after configuration removal or account reconnection.
func gatewayClaudeContinuation(ctx context.Context, custody *GatewayNativeCredentialCustody, scope GatewayNativeCredentialScope, route GatewayNativeRoute,
	selection *GatewayConnectionSelection, publication *GatewayConnectionPublication, revision int64) (apicompat.GeminiContinuationCodec, error) {
	if custody == nil || !GatewayConnectionScopeAuthorized(ctx, scope) || !GatewayConnectionSelectionAuthorized(ctx, selection) || revision < 1 ||
		route.Profile != GatewayClaudeOAuthMessagesProfile || route.BaseURL != GatewayClaudeOAuthBaseURL || route.Model != GatewayClaudeOAuthModel ||
		route.AccountID < 1 || route.CreatedAt.IsZero() || route.Generation != scope.Generation {
		return nil, ErrGatewayNativeIdentity
	}
	if publication != nil && (selection == nil || publication.Connection != *selection || publication.DispatchFamily != "claude_messages" || publication.AuthFamily != "claude_oauth") {
		return nil, ErrGatewayNativeIdentity
	}
	route.CreatedAt = route.CreatedAt.UTC()
	binding, err := json.Marshal(struct {
		Scope       GatewayNativeCredentialScope  `json:"scope"`
		Route       GatewayNativeRoute            `json:"route"`
		Connection  *GatewayConnectionSelection   `json:"connection"`
		Publication *GatewayConnectionPublication `json:"publication"`
		Revision    int64                         `json:"revision"`
	}{scope, route, selection, publication, revision})
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	return &gatewayGeminiContinuationCodec{custody: custody, binding: binding, domain: "anthropic"}, nil
}

func gatewayClaudeSignedThinking(value string) (apicompat.AnthropicContentBlock, bool) {
	var block apicompat.AnthropicContentBlock
	if !strings.HasPrefix(value, gatewayClaudeSignedEnvelopePrefix) || len(value) > apicompat.GeminiContinuationMaxBytes {
		return block, false
	}
	raw, err := base64.RawStdEncoding.Strict().DecodeString(strings.TrimPrefix(value, gatewayClaudeSignedEnvelopePrefix))
	if err != nil || len(raw) > apicompat.GeminiContinuationMaxBytes || !gatewayOAuthUnicodeValid(raw) {
		return block, false
	}
	defer clear(raw)
	if _, valid := gatewayClaudeObject(raw, "type", "thinking", "signature", "data"); !valid || json.Unmarshal(raw, &block) != nil {
		return block, false
	}
	switch block.Type {
	case "thinking":
		return block, len(block.Thinking) <= 64<<10 && block.Data == "" && apicompat.GeminiThoughtSignatureValid(block.Signature)
	case "redacted_thinking":
		return block, block.Thinking == "" && block.Signature == "" && apicompat.GeminiThoughtSignatureValid(block.Data)
	}
	return block, false
}

func gatewayClaudeThinkingSummaryMatches(block apicompat.AnthropicContentBlock, summary []apicompat.ResponsesSummary) bool {
	if block.Thinking == "" {
		return len(summary) == 0
	}
	return len(summary) == 1 && summary[0].Type == "summary_text" && summary[0].Text == block.Thinking
}

// With no codec this is only the public structural admission projection. The
// original bytes remain bounded; the real forwarder repeats preparation after
// opening every carrier against locked current custody, before provider entry.
func gatewayClaudeRestoreThinking(body []byte, codec apicompat.GeminiContinuationCodec) ([]byte, error) {
	fields, valid := gatewayNativeCanonicalObject(body, "input")
	if !valid || len(body) > 4<<20 {
		return nil, ErrGatewayNativeIdentity
	}
	input := bytes.TrimSpace(fields["input"])
	if len(input) == 0 || input[0] != '[' {
		return body, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(input, &items) != nil || len(items) > 4096 {
		return nil, ErrGatewayNativeIdentity
	}
	seen := make(map[string]bool)
	projected := make([]json.RawMessage, 0, len(items))
	changed := false
	for _, raw := range items {
		kind, valid := GatewayNativePayloadFields(raw, "type")
		var itemType string
		if !valid || kind["type"] != nil && json.Unmarshal(kind["type"], &itemType) != nil {
			return nil, ErrGatewayNativeIdentity
		}
		if itemType != "reasoning" {
			projected = append(projected, raw)
			continue
		}
		item, valid := gatewayNativeCanonicalObject(raw, "type", "id", "status", "summary", "content", "encrypted_content")
		if !valid {
			return nil, ErrGatewayNativeIdentity
		}
		for name := range item {
			switch name {
			case "type", "id", "status", "summary", "content", "encrypted_content":
			default:
				return nil, ErrGatewayNativeIdentity
			}
		}
		if content := item["content"]; content != nil && !bytes.Equal(bytes.TrimSpace(content), []byte("null")) {
			return nil, ErrGatewayNativeIdentity
		}
		if status := item["status"]; status != nil && string(bytes.TrimSpace(status)) != `"completed"` {
			return nil, ErrGatewayNativeIdentity
		}
		if id := item["id"]; id != nil && !gatewayNativeProviderString(id) {
			return nil, ErrGatewayNativeIdentity
		}
		var value string
		var summary []apicompat.ResponsesSummary
		var summaryParts []json.RawMessage
		if json.Unmarshal(item["encrypted_content"], &value) != nil || !strings.HasPrefix(value, gatewayClaudeContinuationPrefix) ||
			len(value) > apicompat.GeminiContinuationMaxBytes*2 || seen[value] || json.Unmarshal(item["summary"], &summary) != nil || len(summary) > 1 ||
			json.Unmarshal(item["summary"], &summaryParts) != nil || bytes.Equal(bytes.TrimSpace(item["summary"]), []byte("null")) {
			return nil, ErrGatewayNativeIdentity
		}
		for _, part := range summaryParts {
			if _, valid := gatewayClaudeObject(part, "type", "text"); !valid {
				return nil, ErrGatewayNativeIdentity
			}
		}
		for _, part := range summary {
			if part.Type != "summary_text" || len(part.Text) > 64<<10 {
				return nil, ErrGatewayNativeIdentity
			}
		}
		seen[value], changed = true, true
		if codec == nil {
			continue
		}
		plain, err := codec.Open(strings.TrimPrefix(value, gatewayClaudeContinuationPrefix))
		if err != nil || len(plain) > apicompat.GeminiContinuationMaxBytes {
			clear(plain)
			return nil, ErrGatewayNativeIdentity
		}
		var payload gatewayClaudeThinkingPayload
		_, valid = gatewayClaudeObject(plain, "version", "envelope", "summary")
		err = json.Unmarshal(plain, &payload)
		clear(plain)
		block, blockValid := gatewayClaudeSignedThinking(payload.Envelope)
		if !valid || err != nil || payload.Version != 1 || !blockValid || !gatewayClaudeThinkingSummaryMatches(block, summary) || !gatewayClaudeThinkingSummaryMatches(block, payload.Summary) {
			return nil, ErrGatewayNativeIdentity
		}
		item["encrypted_content"], err = json.Marshal(payload.Envelope)
		if err != nil {
			return nil, ErrGatewayNativeIdentity
		}
		delete(item, "content")
		decoded, err := json.Marshal(item)
		if err != nil {
			return nil, ErrGatewayNativeIdentity
		}
		projected = append(projected, decoded)
	}
	if !changed {
		return body, nil
	}
	var err error
	fields["input"], err = json.Marshal(projected)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	return json.Marshal(fields)
}

// A stream emits the same sealed item in output_item.done and response.completed.
// Cache only for this owned response lifetime; no conversation database exists.
func gatewayClaudeSealThinkingEvent(event apicompat.ResponsesStreamEvent, codec apicompat.GeminiContinuationCodec, sealed map[string]string) (apicompat.ResponsesStreamEvent, error) {
	seal := func(item apicompat.ResponsesOutput) (apicompat.ResponsesOutput, error) {
		if item.Type != "reasoning" {
			return item, nil
		}
		block, valid := gatewayClaudeSignedThinking(item.EncryptedContent)
		if !valid || !gatewayClaudeThinkingSummaryMatches(block, item.Summary) {
			return item, ErrGatewayNativeEffectUnknown
		}
		summary := item.Summary
		if summary == nil {
			summary = []apicompat.ResponsesSummary{}
		}
		payload, err := json.Marshal(gatewayClaudeThinkingPayload{Version: 1, Envelope: item.EncryptedContent, Summary: summary})
		if err != nil || len(payload) > apicompat.GeminiContinuationMaxBytes {
			return item, ErrGatewayNativeEffectUnknown
		}
		key := string(payload)
		value := sealed[key]
		if value == "" {
			value, err = codec.Seal(payload)
			if err != nil {
				return item, ErrGatewayNativeEffectUnknown
			}
			sealed[key] = value
		}
		item.EncryptedContent = gatewayClaudeContinuationPrefix + value
		return item, nil
	}
	if event.Item != nil && event.Type != "response.output_item.added" {
		item, err := seal(*event.Item)
		if err != nil {
			return event, err
		}
		event.Item = &item
	}
	if event.Response != nil {
		response := *event.Response
		response.Output = append([]apicompat.ResponsesOutput(nil), response.Output...)
		for index, item := range response.Output {
			var err error
			response.Output[index], err = seal(item)
			if err != nil {
				return event, err
			}
		}
		event.Response = &response
	}
	return event, nil
}
