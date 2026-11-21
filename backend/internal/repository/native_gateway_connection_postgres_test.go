//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type connectionLostStageACK struct {
	service.GatewayNativeConnectionRepository
	calls atomic.Int32
}

func (r *connectionLostStageACK) StageGatewayNativeConnection(ctx context.Context, in service.GatewayNativeConnectionEnrollment) (service.GatewayNativePublishedRoute, error) {
	r.calls.Add(1)
	out, err := r.GatewayNativeConnectionRepository.StageGatewayNativeConnection(ctx, in)
	if err == nil {
		return service.GatewayNativePublishedRoute{}, service.ErrGatewayNativeIdentity
	}
	return out, err
}

// Real migrated PostgreSQL proves the new journal/account encoding, one effect
// entry, lost publication ACK readback, and erasure with immutable receipts.
// Provider execution has its separate joined transport fixture; this test never
// replaces an upstream dispatch with a successful stub.
func TestGatewayNativeConnectionPostgresLifecycleAndLostACK(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_CONNECTION_TEST_DSN", "native_connection_test_")
	// The durable ciphertext must survive a database timestamp offset that
	// differs from the UTC preparation instant, including after restart.
	endpoint, err := url.Parse(dsn)
	require.NoError(t, err)
	query := endpoint.Query()
	query.Set("TimeZone", "Asia/Shanghai")
	endpoint.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, endpoint.String())
	rows, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("native-fixture", map[string][]byte{"native-fixture": bytes.Repeat([]byte{0x42}, 32)})
	require.NoError(t, err)
	method, err := service.NewGatewayNativeAPIKeyConnectionMethod("deepseek-responses-apikey-v1", "native-fixture-config",
		service.GatewayConnectionMount{BaseURL: service.DefaultDeepseekBaseURL, Model: "deepseek-v4-flash", QualificationRef: "native-fixture-qualified"}, nil)
	require.NoError(t, err)
	var completed atomic.Int32
	complete := method.Complete
	method.Complete = func(ctx context.Context, in service.GatewayNativeConnectionScope, raw json.RawMessage, fields map[string]string) (service.GatewayNativeConnectionCredential, error) {
		completed.Add(1)
		return complete(ctx, in, raw, fields)
	}
	var cleanupRoute *service.GatewayNativeRoute
	cleanup := func(ctx context.Context, in service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
		if ctx.Err() != nil || in.Scope.Owner != "native-owner" || in.CleanupRef != "native-cleanup" || in.CleanupToken != "native-cleanup-token" {
			return service.GatewayNativeOAuthCleanupAuthority{}, service.ErrGatewayNativeIdentity
		}
		return service.GatewayNativeOAuthCleanupAuthority{Owner: in.Scope.Owner, LeaseExpiresAt: time.Now().Add(10 * time.Second), Native: cleanupRoute}, nil
	}
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if ctx.Err() != nil || !service.GatewayConnectionScopeAuthorized(ctx, scope) || scope.Owner != "native-owner" {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	lost := &connectionLostStageACK{GatewayNativeConnectionRepository: rows}
	dependencies := service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: lost, Custody: custody,
		MaterialKey: bytes.Repeat([]byte{0x63}, 32), Methods: []service.GatewayNativeConnectionMethodAdapter{method}, Connect: owner, Refresh: owner, Cleanup: cleanup,
		Dispatchers: map[string]service.GatewayNativeConnectionDispatchAdapter{method.DispatchFamily: {
			Forward: func(context.Context, *gin.Context, *service.Account, service.GatewayNativeRoute, []byte) (*service.OpenAIForwardResult, error) {
				return nil, service.ErrGatewayNativeIdentity
			},
			ValidateRequest: func(*http.Request, *service.Account, service.GatewayNativePublishedRoute, string) bool { return false },
		}}}
	native, err := service.NewGatewayNativeConnections(dependencies)
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "native-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "native-owner")
	require.NoError(t, err)
	in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "native-consumer", Owner: "native-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose},
		Operation: "native-connect", Connection: method.Selection()}
	secret := "native-synthetic-api-key-only-" + uuid.NewString()
	input := map[string]string{"api_key": secret}
	result, err := native.Begin(ownerCtx, in, input)
	require.NoError(t, err)
	require.Equal(t, "completed", result.State, "a lost ACK reads the committed native birth")
	require.Equal(t, "staged", result.Outcome)
	require.Equal(t, &service.GatewayCredentialAttestation{CredentialKind: "api_key", CredentialPolicy: "provider_key", RefreshPolicy: "none"}, result.Credential)
	require.EqualValues(t, 1, completed.Load())
	require.EqualValues(t, 1, lost.calls.Load())
	descriptor, err := native.Describe(ownerCtx, in)
	require.NoError(t, err)
	require.NotNil(t, descriptor.Native)
	cleanupRoute = &descriptor.Native.GatewayNativeRoute
	require.Equal(t, in.Connection, descriptor.Native.Publication.Connection)
	retention, err := native.Custody(ownerCtx, in, *descriptor.Native)
	require.NoError(t, err)
	require.True(t, retention.Proof.Authenticated)
	require.Equal(t, "retained", retention.Retention.Credential)

	// A new runtime and changed/removed catalog recover the original selection
	// from the journal. Read, custody and cleanup never need a current method.
	dependencies.Repository = rows
	dependencies.Methods = nil
	restarted, err := service.NewGatewayNativeConnections(dependencies)
	require.NoError(t, err)
	recovered, err := restarted.Read(ownerCtx, in)
	require.NoError(t, err)
	require.Equal(t, result, recovered)
	_, err = restarted.Begin(ownerCtx, in, input)
	require.Error(t, err, "missing current configuration cannot start another effect")
	readDescriptor, err := restarted.Describe(ownerCtx, in)
	require.NoError(t, err)
	require.Equal(t, descriptor, readDescriptor)

	// Accepted same-operation input cannot be replaced, including after the
	// first result was lost. Exact replay is readback and performs no new effect.
	replay, err := native.Begin(ownerCtx, in, input)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	_, err = native.Begin(ownerCtx, in, map[string]string{"api_key": secret + "-different"})
	require.ErrorIs(t, err, service.ErrGatewayOAuthConflict)
	foreign := in
	foreign.Connection.ConfigurationGeneration = "native-changed-config"
	_, err = native.Read(ownerCtx, foreign)
	require.Error(t, err)
	foreign = in
	foreign.Scope.Owner = "another-owner"
	_, err = native.Read(ownerCtx, foreign)
	require.Error(t, err)
	require.EqualValues(t, 1, completed.Load())

	var durable string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_build_object('accounts',(SELECT jsonb_agg(to_jsonb(a)) FROM accounts a),'intents',(SELECT jsonb_agg(to_jsonb(i)) FROM gateway_oauth_connect_intents i))::text`).Scan(&durable))
	require.NotContains(t, durable, secret)
	require.NotContains(t, durable, "native-changed-config")
	encoded, err := json.Marshal([]any{result, descriptor, retention})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secret)
	_, err = db.ExecContext(ctx, `UPDATE accounts SET status='active',schedulable=true WHERE id=$1`, descriptor.Native.AccountID)
	require.Error(t, err, "new native births must remain inaccessible to the ordinary scheduler")
	_, err = db.ExecContext(ctx, `UPDATE gateway_oauth_connect_intents SET connection_binding=jsonb_set(connection_binding,'{configurationGeneration}','"retargeted"') WHERE operation_ref=$1`, in.Operation)
	require.Error(t, err)
	maintenance, err := restarted.Maintain(ownerCtx, in)
	require.NoError(t, err)
	require.Equal(t, "idle", maintenance.State, "API keys never acquire an invented refresh")
	removed, err := restarted.Cleanup(ownerCtx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "native-cleanup", CleanupToken: "native-cleanup-token"})
	require.NoError(t, err)
	require.True(t, removed.CredentialsErased)
	_, err = restarted.Custody(ownerCtx, in, *descriptor.Native)
	require.Error(t, err)
	readback, err := restarted.Read(ownerCtx, in)
	require.NoError(t, err)
	require.Equal(t, result, readback, "erasure preserves immutable operation provenance")
	require.EqualValues(t, 1, completed.Load())
}
