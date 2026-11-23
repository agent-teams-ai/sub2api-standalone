//go:build integration || gatewaycustody

package repository

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type codexInfoCountHTTP struct {
	http.RoundTripper
	calls atomic.Int32
}

func (c *codexInfoCountHTTP) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.RoundTripper.RoundTrip(r)
}

// The actual token/JWKS and account-check transports, custody, connect FSM,
// refresh writer and snapshot repository all run against a separate migrated
// database; only the upstream account is a signed synthetic fixture.
func TestGatewayAccountInfoCodexPostgresHydrationAndRefresh(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_CODEX_INFO_TEST_DSN", "gateway_info_test_codex_")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := openOAuthDispatchFixtureDB(dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, requireOAuthDispatchEmptyObjects(ctx, db))
	// Upgrade the actual previous schema after it has stored a real observation.
	// The new closed source policy must not rewrite history or alter a checksum.
	baseline := fstest.MapFS{}
	migrationFiles, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	for _, name := range migrationFiles {
		if name == "265_gateway_account_info_source_policy.sql" {
			continue
		}
		data, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		baseline[name] = &fstest.MapFile{Data: data}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	var phase atomic.Int32
	var accountChecks, subscriptions atomic.Int32
	var issuedAccess atomic.Value
	providerID := uuid.NewString()
	subscriptionExpiry := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, transport, _ := oauthPGRefreshFixtureWithSubscription(t, func(w http.ResponseWriter, r *http.Request, bundleFor func(string) service.GatewayNativeOAuthBundle) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		bundle := bundleFor("codex-info-owner")
		issuedAccess.Store(bundle.AccessToken)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": bundle.AccessToken, "refresh_token": bundle.RefreshToken, "id_token": bundle.IDToken, "token_type": "Bearer", "expires_in": 3600, "scope": openai.RefreshScopes})
	}, func(w http.ResponseWriter, r *http.Request) {
		accountChecks.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Empty(t, r.URL.RawQuery)
		require.True(t, r.Header.Get("Authorization") == "Bearer "+issuedAccess.Load().(string), "account selection must use the issued credential")
		_, _ = fmt.Fprintf(w, `{"accounts":{"selected":{"account":{"account_id":%q,"account_structure":"personal","plan_type":"plus","name":"Review account"}}}}`, providerID)
	}, func(w http.ResponseWriter, r *http.Request) {
		subscriptions.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "account_id="+providerID, r.URL.RawQuery)
		require.True(t, r.Header.Get("Authorization") == "Bearer "+issuedAccess.Load().(string), "subscription lookup must use the selected account's issued credential")
		require.Equal(t, "https://chatgpt.com", r.Header.Get("Origin"))
		require.Equal(t, "https://chatgpt.com/", r.Header.Get("Referer"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		_ = json.NewEncoder(w).Encode(map[string]any{"account_id": providerID, "active_until": subscriptionExpiry})
	}, func(claims map[string]any) {
		claims["email"] = fmt.Sprintf("native%d@example.test", phase.Load())
		claims["name"] = "Signed account"
		claims["https://api.openai.com/auth"] = map[string]string{"chatgpt_plan_type": "plus"}
	})
	counted := &codexInfoCountHTTP{RoundTripper: transport}
	verifier := service.NewGatewayNativeOAuthVerifier(counted)
	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("info-codex", map[string][]byte{"info-codex": key})
	require.NoError(t, err)
	repo := NewAccountRepository(nil, db, nil)
	enrollment, err := service.NewGatewayNativeOAuthEnrollment(verifier, custody, repo.(service.GatewayNativeOAuthRepository), key)
	require.NoError(t, err)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	connect, err := service.NewGatewayNativeOAuthConnect(key, counted, journal, enrollment, repo.(service.GatewayNativeOAuthRepository))
	require.NoError(t, err)
	require.NoError(t, connect.SetEntryAuthorizer(func(context.Context, service.GatewayNativeCredentialScope, string) error { return nil }))
	owner, scope := oauthPGScope(t, "info-codex-consumer", "info-codex-owner", "info-codex-account")
	owner, stop := context.WithTimeout(owner, 55*time.Second)
	defer stop()
	const operation = "info-codex-import"
	connected, err := connect.ImportConnect(owner, scope, operation, "refresh_token", "info-codex-refresh")
	require.NoError(t, err)
	require.Equal(t, "completed", connected.State)
	// Seed a valid retained omission through the existing snapshot writer. F3
	// import alone does not qualify account-check facts or publish a snapshot.
	baselineRow, err := repo.(service.GatewayNativeOAuthDispatchRepository).ReadGatewayNativeOAuthDispatch(owner, scope, operation)
	require.NoError(t, err)
	_, baselinePhysical, releaseBaseline, err := repo.(service.GatewayNativeOAuthDispatchRepository).LockGatewayNativeOAuthDispatch(owner, scope, operation, baselineRow.AccountID)
	require.NoError(t, err)
	releaseBaseline()
	baselinePhysical.CredentialRevision = 1
	baselineTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, storeGatewayAccountInfoTx(ctx, baselineTx, baselinePhysical, service.GatewayAccountInfoObservation{
		Source: "verified_claims", CredentialRevision: 1, Fields: service.GatewayAccountInfoEmptyFields(service.GatewayCodexOAuthResponsesProfile),
	}, true))
	require.NoError(t, baselineTx.Commit())
	var legacySources, legacyChecksum string
	var legacyRevision int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sources::text,revision FROM gateway_account_info_snapshots`).Scan(&legacySources, &legacyRevision))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE filename='260_gateway_account_info.sql'`).Scan(&legacyChecksum))
	require.NoError(t, ApplyMigrations(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db), "the forward migration is replay-safe through the real migration runner")
	var upgradedSources, upgradedChecksum string
	var upgradedRevision int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sources::text,revision FROM gateway_account_info_snapshots`).Scan(&upgradedSources, &upgradedRevision))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE filename='260_gateway_account_info.sql'`).Scan(&upgradedChecksum))
	require.True(t, legacySources == upgradedSources, "existing cached observations survive the upgrade byte-for-byte")
	require.Equal(t, legacyRevision, upgradedRevision)
	require.Equal(t, legacyChecksum, upgradedChecksum)
	dispatch, err := service.NewGatewayNativeOAuthDispatch(repo.(service.GatewayNativeOAuthDispatchRepository), custody, verifier, counted)
	require.NoError(t, err)
	descriptor, err := dispatch.Descriptor(owner, scope, operation)
	require.NoError(t, err)
	require.NotNil(t, descriptor.Native)
	require.EqualValues(t, 1, accountChecks.Load())
	require.EqualValues(t, 1, subscriptions.Load(), "qualification may enrich the selected account's known plan")
	qualifiedCalls := counted.calls.Load()
	route := *descriptor.Native
	info := NewGatewayAccountInfoRepository(db)
	input := service.GatewayAccountLimitsInput{OwnerRef: scope.Owner, AccountRef: scope.Account, ProfileID: route.Profile}
	initial, err := info.ReadGatewayAccountInfo(owner, input, route)
	require.NoError(t, err)
	require.Equal(t, "native0@example.test", *initial.Sources["verified_claims"].Fields.Email.Value)
	require.Equal(t, "plus", initial.Sources["account_check"].Fields.Subscription.Value.Code)
	require.Nil(t, initial.Sources["account_check"].Fields.Subscription.Value.EndsAt)
	require.Len(t, initial.Sources, 5)
	for _, source := range []string{"credential", "verified_claims", "account_check", "subscription_status", "training_settings"} {
		require.Contains(t, initial.Sources, source)
	}
	training := initial.Sources["training_settings"]
	require.Positive(t, training.Order)
	require.Equal(t, initial.Sources["account_check"].Order, training.Order, "the same pre-HTTP reservation binds authenticated observations")
	require.EqualValues(t, 1, training.CredentialRevision)
	require.Equal(t, "known", training.Fields.TrainingUsage.Status)
	require.NotNil(t, training.Fields.TrainingUsage.Value)
	require.Equal(t, "disabled", *training.Fields.TrainingUsage.Value)
	require.Equal(t, "provider_api", training.Fields.TrainingUsage.Source)
	require.NotEmpty(t, training.Fields.TrainingUsage.ObservedAt)
	subscription := initial.Sources["subscription_status"].Fields.Subscription
	require.Equal(t, "known", subscription.Status)
	require.Equal(t, "provider_api", subscription.Source)
	require.NotNil(t, subscription.Value)
	require.Equal(t, "plus", subscription.Value.Code)
	require.Equal(t, &service.GatewayAccountInfoDate{Kind: "instant", Value: subscriptionExpiry}, subscription.Value.EndsAt)
	require.Equal(t, qualifiedCalls, counted.calls.Load(), "cached account-info reads perform no HTTP")
	// Exercise the database vocabulary against this real snapshot separately
	// from per-profile source admission, then roll back every probe.
	sourceProbe, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	for _, source := range []string{"training_settings", "subscription_status", "user_declaration"} {
		_, err = sourceProbe.ExecContext(ctx, `UPDATE gateway_account_info_snapshots SET sources=sources||jsonb_build_object($1::text,'{}'::jsonb) WHERE account_id=$2`, source, route.AccountID)
		require.NoError(t, err, "known source %s must fit the closed database vocabulary", source)
	}
	_, err = sourceProbe.ExecContext(ctx, `UPDATE gateway_account_info_snapshots SET sources=sources||'{"unrecognized_source":{}}'::jsonb WHERE account_id=$1`, route.AccountID)
	require.Error(t, err, "unknown metadata sources remain forbidden by PostgreSQL")
	require.NoError(t, sourceProbe.Rollback())
	originalObserved := initial.Sources["credential"].Fields.CredentialExpiresAt.ObservedAt
	require.NotEmpty(t, originalObserved)
	_, err = db.ExecContext(ctx, `DELETE FROM gateway_account_info_snapshots WHERE account_id=$1`, route.AccountID)
	require.NoError(t, err)
	refresh, err := service.NewGatewayNativeOAuthRefresh(repo.(service.GatewayNativeOAuthRefreshRepository), custody, verifier, counted)
	require.NoError(t, err)
	require.NoError(t, refresh.SetEntryAuthorizer(func(context.Context, service.GatewayNativeCredentialScope, string) error { return nil }))
	before := counted.calls.Load()
	// Fail the second legacy source write. Both sources must roll back so the
	// indexed initialized hint remains false and ordinary maintenance can retry.
	_, err = db.ExecContext(ctx, `CREATE FUNCTION gateway_info_hydration_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.sources ? 'credential' THEN RAISE EXCEPTION 'synthetic second-source failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER gateway_info_hydration_reject BEFORE INSERT OR UPDATE ON gateway_account_info_snapshots FOR EACH ROW EXECUTE FUNCTION gateway_info_hydration_reject()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS gateway_info_hydration_reject ON gateway_account_info_snapshots; DROP FUNCTION IF EXISTS gateway_info_hydration_reject()`)
	})
	_, err = refresh.Maintain(owner, scope, operation)
	require.Error(t, err)
	var partialSnapshots int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM gateway_account_info_snapshots WHERE account_id=$1`, route.AccountID).Scan(&partialSnapshots))
	require.Zero(t, partialSnapshots, "no source may commit independently of the initialized snapshot")
	require.Equal(t, before, counted.calls.Load(), "failed retained hydration performs no HTTP")
	_, err = db.ExecContext(ctx, `DROP TRIGGER gateway_info_hydration_reject ON gateway_account_info_snapshots; DROP FUNCTION gateway_info_hydration_reject()`)
	require.NoError(t, err)
	maintained, err := refresh.Maintain(owner, scope, operation)
	require.NoError(t, err)
	require.Equal(t, "idle", maintained.State)
	require.Equal(t, before, counted.calls.Load(), "retained hydration performs no HTTP")
	hydrated, err := info.ReadGatewayAccountInfo(owner, input, route)
	require.NoError(t, err)
	require.Len(t, hydrated.Sources, 2)
	require.EqualValues(t, 0, hydrated.Sources["verified_claims"].Order)
	require.EqualValues(t, 0, hydrated.Sources["credential"].Order)
	require.Equal(t, "native0@example.test", *hydrated.Sources["verified_claims"].Fields.Email.Value)
	require.Equal(t, originalObserved, hydrated.Sources["credential"].Fields.CredentialExpiresAt.ObservedAt)
	_, err = refresh.Maintain(owner, scope, operation)
	require.NoError(t, err)
	repeated, err := info.ReadGatewayAccountInfo(owner, input, route)
	require.NoError(t, err)
	require.Equal(t, hydrated.Revision, repeated.Revision)
	require.Equal(t, before, counted.calls.Load())
	_, physical, release, err := repo.(service.GatewayNativeOAuthDispatchRepository).LockGatewayNativeOAuthDispatch(owner, scope, operation, route.AccountID)
	require.NoError(t, err)
	release()
	physical.CredentialRevision = 1
	order, err := info.ReserveGatewayAccountInfoOrder(owner)
	require.NoError(t, err)
	omission := service.GatewayAccountInfoObservation{Source: "verified_claims", Order: order, CredentialRevision: 1, Fields: service.GatewayAccountInfoEmptyFields(route.Profile)}
	require.NoError(t, info.StoreGatewayAccountInfo(owner, physical, false, omission))
	require.NoError(t, refresh.HydrateAccountInfo(owner, scope, operation))
	tombstone, err := info.ReadGatewayAccountInfo(owner, input, route)
	require.NoError(t, err)
	require.Equal(t, "unknown", tombstone.Sources["verified_claims"].Fields.Email.Status)
	require.Equal(t, order, tombstone.Sources["verified_claims"].Order, "legacy source cannot resurrect an authoritative omission")
	phase.Store(1)
	intent := service.GatewayNativeOAuthRefreshIntent{Scope: scope, AccountID: route.AccountID, CreatedAt: route.CreatedAt, ExpectedVersion: 1, Operation: "info-codex-rotate-one", Intent: "info-codex-rotation-one", ConnectOperation: operation}
	out, err := refresh.Refresh(owner, intent)
	require.NoError(t, err)
	require.Equal(t, "completed", out.State)
	require.EqualValues(t, 2, out.Version)
	fresh, err := info.ReadGatewayAccountInfo(owner, input, route)
	require.NoError(t, err)
	require.EqualValues(t, 2, fresh.CredentialRevision)
	require.Equal(t, "native1@example.test", *fresh.Sources["verified_claims"].Fields.Email.Value)
	require.EqualValues(t, 2, fresh.Sources["credential"].CredentialRevision)
	require.Equal(t, initial.PrincipalVersion, fresh.PrincipalVersion)
	require.Error(t, info.StoreGatewayAccountInfo(owner, physical, false, omission), "old credential observation loses current revision CAS")
	// Fail only the second metadata source after the first source has written.
	// The whole optional batch rolls back while credentials and receipt commit.
	_, err = db.ExecContext(ctx, `CREATE FUNCTION gateway_info_test_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.sources->'credential'->>'credentialRevision')::bigint=3 THEN RAISE EXCEPTION 'synthetic second-source failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER gateway_info_test_reject BEFORE INSERT OR UPDATE ON gateway_account_info_snapshots FOR EACH ROW EXECUTE FUNCTION gateway_info_test_reject()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS gateway_info_test_reject ON gateway_account_info_snapshots; DROP FUNCTION IF EXISTS gateway_info_test_reject()`)
	})
	phase.Store(2)
	intent.ExpectedVersion, intent.Operation, intent.Intent = 2, "info-codex-rotate-two", "info-codex-rotation-two"
	out, err = refresh.Refresh(owner, intent)
	require.NoError(t, err)
	require.Equal(t, "completed", out.State)
	require.EqualValues(t, 3, out.Version)
	var credentialVersion, resultVersion int64
	var attemptState string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT gateway_oauth_credential_version(credentials) FROM accounts WHERE id=$1`, route.AccountID).Scan(&credentialVersion))
	require.EqualValues(t, 3, credentialVersion)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT state,result_version FROM gateway_oauth_refresh_attempts WHERE consumer=$1 AND operation_ref=$2`, scope.Consumer, intent.Operation).Scan(&attemptState, &resultVersion))
	require.Equal(t, "completed", attemptState)
	require.EqualValues(t, 3, resultVersion)
	unchanged, err := info.ReadGatewayAccountInfo(owner, input, route)
	require.NoError(t, err)
	require.EqualValues(t, 3, unchanged.CredentialRevision)
	require.Equal(t, fresh.Revision, unchanged.Revision)
	require.Equal(t, fresh.Sources, unchanged.Sources, "no first-source write may escape the failed metadata batch")
	fields, valid := service.GatewayAccountInfoProject(route.Profile, unchanged, time.Now())
	require.True(t, valid)
	require.Equal(t, "unknown", fields.CredentialExpiresAt.Status, "old expiry cannot describe the rotated credential")
	require.Equal(t, "native1@example.test", *fields.Email.Value)
	require.Equal(t, fresh.Sources["verified_claims"].Fields.Email.ObservedAt, fields.Email.ObservedAt)
	callsAfterRotation := counted.calls.Load()
	replayed, err := refresh.Refresh(owner, intent)
	require.NoError(t, err)
	require.Equal(t, out, replayed)
	require.Equal(t, callsAfterRotation, counted.calls.Load(), "completed refresh replay must not exchange again")
	stale := intent
	stale.Operation, stale.Intent = "info-codex-rotate-stale", "info-codex-rotation-stale"
	_, err = refresh.Refresh(owner, stale)
	require.Error(t, err, "optional metadata isolation cannot relax the credential version CAS")
	require.Equal(t, callsAfterRotation, counted.calls.Load())
	// A newer observation order must not relabel an old credential's expiry.
	// Remove the synthetic SQL failure so only the revision guard can reject it.
	_, err = db.ExecContext(ctx, `DROP TRIGGER gateway_info_test_reject ON gateway_account_info_snapshots; DROP FUNCTION gateway_info_test_reject()`)
	require.NoError(t, err)
	staleObservation := fresh.Sources["credential"]
	staleObservation.Order, err = info.ReserveGatewayAccountInfoOrder(owner)
	require.NoError(t, err)
	guardTx, current, err := info.physicalTx(owner, scope, route, true)
	require.NoError(t, err)
	defer func() { _ = guardTx.Rollback() }()
	require.EqualValues(t, 3, current.CredentialRevision)
	require.EqualValues(t, 2, staleObservation.CredentialRevision)
	require.NoError(t, storeGatewayRefreshAccountInfoTx(owner, guardTx, current, []service.GatewayAccountInfoObservation{staleObservation}))
	require.NoError(t, guardTx.Commit())
	guarded, err := info.ReadGatewayAccountInfo(owner, input, route)
	require.NoError(t, err)
	require.Equal(t, unchanged.Revision, guarded.Revision)
	require.Equal(t, unchanged.Sources, guarded.Sources, "stale expiry must not become a current-credential fact")
	require.EqualValues(t, 1, accountChecks.Load(), "maintenance never repeats account qualification")
	require.EqualValues(t, 1, subscriptions.Load(), "cached reads and retained hydration never fetch subscription metadata")

	t.Run("stored v1 rows preserve evidence and current authority", func(t *testing.T) {
		// The actual writer above created this exact physical principal and the
		// real refresh journal advanced it to credential revision 3. Install an
		// independent old wire fixture only in the cached observation column;
		// credentials, identity, birth and snapshot revision remain untouched.
		const oldSources = `{
		 "verified_claims":{"source":"verified_claims","order":17,"credentialRevision":2,"fields":{
		  "email":{"status":"known","value":"owner@example.invalid","source":"verified_claim","observedAt":"2026-11-19T10:00:00Z","freshness":"stale"},
		  "accountLabel":{"status":"unknown"},"subscription":{"status":"unknown"},"credentialExpiresAt":{"status":"unknown"},"privacyMode":{"status":"unsupported"}}},
		 "credential":{"source":"credential","order":18,"credentialRevision":2,"fields":{
		  "email":{"status":"unknown"},"accountLabel":{"status":"unknown"},"subscription":{"status":"unknown"},
		  "credentialExpiresAt":{"status":"known","value":"2026-12-19T10:00:00Z","source":"provider_api","observedAt":"2026-11-19T10:00:00Z","freshness":"stale"},"privacyMode":{"status":"unsupported"}}}
		}`
		installed, err := db.ExecContext(owner, `UPDATE gateway_account_info_snapshots SET sources=$6::jsonb
 WHERE account_id=$1 AND native_created_at=$2 AND generation=$3 AND principal_version=$4 AND revision=$5`, route.AccountID, route.CreatedAt, route.Generation, guarded.PrincipalVersion, guarded.Revision, oldSources)
		require.NoError(t, err)
		rows, err := installed.RowsAffected()
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
		readRow := func() string {
			var row string
			require.NoError(t, db.QueryRowContext(owner, `SELECT to_jsonb(s)::text FROM gateway_account_info_snapshots s WHERE account_id=$1`, route.AccountID).Scan(&row))
			return row
		}
		originalRow, httpBefore := readRow(), counted.calls.Load()
		stored, err := info.ReadGatewayAccountInfo(owner, input, route)
		require.NoError(t, err)
		require.Equal(t, guarded.PrincipalVersion, stored.PrincipalVersion)
		require.Equal(t, guarded.Revision, stored.Revision)
		require.EqualValues(t, 3, stored.CredentialRevision)
		require.Len(t, stored.Sources, 2)
		claims, credential := stored.Sources["verified_claims"], stored.Sources["credential"]
		require.Equal(t, "verified_claims", claims.Source)
		require.EqualValues(t, 17, claims.Order)
		require.EqualValues(t, 18, credential.Order)
		require.EqualValues(t, 2, claims.CredentialRevision)
		require.EqualValues(t, 2, credential.CredentialRevision)
		require.Equal(t, "2026-11-19T10:00:00Z", claims.Fields.Email.ObservedAt)
		require.Equal(t, "verified_claim", claims.Fields.Email.Source)
		require.Equal(t, "stale", claims.Fields.Email.Freshness)
		require.Equal(t, "2026-11-19T10:00:00Z", credential.Fields.CredentialExpiresAt.ObservedAt)
		require.Equal(t, "provider_api", credential.Fields.CredentialExpiresAt.Source)
		require.Equal(t, "unsupported", claims.Fields.TrainingUsage.Status)

		selectedRoute := route
		reader, err := service.NewGatewayAccountInfoReader(info, func(context.Context, service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
			return selectedRoute, nil
		})
		require.NoError(t, err)
		public := reader.Read(owner, input)
		require.Equal(t, "available", public.Info.Status)
		require.Equal(t, fmt.Sprint(guarded.Revision), public.Info.InfoRevision)
		require.Equal(t, route.Generation, public.PhysicalGeneration)
		require.Equal(t, guarded.PrincipalVersion, public.PrincipalVersion)
		require.Equal(t, claims.Fields.Email, public.Info.Fields.Email)
		require.Equal(t, "unknown", public.Info.Fields.CredentialExpiresAt.Status, "revision 2 expiry cannot describe the actually rotated revision 3 credential")
		require.Equal(t, "unknown", public.Info.Fields.TrainingUsage.Status, "old privacy support is not a provider training-settings observation")

		foreignContext, err := service.WithGatewayNativeConsumer(ctx, scope.Consumer)
		require.NoError(t, err)
		foreignContext, err = service.WithGatewayNativeOAuthOwner(foreignContext, "different-owner")
		require.NoError(t, err)
		foreignInput := input
		foreignInput.OwnerRef = "different-owner"
		_, err = info.ReadGatewayAccountInfo(foreignContext, foreignInput, route)
		require.Error(t, err, "legacy fields do not bypass exact owner custody")
		require.Equal(t, "unavailable", reader.Read(foreignContext, foreignInput).Info.Status)
		selectedRoute.CreatedAt = route.CreatedAt.Add(time.Microsecond)
		_, err = info.ReadGatewayAccountInfo(owner, input, selectedRoute)
		require.Error(t, err, "legacy fields do not bypass physical birth")
		require.Equal(t, "unavailable", reader.Read(owner, input).Info.Status)
		selectedRoute = route

		late := claims
		late.Order, late.CredentialRevision = 16, current.CredentialRevision
		require.NoError(t, info.StoreGatewayAccountInfo(owner, current, false, late), "a lower source order remains an ignored late observation")
		stalePhysical := current
		stalePhysical.CredentialRevision = credential.CredentialRevision
		credential.Order = 19
		require.Error(t, info.StoreGatewayAccountInfo(owner, stalePhysical, false, credential), "a higher source order cannot bypass current credential revision")
		require.Equal(t, originalRow, readRow(), "cached reads and rejected/obsolete writes preserve the whole old row, including its original encoding")
		require.Equal(t, httpBefore, counted.calls.Load(), "legacy decoding, public reads and fences never re-observe provider facts")
	})
}
