//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Static importers deliberately do no upstream probe. Inspect the real active
// vault only inside Native, before retirement; never export its material or
// claim a second provider request. Both physical births are checked at erasure.
func requireNativeConnectionJoinedReconnect(t *testing.T, ctx context.Context, build nativeConnectionJoinedBuild, row nativeConnectionJoinedRow, consumer, accountRef, generation string) {
	t.Helper()
	var id int64
	require.NoError(t, build.DB.QueryRowContext(ctx, `SELECT outcome_account_id FROM gateway_oauth_connect_intents
 WHERE consumer=$1 AND account_ref=$2 AND generation=$3 AND client_id='native-connection-v1' AND state='completed'`, consumer, accountRef, generation).Scan(&id))
	account, err := build.Repository.GetByID(ctx, id)
	require.NoError(t, err)
	scope, err := service.GatewayNativeConnectionStoredScope(account)
	require.NoError(t, err)
	require.Equal(t, consumer, scope.Consumer)
	require.Equal(t, accountRef, scope.Account)
	require.Equal(t, generation, scope.Generation)
	publication, err := service.GatewayNativeStoredConnectionPublication(account)
	require.NoError(t, err)
	require.Equal(t, row.ProfileID, publication.Connection.ProfileID)
	require.Equal(t, row.MethodID, publication.Connection.MethodID)
	revision, err := service.GatewayNativeConnectionCredentialRevision(account)
	require.NoError(t, err)
	raw, err := build.Custody.OpenConnection(scope, publication, revision, account.GetCredential(service.GatewayConnectionBundleCredentialKey))
	require.NoError(t, err)
	defer clear(raw)
	var document map[string]string
	require.NoError(t, json.Unmarshal(raw, &document))
	defer clear(document)
	require.True(t, nativeConnectionJoinedReplacementMatches(row, document), "active vault must contain the distinct replacement and preserve its configured context")
}

func nativeConnectionJoinedReplacementMatches(row nativeConnectionJoinedRow, actual map[string]string) bool {
	input := row.ReconnectFields
	expected := maps.Clone(input)
	switch row.MethodID {
	case "aws_sigv4":
		expected = map[string]string{"aws_access_key_id": input["access_key_id"], "aws_secret_access_key": input["secret_access_key"], "aws_region": input["region"]}
		if input["session_token"] != "" {
			expected["aws_session_token"] = input["session_token"]
		}
		if input["access_key_id"] == row.Fields["access_key_id"] || input["secret_access_key"] == row.Fields["secret_access_key"] || input["region"] != row.Fields["region"] {
			return false
		}
	case "bedrock_api_key":
		expected = map[string]string{"api_key": input["api_key"], "auth_mode": "apikey", "aws_region": input["region"]}
		if input["api_key"] == row.Fields["api_key"] || input["region"] != row.Fields["region"] {
			return false
		}
	case "vertex_api_key":
		expected["auth_mode"], expected["project_id"], expected["location"] = "apikey", input["project_id"], input["location"]
		if expected["location"] == "" {
			expected["location"] = "us-central1"
		}
		if input["api_key"] == row.Fields["api_key"] || input["project_id"] != row.Fields["project_id"] || input["location"] != row.Fields["location"] {
			return false
		}
	case "vertex_service_account":
		var before, replacement, stored map[string]string
		if json.Unmarshal([]byte(row.Fields["service_account_json"]), &before) != nil || json.Unmarshal([]byte(input["service_account_json"]), &replacement) != nil || json.Unmarshal([]byte(actual["service_account_json"]), &stored) != nil {
			return false
		}
		defer clear(before)
		defer clear(replacement)
		defer clear(stored)
		if replacement["private_key"] == before["private_key"] || replacement["client_email"] != before["client_email"] || replacement["project_id"] != before["project_id"] || input["project_id"] != row.Fields["project_id"] || input["location"] != row.Fields["location"] {
			return false
		}
		replacement["token_uri"] = "https://oauth2.googleapis.com/token"
		if !maps.Equal(replacement, stored) {
			return false
		}
		expected["service_account_json"] = actual["service_account_json"]
	case "api_key":
		if row.ProfileID != "antigravity-target-responses-apikey-v1" || input["api_key"] == row.Fields["api_key"] || input["target_id"] != row.Fields["target_id"] || input["project_id"] != row.Fields["project_id"] {
			return false
		}
		expected["base_url"] = "https://qualified-native.example.test"
	default:
		return false
	}
	return maps.Equal(expected, actual)
}
