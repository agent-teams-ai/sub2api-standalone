package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

// NewGatewayNativeCodexConnectionAdapter mounts only the existing qualified
// refreshable flows. Session/PAT/Agent Identity have different custody and are
// intentionally absent until their concrete native adapters are composed.
func NewGatewayNativeCodexConnectionAdapter(connect *GatewayNativeOAuthConnect, dispatch *GatewayNativeOAuthDispatch, refresh *GatewayNativeOAuthRefresh, cleanup GatewayNativeOAuthCleanupAuthorizer) (GatewayNativeConnectionAdapter, error) {
	if connect == nil || dispatch == nil || refresh == nil || cleanup == nil || connect.authorizeEntry == nil || refresh.authorizeEntry == nil {
		return GatewayNativeConnectionAdapter{}, ErrGatewayNativeIdentity
	}
	importInput := func(ctx context.Context, in GatewayNativeConnectionScope, input map[string]string) (GatewayNativeOAuthConnectResult, error) {
		field := "refresh_token"
		if in.Connection.MethodID == "auth_json" {
			field = "content"
		}
		return connect.ImportConnect(ctx, in.Scope, in.Operation, in.Connection.MethodID, input[field])
	}
	return newGatewayBoundOAuthAdapter(GatewayCodexOAuthResponsesProfile, "codex_responses", "codex_oauth", []string{"browser", "device_code", "refresh_token", "auth_json"}, gatewayBoundOAuthPorts{
		begin: func(ctx context.Context, in GatewayNativeConnectionScope, input map[string]string) (GatewayNativeOAuthConnectResult, error) {
			if GatewayNativeOAuthImportMethodValid(in.Connection.MethodID) {
				return importInput(ctx, in, input)
			}
			return connect.BeginConnectMethod(ctx, in.Scope, in.Operation, in.Connection.MethodID)
		},
		read: connect.ReadConnect,
		submit: func(ctx context.Context, in GatewayNativeConnectionScope, input map[string]string) (GatewayNativeOAuthConnectResult, error) {
			if GatewayNativeOAuthImportMethodValid(in.Connection.MethodID) {
				return importInput(ctx, in, input)
			}
			state, code, err := gatewayConnectionCallback(input["completion"], GatewayOAuthConnectRedirect)
			if err != nil {
				return GatewayNativeOAuthConnectResult{}, err
			}
			return connect.CompleteOwnerCallback(ctx, in.Scope, in.Operation, state, code)
		},
		poll: connect.PollDeviceConnect, describe: dispatch.Descriptor, refresh: refresh.Maintain, custody: dispatch.RetainedCustody,
		cleanup: func(ctx context.Context, request GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error) {
			return connect.Cleanup(ctx, request, cleanup)
		},
	})
}

func NewGatewayNativeClaudeConnectionAdapter(native *GatewayNativeClaudeOAuth, cleanup GatewayNativeOAuthCleanupAuthorizer) (GatewayNativeConnectionAdapter, error) {
	if native == nil || cleanup == nil || native.connectEntry == nil || native.refreshEntry == nil {
		return GatewayNativeConnectionAdapter{}, ErrGatewayNativeIdentity
	}
	return newGatewayBoundOAuthAdapter(GatewayClaudeOAuthMessagesProfile, "claude_messages", "claude_oauth", []string{"browser"}, gatewayBoundOAuthPorts{
		begin: func(ctx context.Context, in GatewayNativeConnectionScope, _ map[string]string) (GatewayNativeOAuthConnectResult, error) {
			return native.BeginConnectMethod(ctx, in.Scope, in.Operation, in.Connection.MethodID)
		},
		read: native.ReadConnect,
		submit: func(ctx context.Context, in GatewayNativeConnectionScope, input map[string]string) (GatewayNativeOAuthConnectResult, error) {
			completion := input["completion"]
			if strings.Contains(completion, "://") {
				state, code, err := gatewayConnectionCallback(completion, oauth.RedirectURI)
				if err != nil {
					return GatewayNativeOAuthConnectResult{}, err
				}
				return native.CompleteOwnerCallback(ctx, in.Scope, in.Operation, state, code)
			}
			return native.CompleteOwnerCode(ctx, in.Scope, in.Operation, completion)
		},
		describe: native.Descriptor, refresh: native.Maintain, custody: native.RetainedCustody,
		cleanup: func(ctx context.Context, request GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error) {
			return native.Cleanup(ctx, request, cleanup)
		},
	})
}

