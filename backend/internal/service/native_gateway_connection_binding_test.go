package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayConnectionBindingSurvivesReadbackAndCannotRetargetPreparedIntent(t *testing.T) {
	connect, repo, legacyContext, scope := connectPureFixture(t)
	selection := GatewayConnectionSelection{ProfileID: GatewayCodexOAuthResponsesProfile, MethodID: "browser", MethodRevision: 1, ConfigurationGeneration: "original-target"}
	ctx, err := WithGatewayConnectionSelection(legacyContext, selection)
	require.NoError(t, err)
	first, err := connect.BeginConnect(ctx, scope, "bound-operation")
	require.NoError(t, err)
	require.Equal(t, &selection, first.Connection)
	read, err := connect.ReadConnect(ctx, scope, "bound-operation")
	require.NoError(t, err)
	require.Equal(t, &selection, read.Connection)
	require.Empty(t, read.AuthorizeURL)
	_, err = connect.ReadConnect(legacyContext, scope, "bound-operation")
	require.ErrorIs(t, err, ErrGatewayNativeIdentity, "new selection cannot disappear through an old endpoint")
	changed := selection
	changed.ConfigurationGeneration = "different-target"
	changedContext, err := WithGatewayConnectionSelection(legacyContext, changed)
	require.NoError(t, err)
	_, err = connect.BeginConnect(changedContext, scope, "bound-operation")
	require.ErrorIs(t, err, ErrGatewayOAuthConflict)
	_, err = connect.ReadConnect(changedContext, scope, "bound-operation")
	require.ErrorIs(t, err, ErrGatewayNativeIdentity)
	tampered := repo.row
	tampered.Connection = &changed
	_, err = connect.open(tampered)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity, "encrypted prepared material binds the immutable selection")
	_, err = connect.open(repo.row)
	require.NoError(t, err)
}

func TestGatewayConnectionCredentialEncodingPreservesLegacyAndRejectsLostBinding(t *testing.T) {
	legacy := GatewayNativeOAuthBundle{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", IDToken: "fixture-id", SensitiveMetadata: json.RawMessage(`{}`)}
	raw, err := legacy.bytes()
	require.NoError(t, err)
	require.NotContains(t, string(raw), "connection")
	decoded, err := gatewayOAuthDecodeBundle(raw)
	require.NoError(t, err)
	require.Nil(t, decoded.Connection)
	selection := GatewayConnectionSelection{ProfileID: GatewayCodexOAuthResponsesProfile, MethodID: "auth_json", MethodRevision: 1, ConfigurationGeneration: "binding-1"}
	bound := legacy
	bound.Connection = &selection
	raw, err = bound.bytes()
	require.NoError(t, err)
	decoded, err = gatewayOAuthDecodeBundle(raw)
	require.NoError(t, err)
	require.Equal(t, &selection, decoded.Connection)
	for _, invalid := range []string{
		`{"access_token":"a","refresh_token":"r","id_token":"i","sensitive_metadata":{},"connection":null}`,
		`{"access_token":"a","refresh_token":"r","id_token":"i","sensitive_metadata":{},"unknown":"ignored"}`,
	} {
		_, err = gatewayOAuthDecodeBundle([]byte(invalid))
		require.ErrorIs(t, err, ErrGatewayNativeIdentity)
	}
}
