//go:build unit

package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func inventoryWireFixture(t *testing.T) PlatformNativeConnectionInventory {
	t.Helper()
	raw, err := os.ReadFile("testdata/native_connection_catalog_contract.json")
	require.NoError(t, err)
	var catalog PlatformCatalog
	require.NoError(t, json.Unmarshal(raw, &catalog))
	return catalog.NativeConnectionInventory
}

func TestGatewayConnectionInventoryDiffReportsActualContractChanges(t *testing.T) {
	before, after := inventoryWireFixture(t), inventoryWireFixture(t)
	// This independently declared grammar change is a diff oracle, not an
	// additional input claimed by the shared existing-form fixture.
	preflight := GatewayConnectionField{FieldID: "project_id", Kind: "text", Label: "Project", Required: true, MaxBytes: 128}
	before.Profiles[0].Methods[0].Interaction.PreflightFields = []GatewayConnectionField{preflight}
	after.Profiles[0].Methods[0].Interaction.PreflightFields = []GatewayConnectionField{preflight}
	removedMethod := before.Profiles[0].Methods[0]
	removedMethod.MethodID = "removed_method"
	before.Profiles[0].Methods = append(before.Profiles[0].Methods, removedMethod)
	removedProfile := inventoryWireFixture(t).Profiles[0]
	removedProfile.ProfileID = "removed-profile"
	before.Profiles = append(before.Profiles, removedProfile)
	after.Profiles[0].Product.ID = "new_plan"
	changed := &after.Profiles[0].Methods[0]
	changed.Revision = 2
	changed.Requirements = append(changed.Requirements, "new_security_requirement/v1")
	changed.AdminActions[0].ConfigurationRequirements = []string{"custom_oauth_client"}
	changed.Interaction.CallbackMode = "automatic"
	changed.Interaction.CompletionInputKind = "callback_url"
	changed.Interaction.PreflightFields[0].Required = false
	changed.Interaction.PreflightFields[0].MaxBytes = 256
	addedProfile := inventoryWireFixture(t).Profiles[0]
	addedProfile.ProfileID = "added-profile"
	after.Profiles = append(after.Profiles, addedProfile)
	report, err := diffConnectionInventories(before, after, "exported")
	require.NoError(t, err)
	products := map[string]string{}
	for _, change := range report.Products {
		products[change.ProfileID] = change.Change
	}
	require.Equal(t, map[string]string{"added-profile": "added", "fixture-native-admin-v1": "changed", "removed-profile": "removed"}, products)
	methods := map[string]string{}
	for _, change := range report.Methods {
		methods[change.ProfileID+"/"+change.MethodID] = change.Change
	}
	require.Equal(t, map[string]string{
		"added-profile/fixture_browser_grant": "added", "fixture-native-admin-v1/fixture_browser_grant": "changed",
		"fixture-native-admin-v1/removed_method": "removed", "removed-profile/fixture_browser_grant": "removed",
	}, methods)
	var changedInput, changedRequirement, changedRevision int
	for _, input := range report.Inputs {
		if input.ProfileID != "fixture-native-admin-v1" || input.MethodID != "fixture_browser_grant" {
			continue
		}
		changedInput++
		require.Equal(t, "changed", input.Change)
		switch input.Scope {
		case "preflight":
			require.Equal(t, "project_id", input.FieldID)
			require.JSONEq(t, `{"fieldId":"project_id","kind":"text","label":"Project","required":true,"maxBytes":128}`, string(input.Before))
			require.JSONEq(t, `{"fieldId":"project_id","kind":"text","label":"Project","required":false,"maxBytes":256}`, string(input.After))
		case "interaction":
			require.Empty(t, input.FieldID)
			require.JSONEq(t, `{"kind":"browser","callbackMode":"manual","completionInputKind":"authorization_code"}`, string(input.Before))
			require.JSONEq(t, `{"kind":"browser","callbackMode":"automatic","completionInputKind":"callback_url"}`, string(input.After))
		default:
			t.Fatalf("unexpected changed input scope: %s", input.Scope)
		}
	}
	for _, requirement := range report.Requirements {
		if requirement.Change != "changed" {
			continue
		}
		changedRequirement++
		switch requirement.Scope {
		case "security":
			require.JSONEq(t, `["owner_scoped_custody/v1","effect_fence/v1","native_dispatch/v1","new_security_requirement/v1"]`, string(requirement.After))
		case "admin":
			require.JSONEq(t, `[{"category":"oauth-based","add_method":"oauth","input_method":"manual","surfaces":["create","reauth","flow"],"configuration_requirements":[]}]`, string(requirement.Before))
			require.JSONEq(t, `[{"category":"oauth-based","add_method":"oauth","input_method":"manual","surfaces":["create","reauth","flow"],"configuration_requirements":["custom_oauth_client"]}]`, string(requirement.After))
		default:
			t.Fatalf("unexpected changed requirement scope: %s", requirement.Scope)
		}
	}
	for _, revision := range report.SemanticRevisions {
		if revision.Change != "changed" {
			continue
		}
		changedRevision++
		require.Equal(t, "1", string(revision.Before))
		require.Equal(t, "2", string(revision.After))
	}
	require.Equal(t, 2, changedInput)
	require.Equal(t, 2, changedRequirement)
	require.Equal(t, 1, changedRevision)
	same, err := diffConnectionInventories(after, after, "exported")
	require.NoError(t, err)
	require.Empty(t, same.Products)
	require.Empty(t, same.Methods)
	require.Empty(t, same.Inputs)
	require.Empty(t, same.Requirements)
	require.Empty(t, same.SemanticRevisions)
}

