package service

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayConnectionCustodyCannotRelabelNativeCredential(t *testing.T) {
	custody, err := NewGatewayNativeCredentialCustody("native-test", map[string][]byte{"native-test": bytes.Repeat([]byte{0x42}, 32)})
	require.NoError(t, err)
	scope := GatewayNativeCredentialScope{Consumer: "test-consumer", Owner: "owner-a", Account: "account-a", Generation: "a1111111-1111-4111-8111-111111111111", Purpose: GatewayOAuthBundlePurpose}
	publication := GatewayConnectionPublication{Connection: GatewayConnectionSelection{ProfileID: GatewayCodexOAuthResponsesProfile, MethodID: "codex_pat", MethodRevision: 1, ConfigurationGeneration: "config-a"},
		Credential: GatewayCredentialAttestation{CredentialKind: "native", CredentialPolicy: "subscription_session", RefreshPolicy: "none"}, DispatchFamily: "codex_responses", AuthFamily: "codex_pat"}
	document := json.RawMessage(`{"access_token":"at-synthetic-only","auth_mode":"personal_access_token"}`)
	envelope, err := custody.SealConnection(scope, publication, 1, document)
	require.NoError(t, err)
	require.NotContains(t, envelope, "at-synthetic-only")
	raw, err := custody.OpenConnection(scope, publication, 1, envelope)
	require.NoError(t, err)
	require.JSONEq(t, string(document), string(raw))
	clear(raw)
	for _, change := range []func(*GatewayConnectionPublication){
		func(p *GatewayConnectionPublication) { p.Connection.MethodID = "codex_session" },
		func(p *GatewayConnectionPublication) { p.Connection.ConfigurationGeneration = "config-b" },
		func(p *GatewayConnectionPublication) { p.Credential.RefreshPolicy = "native" },
		func(p *GatewayConnectionPublication) { p.Credential.CredentialKind = "oauth" },
		func(p *GatewayConnectionPublication) { p.AuthFamily = "codex_oauth" },
		func(p *GatewayConnectionPublication) { p.DispatchFamily = "openai_responses" },
	} {
		changed := publication
		change(&changed)
		_, err := custody.OpenConnection(scope, changed, 1, envelope)
		require.ErrorIs(t, err, ErrGatewayNativeIdentity)
	}
	foreign := scope
	foreign.Owner = "owner-b"
	_, err = custody.OpenConnection(foreign, publication, 1, envelope)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity)
	_, err = custody.OpenConnection(scope, publication, 2, envelope)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity)
	_, err = custody.openOAuthVersion(envelope, scope, 1)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity, "a native identity must never be decoded as a legacy refreshable OAuth bundle")
}
