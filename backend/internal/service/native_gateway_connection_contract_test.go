package service

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayConnectionInputCommitmentBindsSelectionWithoutNormalizingSecrets(t *testing.T) {
	selection := GatewayConnectionSelection{ProfileID: GatewayCodexOAuthResponsesProfile, MethodID: "codex_session", MethodRevision: 1, ConfigurationGeneration: "binding-1"}
	input := map[string]string{"content": " {\"tokens\": {\"access_token\": \"test-only\"}} \n", "project": "project-a"}
	first, err := CanonicalGatewayConnectionInput(selection, input)
	require.NoError(t, err)
	reordered, err := CanonicalGatewayConnectionInput(selection, map[string]string{"project": input["project"], "content": input["content"]})
	require.NoError(t, err)
	require.Equal(t, first, reordered)
	for _, altered := range []GatewayConnectionSelection{
		{selection.ProfileID, "agent_identity", 1, "binding-1"},
		{selection.ProfileID, selection.MethodID, 2, "binding-1"},
		{selection.ProfileID, selection.MethodID, 1, "binding-2"},
	} {
		raw, err := CanonicalGatewayConnectionInput(altered, input)
		require.NoError(t, err)
		require.False(t, bytes.Equal(first, raw))
	}
	input["content"] = strings.TrimSpace(input["content"])
	trimmed, err := CanonicalGatewayConnectionInput(selection, input)
	require.NoError(t, err)
	require.NotEqual(t, first, trimmed, "provider document bytes must not be silently normalized")
	fields := []GatewayConnectionField{gatewayConnectionField("content", "Credential", "document", true, GatewayConnectionInputBytes)}
	require.NoError(t, ValidateGatewayConnectionInput(fields, map[string]string{"content": "fixture"}))
	for _, input := range []map[string]string{
		{"content": "fixture", "target_url": "https://not-allowed.invalid"},
		{"content": strings.Repeat("x", GatewayConnectionInputBytes+1)},
		{"content": string([]byte{0xff})},
		{},
	} {
		require.ErrorIs(t, ValidateGatewayConnectionInput(fields, input), ErrGatewayNativeIdentity)
	}
}

func TestGatewayConnectionLegacyAliasesCannotGrantNewCredentialPolicies(t *testing.T) {
	profile, ok := LookupGatewayConnectionProfile(GatewayCodexOAuthResponsesProfile)
	require.True(t, ok)
	var session, refresh GatewayConnectionMethodDefinition
	for _, m := range profile.Methods {
		switch m.MethodID {
		case "codex_session":
			session = m
		case "refresh_token":
			refresh = m
		}
	}
	_, ok = gatewayLegacyConnectionMethod(session, true)
	require.False(t, ok, "same document UI must not turn new mixed custody into an old import")
	legacy, ok := gatewayLegacyConnectionMethod(refresh, true)
	require.True(t, ok)
	require.Equal(t, "refresh_token", legacy.Method)
	refresh.AllowedCredentialPolicies = []string{"delegated_identity"}
	_, ok = gatewayLegacyConnectionMethod(refresh, true)
	require.False(t, ok)
	method := session.Descriptor("fixture-generation", true)
	require.True(t, GatewayConnectionMethodValid(method))
	method.Requirements = append(method.Requirements, "future_security_requirement")
	require.False(t, GatewayConnectionMethodValid(method))
	method = session.Descriptor("fixture-generation", true)
	method.AllowedCredentialPolicies = append(method.AllowedCredentialPolicies, "future_policy")
	require.False(t, GatewayConnectionMethodValid(method))
}
