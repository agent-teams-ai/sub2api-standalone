//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Regress the actual V2 JSON acknowledgement contract: an extra legacy method
// made Gateway reject a valid one-time handoff, then recover only a prepared
// readback without the authorization URL. The underlying V1 receipt is kept.
func TestGatewayConnectionOAuthV2WirePreservesHandoffAndRejectsMethodRetarget(t *testing.T) {
	for _, row := range []struct{ profile, method, dispatch, auth string }{
		{GatewayCodexOAuthResponsesProfile, "browser", "codex_responses", "codex_oauth"},
		{GatewayCodexOAuthResponsesProfile, "device_code", "codex_responses", "codex_oauth"},
		{GatewayClaudeOAuthMessagesProfile, "browser", "claude_messages", "claude_oauth"},
		{GatewayGrokOAuthResponsesProfile, "browser", "grok_responses", "grok_oauth"},
	} {
		t.Run(row.profile+"/"+row.method, func(t *testing.T) {
			_, _, ctx, scope := connectPureFixture(t)
			in := GatewayNativeConnectionScope{Scope: scope, Operation: "wire-operation", Connection: GatewayConnectionSelection{ProfileID: row.profile, MethodID: row.method, MethodRevision: 1, ConfigurationGeneration: "wire-generation"}}
			legacy := GatewayNativeOAuthConnectResult{Connection: &in.Connection, Operation: in.Operation, Account: scope.Account, State: "prepared", Method: row.method}
			if row.method == "browser" {
				legacy.AuthorizeURL = "https://native-authorize.example.test/?state=source-bound"
			} else {
				legacy.DeviceAuthorization = &GatewayNativeDeviceAuthorization{UserCode: "SOURCE-1234", VerificationURL: "https://native-authorize.example.test/device", ExpiresAt: time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC), IntervalSeconds: 3}
			}
			ports := gatewayBoundOAuthPorts{
				begin: func(context.Context, GatewayNativeConnectionScope, map[string]string) (GatewayNativeOAuthConnectResult, error) {
					return legacy, nil
				},
				read: func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthConnectResult, error) {
					out := legacy
					out.AuthorizeURL = ""
					out.DeviceAuthorization = nil
					return out, nil
				},
				submit: func(context.Context, GatewayNativeConnectionScope, map[string]string) (GatewayNativeOAuthConnectResult, error) {
					return GatewayNativeOAuthConnectResult{}, ErrGatewayNativeIdentity
				},
				describe: func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthDescriptorResult, error) {
					return GatewayNativeOAuthDescriptorResult{}, ErrGatewayNativeIdentity
				},
				refresh: func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthMaintenanceOutcome, error) {
					return GatewayNativeOAuthMaintenanceOutcome{}, ErrGatewayNativeIdentity
				},
				custody: func(context.Context, GatewayNativeCredentialScope, string, GatewayNativeRoute) (GatewayNativeRetainedCustodyResult, error) {
					return GatewayNativeRetainedCustodyResult{}, ErrGatewayNativeIdentity
				},
				cleanup: func(context.Context, GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error) {
					return GatewayNativeOAuthCleanupResult{}, ErrGatewayNativeIdentity
				},
			}
			adapter, err := newGatewayBoundOAuthAdapter(row.profile, row.dispatch, row.auth, []string{row.method}, ports)
			require.NoError(t, err)
			begun, err := adapter.Begin(ctx, in, map[string]string{})
			require.NoError(t, err)
			wire, err := json.Marshal(begun)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(wire, &fields))
			require.NotContains(t, fields, "method")
			require.Len(t, fields, 5)
			var selected GatewayConnectionSelection
			require.NoError(t, json.Unmarshal(fields["connection"], &selected))
			require.Equal(t, in.Connection, selected)
			require.Equal(t, legacy.AuthorizeURL, begun.AuthorizeURL)
			require.Equal(t, legacy.DeviceAuthorization, begun.DeviceAuthorization)
			require.Equal(t, row.method, legacy.Method, "the adapter must not change the legacy source receipt")
			read, err := adapter.Read(ctx, in)
			require.NoError(t, err)
			require.Empty(t, read.Method)
			require.Empty(t, read.AuthorizeURL)
			require.Nil(t, read.DeviceAuthorization)
			legacy.Method = "different-method"
			_, err = adapter.Begin(ctx, in, map[string]string{})
			require.ErrorIs(t, err, ErrGatewayNativeIdentity)
			_, err = adapter.Read(ctx, in)
			require.ErrorIs(t, err, ErrGatewayNativeIdentity)
		})
	}
}