func NewGatewayNativeGrokConnectionAdapter(native *GatewayNativeGrokOAuth, cleanup GatewayNativeOAuthCleanupAuthorizer) (GatewayNativeConnectionAdapter, error) {
	if native == nil || cleanup == nil || native.connectEntry == nil || native.refreshEntry == nil {
		return GatewayNativeConnectionAdapter{}, ErrGatewayNativeIdentity
	}
	return newGatewayBoundOAuthAdapter(GatewayGrokOAuthResponsesProfile, "grok_responses", "grok_oauth", []string{"browser"}, gatewayBoundOAuthPorts{
		begin: func(ctx context.Context, in GatewayNativeConnectionScope, _ map[string]string) (GatewayNativeOAuthConnectResult, error) {
			return native.BeginConnectMethod(ctx, in.Scope, in.Operation, in.Connection.MethodID)
		},
		read: native.ReadConnect,
		submit: func(ctx context.Context, in GatewayNativeConnectionScope, input map[string]string) (GatewayNativeOAuthConnectResult, error) {
			completion := input["completion"]
			if strings.Contains(completion, "://") {
				state, code, err := gatewayConnectionCallback(completion, native.qualification.RedirectURI)
				if err != nil {
					return GatewayNativeOAuthConnectResult{}, err
				}
				return native.CompleteOwnerCallback(ctx, in.Scope, in.Operation, state, code)
			}
			return native.CompleteOwnerCode(ctx, in.Scope, in.Operation, completion)
		},
		describe: native.Descriptor, refresh: native.Maintain, custody: native.RetainedCustody,
		cleanup: func(ctx context.Context, request GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error) {
			return native.Cleanup(ctx, request, cleanup)
		},
	})
}

// CompleteOwnerCode recovers state from this owner's sealed original intent.
// A terminal or uncertain state is readback only, never a new provider exchange.
func (s *GatewayNativeClaudeOAuth) CompleteOwnerCode(ctx context.Context, scope GatewayNativeCredentialScope, operation, code string) (GatewayNativeOAuthConnectResult, error) {
	var empty GatewayNativeOAuthConnectResult
	if s == nil || !claudeScope(ctx, scope, operation) || !connectCodeValid(code) || strings.ContainsAny(code, "?&") {
		return empty, ErrGatewayNativeIdentity
	}
	in, err := s.repository.ReadGatewayClaudeConnect(ctx, scope, operation)
	if err != nil || !GatewayConnectionSelectionAuthorized(ctx, in.Connection) || in.Scope != scope || in.Operation != operation {
		return empty, ErrGatewayNativeIdentity
	}
	switch in.State {
	case "completed", "exchanging", "erased":
		return claudeConnectView(in), nil
	case "prepared":
		if !time.Now().Before(in.Deadline) {
			return claudeConnectView(in), nil
		}
	default:
		return empty, ErrGatewayNativeIdentity
	}
	var secret gatewayClaudeConnectSecret
	if s.open(scope, in.Envelope, &secret) != nil || !SameGatewayConnectionSelection(secret.Connection, in.Connection) || secret.Kind != "claude-connect/v1" {
		return empty, ErrGatewayNativeIdentity
	}
	digest := sha256.Sum256([]byte(secret.State))
	if hex.EncodeToString(digest[:]) != in.StateHash {
		return empty, ErrGatewayNativeIdentity
	}
	return s.CompleteOwnerCallback(ctx, scope, operation, secret.State, code)
}
