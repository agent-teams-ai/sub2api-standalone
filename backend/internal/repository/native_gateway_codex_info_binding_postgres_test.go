//go:build integration || gatewaycustody

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Qualification writes a connection-bound principal. Account-info reads and
// later snapshot writes must recover that same immutable binding from SQL,
// including when their HTTP caller supplies only the authorized account route.
func TestGatewayAccountInfoCodexPostgresConnectionBinding(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_CODEX_BINDING_INFO_TEST_DSN", "gateway_info_test_codex_binding_")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := openOAuthDispatchFixtureDB(dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, requireOAuthDispatchEmptyObjects(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db))

	for _, name := range []string{"bound_browser", "legacy_browser"} {
		t.Run(name, func(t *testing.T) {
			providerID := uuid.NewString()
			_, transport, _ := oauthPGRefreshFixture(t, func(w http.ResponseWriter, r *http.Request, bundleFor func(string) service.GatewayNativeOAuthBundle) {
				require.NoError(t, r.ParseForm())
				require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
				bundle := bundleFor(name)
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": bundle.AccessToken, "refresh_token": bundle.RefreshToken, "id_token": bundle.IDToken, "token_type": "Bearer", "expires_in": 3600})
			}, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{"selected": map[string]any{"account": map[string]any{"account_id": providerID, "account_structure": "personal"}}}})
			}, func(claims map[string]any) { claims["email"] = "verified-codex@example.test" })
			counted := &codexInfoCountHTTP{RoundTripper: transport}
			verifier := service.NewGatewayNativeOAuthVerifier(counted)
			key := custodyPGBytes(t, 32)
			custody, err := service.NewGatewayNativeCredentialCustody("binding-info", map[string][]byte{"binding-info": key})
			require.NoError(t, err)
			repo := NewAccountRepository(nil, db, nil)
			enrollment, err := service.NewGatewayNativeOAuthEnrollment(verifier, custody, repo.(service.GatewayNativeOAuthRepository), key)
			require.NoError(t, err)
			journal, err := NewGatewayNativeOAuthConnectRepository(db)
			require.NoError(t, err)
			connect, err := service.NewGatewayNativeOAuthConnect(key, counted, journal, enrollment, repo.(service.GatewayNativeOAuthRepository))
			require.NoError(t, err)
			require.NoError(t, connect.SetEntryAuthorizer(func(context.Context, service.GatewayNativeCredentialScope, string) error { return nil }))
			owner, scope := oauthPGScope(t, "binding-info-consumer", "binding-info-owner", "binding-info-"+name)
			owner, stop := context.WithTimeout(owner, 30*time.Second)
			defer stop()
			selected := owner
			var binding *service.GatewayConnectionSelection
			if name == "bound_browser" {
				binding = &service.GatewayConnectionSelection{ProfileID: service.GatewayCodexOAuthResponsesProfile, MethodID: "browser", MethodRevision: 1, ConfigurationGeneration: "binding-info-config"}
				selected, err = service.WithGatewayConnectionSelection(owner, *binding)
				require.NoError(t, err)
			}
			operation := "binding-info-" + name
			begin, err := connect.BeginConnectMethod(selected, scope, operation, "browser")
			require.NoError(t, err)
			authorize, err := url.Parse(begin.AuthorizeURL)
			require.NoError(t, err)
			completed, err := connect.CompleteOwnerCallback(selected, scope, operation, authorize.Query().Get("state"), "binding-info-code")
			require.NoError(t, err)
			require.Equal(t, "completed", completed.State)
			dispatchRepository := repo.(service.GatewayNativeOAuthDispatchRepository)
			dispatch, err := service.NewGatewayNativeOAuthDispatch(dispatchRepository, custody, verifier, counted)
			require.NoError(t, err)
			descriptor, err := dispatch.Descriptor(selected, scope, operation)
			require.NoError(t, err)
			require.NotNil(t, descriptor.Native)
			route := *descriptor.Native
			_, physical, release, err := dispatchRepository.LockGatewayNativeOAuthDispatch(selected, scope, operation, route.AccountID)
			require.NoError(t, err)
			release()
			physical.CredentialRevision = 1
			require.Equal(t, binding, physical.Connection)
			principal := service.GatewayAccountInfoPrincipal(physical)
			readSnapshot := func() string {
				var row string
				require.NoError(t, db.QueryRowContext(owner, `SELECT to_jsonb(s)::text FROM gateway_account_info_snapshots s WHERE account_id=$1`, route.AccountID).Scan(&row))
				return row
			}
			before, calls := readSnapshot(), counted.calls.Load()
			t.Run("refresh_resolution", func(t *testing.T) {
				current, err := repo.(service.GatewayNativeOAuthRefreshResolver).ResolveGatewayNativeOAuthRefresh(selected, scope, operation)
				require.NoError(t, err)
				require.True(t, current.Qualified)
				require.True(t, current.AccountInfoInitialized, "maintenance must recognize the snapshot already committed for this binding")
			})
			info := NewGatewayAccountInfoRepository(db)
			input := service.GatewayAccountLimitsInput{OwnerRef: scope.Owner, AccountRef: scope.Account, ProfileID: route.Profile}
			reader, err := service.NewGatewayAccountInfoReader(info, func(context.Context, service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
				return route, nil
			})
			require.NoError(t, err)
			public := reader.Read(owner, input)
			require.Equal(t, "available", public.Info.Status)
			require.NotNil(t, public.Info.Fields)
			require.Equal(t, "known", public.Info.Fields.Email.Status, "the qualified browser identity must survive the account-only metadata read")
			require.Equal(t, "verified-codex@example.test", *public.Info.Fields.Email.Value)
			require.Equal(t, "verified_claim", public.Info.Fields.Email.Source)
			require.Equal(t, "current", public.Info.Fields.Email.Freshness)
			require.Equal(t, principal, public.PrincipalVersion)
			require.Equal(t, public, reader.Read(owner, input))
			require.Equal(t, public.Info.Fields.Email, service.ProjectGatewayAccountInfoV1(public).Info.Fields.Email)
			require.Equal(t, before, readSnapshot(), "metadata reads never rewrite the committed snapshot")
			require.Equal(t, calls, counted.calls.Load(), "metadata reads never fetch provider identity")

			stored, err := info.ReadGatewayAccountInfo(owner, input, route)
			require.NoError(t, err)
			observation := stored.Sources["verified_claims"]
			observation.Order, err = info.ReserveGatewayAccountInfoOrder(owner)
			require.NoError(t, err)
			require.NoError(t, info.StoreGatewayAccountInfo(owner, physical, false, observation), "snapshot CAS must recover the same connection-bound principal")
			written, err := info.ReadGatewayAccountInfo(owner, input, route)
			require.NoError(t, err)
			require.Equal(t, stored.Revision+1, written.Revision)
			require.Equal(t, principal, written.PrincipalVersion)
			wrongBinding := physical
			if binding != nil {
				changed := *binding
				changed.ConfigurationGeneration += "-other"
				wrongBinding.Connection = &changed
			} else {
				wrongBinding.Connection = &service.GatewayConnectionSelection{ProfileID: route.Profile, MethodID: "browser", MethodRevision: 1, ConfigurationGeneration: "unexpected-binding"}
			}
			require.Error(t, info.StoreGatewayAccountInfo(owner, wrongBinding, false, observation), "another method binding cannot overwrite the snapshot")
			wrongRevision := physical
			wrongRevision.CredentialRevision++
			stale := observation
			stale.CredentialRevision = wrongRevision.CredentialRevision
			require.Error(t, info.StoreGatewayAccountInfo(owner, wrongRevision, false, stale), "binding recovery must retain credential revision CAS")
			foreign := input
			foreign.OwnerRef = "different-owner"
			require.Equal(t, "unavailable", reader.Read(owner, foreign).Info.Status)
			wrongBirth := route
			wrongBirth.CreatedAt = route.CreatedAt.Add(time.Microsecond)
			_, err = info.ReadGatewayAccountInfo(owner, input, wrongBirth)
			require.Error(t, err)
			after, err := info.ReadGatewayAccountInfo(owner, input, route)
			require.NoError(t, err)
			require.Equal(t, written, after, "rejected writes and foreign reads preserve the snapshot")
			require.Equal(t, calls, counted.calls.Load())
		})
	}
}
