//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayNativeUsageContextCapturedBeforeEntryAndFrozen(t *testing.T) {
	publication := GatewayConnectionPublication{
		Connection:     GatewayConnectionSelection{ProfileID: "openai-api-responses-apikey-v1", MethodID: "api_key", MethodRevision: 1, ConfigurationGeneration: "source-config-v1"},
		Credential:     GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"},
		DispatchFamily: "openai_responses", AuthFamily: "provider_api_key",
	}
	valid := publication
	incompatible := publication
	incompatible.DispatchFamily = "anthropic_messages"
	var observations []GatewayNativeUsage
	ctx := WithGatewayNativeUsageObserver(context.Background(), func(usage GatewayNativeUsage) { observations = append(observations, usage) })
	var entered atomic.Bool
	bound, finish, err := withGatewayNativeConnectionUsage(ctx, publication, "actual-request-model", &entered)
	require.NoError(t, err)
	finish()
	require.Empty(t, observations, "no fallback observation before provider entry")
	observe, ok := bound.Value(gatewayNativeUsageObserverKey{}).(func(GatewayNativeUsage))
	require.True(t, ok)
	input := GatewayNativeUsage{Source: "native_provider_response", State: "unknown", CostState: "unknown", Finalized: true,
		NativeContext: &GatewayNativeUsageContext{ProviderID: "untrusted-reprojection", Billing: GatewayNativeUsageBilling{Status: "known"}}}
	observe(input)
	require.Nil(t, observations[0].NativeContext, "no source identity is asserted before the actual provider entry")
	publication.Connection.ProfileID = "retired-or-relabelled-profile"
	publication.DispatchFamily = "retired-family"
	entered.Store(true)
	observe(input)
	want := &GatewayNativeUsageContext{ProviderID: "openai", ProductID: "pay_as_you_go", ProfileID: "openai-api-responses-apikey-v1",
		ModelID: "actual-request-model", Billing: GatewayNativeUsageBilling{Status: "unknown"}}
	require.Equal(t, want, observations[1].NativeContext)
	observations[1].NativeContext.ProviderID = "mutated-receipt"
	observations[1].NativeContext.Billing.Status = "known"
	observe(input)
	require.Equal(t, want, observations[2].NativeContext, "a receipt cannot mutate the captured native facts")
	raw, err := json.Marshal(observations[2])
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"cost":`)
	require.Contains(t, string(raw), `"billing":{"status":"unknown"}`)
	finish()
	require.Len(t, observations, 3, "finalization cannot duplicate a provider observation")
	_, finishUnknown, err := withGatewayNativeConnectionUsage(ctx, valid, "actual-request-model", &entered)
	require.NoError(t, err)
	finishUnknown()
	finishUnknown()
	require.Len(t, observations, 4)
	require.Equal(t, want, observations[3].NativeContext)
	require.Equal(t, "unknown", observations[3].State)
	require.True(t, observations[3].Finalized)
	require.Nil(t, observations[3].InputTokens)
	require.Nil(t, observations[3].OutputTokens)
	for _, invalid := range []GatewayConnectionPublication{publication, incompatible} {
		_, _, err := withGatewayNativeConnectionUsage(ctx, invalid, "actual-request-model", &entered)
		require.ErrorIs(t, err, ErrGatewayNativeIdentity, "missing or incompatible native definition cannot mint context")
	}
}
