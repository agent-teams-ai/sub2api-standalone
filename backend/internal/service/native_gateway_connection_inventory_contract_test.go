//go:build unit

package service

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayConnectionInventorySharedWireExample(t *testing.T) {
	raw, err := os.ReadFile("testdata/native_connection_catalog_contract.json")
	require.NoError(t, err)
	var expected PlatformCatalog
	require.NoError(t, json.Unmarshal(raw, &expected))
	method := gatewayConnectionBrowser("fixture_browser_grant", "Fixture authorization", "manual", "authorization_code")
	method.AdminActions = []GatewayConnectionAdminAction{{Category: "oauth-based", AddMethod: "oauth", InputMethod: "manual",
		Surfaces: []string{"create", "reauth", "flow"}, ConfigurationRequirements: []string{}}}
	profile := gatewayConnectionProfile("fixture-native-admin-v1", PlatformOpenAI, "fixture", "Fixture provider", "fixture_plan", "Fixture plan", "openai_responses", method)
	// A new ID uses its declared, already implemented UI action. Neither the
	// projection nor the consumers need a new provider or method allowlist.
	declareGatewayConnectionAdminActions(&profile)
	definitions := []GatewayConnectionProfileDefinition{profile}
	require.Equal(t, expected.AdminConnectionMethods, buildPlatformAdminConnectionMethods(definitions))
	require.Equal(t, expected.NativeConnectionInventory, buildPlatformNativeConnectionInventory(definitions))
	require.NoError(t, validateConnectionInventory(expected.NativeConnectionInventory))
}

func TestGatewayConnectionInventoryAdminSurfacesAndRequirements(t *testing.T) {
	catalog := BuildPlatformCatalog()
	var mobile, studio, password, sso *PlatformAdminConnectionMethod
	providers := map[string]bool{}
	for index := range catalog.AdminConnectionMethods {
		entry := &catalog.AdminConnectionMethods[index]
		providers[entry.Provider.ID] = true
		require.NotEmpty(t, entry.Surfaces)
		require.NotNil(t, entry.ConfigurationRequirements)
		require.NotEqual(t, "session_token", entry.InputMethod)
		require.NotEqual(t, "access_token", entry.InputMethod)
		if entry.MethodID == "mobile_refresh_token" {
			mobile = entry
		}
		if entry.OAuthType == "ai_studio" {
			studio = entry
		}
		if entry.InputMethod == "email_password" {
			password = entry
		}
		if entry.InputMethod == "sso_cookie" {
			sso = entry
		}
	}
	require.NotNil(t, mobile)
	require.Equal(t, []string{"create", "flow"}, mobile.Surfaces)
	require.NotNil(t, studio)
	require.Equal(t, []string{"custom_oauth_client"}, studio.ConfigurationRequirements)
	require.NotNil(t, password)
	require.Equal(t, "Email and password", password.Label)
	require.NotNil(t, sso)
	require.Equal(t, "Grok SSO", sso.Label)
	require.Equal(t, []string{"flow"}, password.Surfaces)
	require.Equal(t, []string{"grok_password_login"}, password.ConfigurationRequirements)
	// These native profiles deliberately share the OpenAI storage platform.
	// An admin inventory must not lose their distinct provider/product facts.
	for _, provider := range []string{"mimo", "openrouter", "nvidia", "openai", "anthropic", "gemini", "grok", "kimi", "glm", "minimax", "deepseek", "opencode", "typesafe", "commandcode", "cline", "antigravity"} {
		require.True(t, providers[provider], provider)
	}
	require.NoError(t, validateConnectionInventory(catalog.NativeConnectionInventory))
	raw, err := json.Marshal(catalog.NativeConnectionInventory)
	require.NoError(t, err)
	for _, forbidden := range []string{`"configurationGeneration":`, `"mount":`, `"baseURL":`, `"credentials":`, `"ownerRef":`} {
		require.NotContains(t, string(raw), forbidden)
	}
}

func TestGatewayConnectionInventoryProjectionReturnsFreshData(t *testing.T) {
	first := BuildPlatformCatalog()
	first.AdminConnectionMethods[0].Surfaces[0] = "changed"
	first.NativeConnectionInventory.Profiles[0].Methods[0].Requirements[0] = "changed"
	second := BuildPlatformCatalog()
	require.NotEqual(t, first.AdminConnectionMethods[0].Surfaces, second.AdminConnectionMethods[0].Surfaces)
	require.NotEqual(t, first.NativeConnectionInventory.Profiles[0].Methods[0].Requirements, second.NativeConnectionInventory.Profiles[0].Methods[0].Requirements)
}