func TestGatewayConnectionInventoryDiffRejectsMalformedAndUnboundedSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"platforms":[],"composite_precedence":[]}`), 0o600))
	before, state, err := readConnectionInventoryBaseline(path)
	require.NoError(t, err)
	require.Equal(t, "not_exported", state)
	report, err := diffConnectionInventories(before, inventoryWireFixture(t), state)
	require.NoError(t, err)
	require.Len(t, report.Products, 1)
	require.Equal(t, "added", report.Products[0].Change)
	require.Len(t, report.Methods, 1)
	require.Equal(t, "added", report.Methods[0].Change)
	validExport, err := os.ReadFile("testdata/native_connection_catalog_contract.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, validExport, 0o600))
	exported, state, err := readConnectionInventoryBaseline(path)
	require.NoError(t, err)
	require.Equal(t, "exported", state)
	require.Equal(t, inventoryWireFixture(t), exported)
	for _, raw := range []string{
		`null`, `{`, `{}`,
		`{"platforms":null,"composite_precedence":[]}`,
		`{"platforms":{},"composite_precedence":[]}`,
		`{"platforms":[],"composite_precedence":null}`,
		`{"platforms":[],"composite_precedence":[17]}`,
		`{"platforms":[{"id":"openai","display_name":"OpenAI","gateway":"openai","cn_provider":"false"}],"composite_precedence":["openai"]}`,
		`{"platforms":[null],"composite_precedence":[]}`,
		`{"platforms":[],"composite_precedence":[],"native_connection_inventory":null}`,
		`{"platforms":[],"composite_precedence":[],"native_connection_inventory":{"contract":"unknown/v2","profiles":[]}}`,
		`{"platforms":[],"composite_precedence":[],"native_connection_inventory":{"contract":"native-connection-inventory/v1","profiles":[],"credentials":"forbidden"}}`,
		strings.Repeat(" ", connectionInventoryMaxBytes+1),
	} {
		require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
		_, _, err := readConnectionInventoryBaseline(path)
		require.Error(t, err)
	}
	duplicate := inventoryWireFixture(t)
	duplicate.Profiles = append(duplicate.Profiles, duplicate.Profiles[0])
	require.ErrorContains(t, validateConnectionInventory(duplicate), "profile_identity")
	duplicate = inventoryWireFixture(t)
	duplicate.Profiles[0].Methods = append(duplicate.Profiles[0].Methods, duplicate.Profiles[0].Methods[0])
	require.ErrorContains(t, validateConnectionInventory(duplicate), "method_identity")
	duplicate = inventoryWireFixture(t)
	fields := &duplicate.Profiles[0].Methods[0].Interaction.PreflightFields
	field := GatewayConnectionField{FieldID: "duplicate", Kind: "text", Label: "Duplicate", Required: true, MaxBytes: 128}
	*fields = []GatewayConnectionField{field, field}
	require.ErrorContains(t, validateConnectionInventory(duplicate), "field_identity")
	oversize := inventoryWireFixture(t)
	oversize.Profiles[0].Product.Label = strings.Repeat("x", connectionInventoryMaxBytes)
	require.ErrorContains(t, validateConnectionInventory(oversize), "byte_bound")
}
