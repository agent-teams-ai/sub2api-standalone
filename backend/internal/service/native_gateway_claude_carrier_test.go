//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func claudeCarrierContext(t *testing.T, scope GatewayNativeCredentialScope, selection *GatewayConnectionSelection) context.Context {
	t.Helper()
	ctx, err := WithGatewayNativeConsumer(context.Background(), scope.Consumer)
	require.NoError(t, err)
	ctx, err = WithGatewayNativeOAuthOwner(ctx, scope.Owner)
	require.NoError(t, err)
	if selection != nil {
		ctx, err = WithGatewayConnectionSelection(ctx, *selection)
		require.NoError(t, err)
	}
	return ctx
}

func TestGatewayClaudeContinuationBindsCurrentOwnerBirthMethodAndCredentialRevision(t *testing.T) {
	scope := custodyTestScope()
	scope.Purpose = GatewayOAuthBundlePurpose
	selection := GatewayConnectionSelection{ProfileID: GatewayClaudeOAuthMessagesProfile, MethodID: "cookie_oauth", MethodRevision: 1, ConfigurationGeneration: "fixture-generation"}
	route := GatewayNativeRoute{AccountID: 41, Generation: scope.Generation, CreatedAt: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC), Profile: GatewayClaudeOAuthMessagesProfile, BaseURL: GatewayClaudeOAuthBaseURL, Model: GatewayClaudeOAuthModel}
	publication := GatewayConnectionPublication{Connection: selection, Credential: GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native"}, DispatchFamily: "claude_messages", AuthFamily: "claude_oauth"}
	custody := custodyTestKeyring(t, "claude-fixture")
	codec, err := gatewayClaudeContinuation(claudeCarrierContext(t, scope, &selection), custody, scope, route, &selection, &publication, 2)
	require.NoError(t, err)
	sealed, err := codec.Seal([]byte("source-issued-signature"))
	require.NoError(t, err)
	for _, change := range []struct {
		name  string
		apply func(*GatewayNativeCredentialScope, *GatewayNativeRoute, *GatewayConnectionPublication, *int64)
	}{
		{"owner", func(s *GatewayNativeCredentialScope, _ *GatewayNativeRoute, _ *GatewayConnectionPublication, _ *int64) {
			s.Owner = "foreign-owner"
		}},
		{"account", func(s *GatewayNativeCredentialScope, _ *GatewayNativeRoute, _ *GatewayConnectionPublication, _ *int64) {
			s.Account = "other-account"
		}},
		{"generation", func(s *GatewayNativeCredentialScope, r *GatewayNativeRoute, _ *GatewayConnectionPublication, _ *int64) {
			s.Generation = "b1111111-1111-4111-8111-111111111111"
			r.Generation = s.Generation
		}},
		{"physical-birth", func(_ *GatewayNativeCredentialScope, r *GatewayNativeRoute, _ *GatewayConnectionPublication, _ *int64) {
			r.CreatedAt = r.CreatedAt.Add(time.Second)
		}},
		{"physical-account", func(_ *GatewayNativeCredentialScope, r *GatewayNativeRoute, _ *GatewayConnectionPublication, _ *int64) {
			r.AccountID++
		}},
		{"method", func(_ *GatewayNativeCredentialScope, _ *GatewayNativeRoute, p *GatewayConnectionPublication, _ *int64) {
			p.Connection.MethodID = "setup_token_cookie"
		}},
		{"credential-revision", func(_ *GatewayNativeCredentialScope, _ *GatewayNativeRoute, _ *GatewayConnectionPublication, revision *int64) {
			*revision++
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			s, r, p, revision := scope, route, publication, int64(2)
			change.apply(&s, &r, &p, &revision)
			other, err := gatewayClaudeContinuation(claudeCarrierContext(t, s, &p.Connection), custody, s, r, &p.Connection, &p, revision)
			require.NoError(t, err)
			_, err = other.Open(sealed)
			require.ErrorIs(t, err, ErrGatewayNativeIdentity)
		})
	}
	route.CreatedAt = route.CreatedAt.In(time.FixedZone("same-instant", 3600))
	same, err := gatewayClaudeContinuation(claudeCarrierContext(t, scope, &selection), custody, scope, route, &selection, &publication, 2)
	require.NoError(t, err)
	_, err = same.Open(sealed)
	require.NoError(t, err)
	shared, ok := codec.(*gatewayGeminiContinuationCodec)
	require.True(t, ok)
	wrongDomain := *shared
	wrongDomain.domain = ""
	_, err = wrongDomain.Open(sealed)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity, "the shared keyring never allows cross-provider carrier interpretation")
	_, err = gatewayClaudeContinuation(context.Background(), custody, scope, route, &selection, &publication, 2)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity)
}

