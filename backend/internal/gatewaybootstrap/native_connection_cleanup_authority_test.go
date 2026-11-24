//go:build linux

package gatewaybootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Critical authorization regression: logical HTTP admission is not a physical
// erase proof, and adding generic cleanup never broadens the legacy tuple gate.
func TestNativeConnectionCleanupAuthorityStoredTuple(t *testing.T) {
	generation := "11111111-1111-4111-8111-111111111111"
	original := service.GatewayNativeRoute{AccountID: 42, Generation: generation,
		CreatedAt: time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC),
		Profile:   "deepseek-responses-apikey-v1", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"}
	route, owner, lease := original, "owner", time.Now().Add(time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/private/native/v1/oauth-cleanup-authority", r.URL.Path)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, map[string]string{"consumerId": "fixture-consumer", "operationId": "op", "accountRef": "account", "generation": generation, "cleanupRef": "cleanup", "cleanupToken": "token", "action": "erase"}, body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ownerRef": owner, "leaseExpiresAt": lease.UTC().Format(time.RFC3339Nano), "native": route})
	}))
	defer server.Close()
	a, err := newAuthority(AuthorityConfig{Origin: server.URL, Credential: fixtureConfig().Authority.Credential}, nil)
	require.NoError(t, err)
	defer a.client.CloseIdleConnections()
	in := service.GatewayNativeOAuthCleanupRequest{Scope: service.GatewayNativeCredentialScope{Consumer: "fixture-consumer", Owner: "owner", Account: "account", Generation: generation, Purpose: service.GatewayOAuthBundlePurpose}, Operation: "op", Action: "erase", CleanupRef: "cleanup", CleanupToken: "token"}
	admitted, err := a.AuthorizeNativeOAuthCleanupOwner(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "owner", admitted)
	_, err = a.AuthorizeNativeOAuthCleanup(context.Background(), in)
	require.Error(t, err, "owner-only admission never broadens the legacy erase proof")
	in.Connection = &service.GatewayConnectionSelection{ProfileID: original.Profile, MethodID: "api_key", MethodRevision: 1, ConfigurationGeneration: "retired-configuration"}
	in.NativeMount = &service.GatewayConnectionMount{BaseURL: original.BaseURL, Model: original.Model, QualificationRef: "removed-mount"}
	grant, err := a.AuthorizeNativeOAuthCleanup(context.Background(), in)
	require.NoError(t, err, "saved tuple remains valid without a current mount")
	require.NotNil(t, grant.Native)
	require.True(t, service.SameGatewayNativeDescriptor(original, *grant.Native))
	for name, change := range map[string]func(*service.GatewayNativeRoute){
		"profile":         func(r *service.GatewayNativeRoute) { r.Profile = service.GatewayGrokOAuthResponsesProfile },
		"base":            func(r *service.GatewayNativeRoute) { r.BaseURL = "https://other.invalid" },
		"model":           func(r *service.GatewayNativeRoute) { r.Model = "other-model" },
		"generation":      func(r *service.GatewayNativeRoute) { r.Generation = "22222222-2222-4222-8222-222222222222" },
		"birth_precision": func(r *service.GatewayNativeRoute) { r.CreatedAt = r.CreatedAt.Add(time.Nanosecond) },
		"physical_id":     func(r *service.GatewayNativeRoute) { r.AccountID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			route = original
			change(&route)
			_, err := a.AuthorizeNativeOAuthCleanup(context.Background(), in)
			require.Error(t, err)
		})
	}
	route = original
	owner = "foreign-owner"
	_, err = a.AuthorizeNativeOAuthCleanupOwner(context.Background(), in)
	require.Error(t, err)
	owner, lease = "owner", time.Now().Add(-time.Second)
	_, err = a.AuthorizeNativeOAuthCleanupOwner(context.Background(), in)
	require.Error(t, err)
	lease = time.Now().Add(time.Minute)
	in.NativeMount = nil
	_, err = a.AuthorizeNativeOAuthCleanup(context.Background(), in)
	require.Error(t, err, "a partial locally pinned tuple is rejected")
}