func TestGatewayClaudeThinkingCarrierSealsExactNativeOutputAndRejectsTampering(t *testing.T) {
	scope := custodyTestScope()
	scope.Purpose = GatewayOAuthBundlePurpose
	route := GatewayNativeRoute{AccountID: 41, Generation: scope.Generation, CreatedAt: time.Now().UTC(), Profile: GatewayClaudeOAuthMessagesProfile, BaseURL: GatewayClaudeOAuthBaseURL, Model: GatewayClaudeOAuthModel}
	codec, err := gatewayClaudeContinuation(claudeCarrierContext(t, scope, nil), custodyTestKeyring(t, "claude-fixture"), scope, route, nil, nil, 1)
	require.NoError(t, err)
	for _, block := range []apicompat.AnthropicContentBlock{
		{Type: "thinking", Thinking: "Read the source first.", Signature: "source-issued-signature"},
		{Type: "redacted_thinking", Data: "source-issued-redacted-data"},
	} {
		t.Run(block.Type, func(t *testing.T) {
			raw, err := json.Marshal(block)
			require.NoError(t, err)
			item := apicompat.ResponsesOutput{Type: "reasoning", ID: "rs_source", Status: "completed", EncryptedContent: gatewayClaudeSignedEnvelopePrefix + base64.RawStdEncoding.EncodeToString(raw)}
			if block.Thinking != "" {
				item.Summary = []apicompat.ResponsesSummary{{Type: "summary_text", Text: block.Thinking}}
			}
			cache := map[string]string{}
			added, err := gatewayClaudeSealThinkingEvent(apicompat.ResponsesStreamEvent{Type: "response.output_item.added", Item: &apicompat.ResponsesOutput{Type: "reasoning"}}, codec, cache)
			require.NoError(t, err)
			require.Empty(t, added.Item.EncryptedContent)
			done, err := gatewayClaudeSealThinkingEvent(apicompat.ResponsesStreamEvent{Type: "response.output_item.done", Item: &item}, codec, cache)
			require.NoError(t, err)
			completed, err := gatewayClaudeSealThinkingEvent(apicompat.ResponsesStreamEvent{Type: "response.completed", Response: &apicompat.ResponsesResponse{Output: []apicompat.ResponsesOutput{item}}}, codec, cache)
			require.NoError(t, err)
			require.Equal(t, done.Item.EncryptedContent, completed.Response.Output[0].EncryptedContent)
			require.True(t, strings.HasPrefix(item.EncryptedContent, gatewayClaudeSignedEnvelopePrefix), "stream state remains untouched")
			require.NotContains(t, done.Item.EncryptedContent, "source-issued")
			bodyFor := func(input any) []byte {
				body, err := json.Marshal(map[string]any{"model": GatewayClaudeOAuthModel, "input": input, "stream": true, "store": false, "max_output_tokens": 8192, "reasoning": map[string]string{"effort": "medium"}})
				require.NoError(t, err)
				return body
			}
			body := bodyFor([]any{done.Item, map[string]string{"role": "user", "content": "Continue"}})
			require.NoError(t, ValidateGatewayNativeClaudeResponsesBounded(body, 1<<20))
			restored, err := gatewayClaudeRestoreThinking(body, codec)
			require.NoError(t, err)
			require.Contains(t, string(restored), item.EncryptedContent)
			_, _, _, _, err = gatewayClaudePrepareBoundedResponses(restored, 1<<20)
			require.NoError(t, err)
			tampered := *done.Item
			tampered.Summary = []apicompat.ResponsesSummary{{Type: "summary_text", Text: "Altered source reasoning"}}
			_, err = gatewayClaudeRestoreThinking(bodyFor([]any{tampered}), codec)
			require.ErrorIs(t, err, ErrGatewayNativeIdentity)
			tampered = *done.Item
			replacement := "A"
			if strings.HasSuffix(tampered.EncryptedContent, replacement) {
				replacement = "B"
			}
			tampered.EncryptedContent = tampered.EncryptedContent[:len(tampered.EncryptedContent)-1] + replacement
			_, err = gatewayClaudeRestoreThinking(bodyFor([]any{tampered}), codec)
			require.ErrorIs(t, err, ErrGatewayNativeIdentity, "ciphertext tampering is rejected before native dispatch")
			_, err = gatewayClaudeRestoreThinking(bodyFor([]any{item}), codec)
			require.ErrorIs(t, err, ErrGatewayNativeIdentity, "raw provider signatures are never accepted from an HTTP caller")
			_, err = gatewayClaudeRestoreThinking(bodyFor([]any{done.Item, done.Item}), codec)
			require.ErrorIs(t, err, ErrGatewayNativeIdentity)
		})
	}
}
