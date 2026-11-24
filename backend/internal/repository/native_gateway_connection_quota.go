package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Enumeration carries no credential or provider fact. Maintenance must acquire
// the original journal/publication/physical lock before opening any candidate.
func (r *accountRepository) ListGatewayNativeConnectionQuotaAccounts(ctx context.Context) ([]service.GatewayNativeConnectionPhysical, error) {
	consumer, err := service.GatewayNativeConsumer(ctx)
	db, ok := r.sql.(interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	})
	if err != nil || !ok {
		return nil, service.ErrGatewayNativeIdentity
	}
	rows, err := db.QueryContext(ctx, `SELECT c.operation_ref,c.owner_ref,c.account_ref,c.generation,c.purpose,
 c.native_publication::text,c.native_mount::text,a.id,a.created_at,gateway_native_connection_credential_version(a.credentials)
 FROM gateway_oauth_connect_intents c JOIN accounts a ON a.id=c.outcome_account_id
 WHERE c.consumer=$1 AND c.client_id='native-connection-v1' AND c.state='completed' AND NOT c.recovery_denied
 AND a.deleted_at IS NULL AND a.status='disabled' AND NOT a.schedulable
 AND a.extra->'gateway_native_connection_v1'='true'::jsonb
 AND a.extra->>'gateway_connection_operation_v1'=c.operation_ref
 AND a.credentials->'connection_publication'=c.native_publication
 ORDER BY a.id LIMIT 10000`, consumer)
	if err != nil {
		return nil, service.ErrGatewayNativeIdentity
	}
	defer func() { _ = rows.Close() }()
	out := []service.GatewayNativeConnectionPhysical{}
	for rows.Next() {
		var p service.GatewayNativeConnectionPhysical
		var rawPublication, rawMount []byte
		if rows.Scan(&p.Operation, &p.Scope.Owner, &p.Scope.Account, &p.Scope.Generation, &p.Scope.Purpose,
			&rawPublication, &rawMount, &p.Native.AccountID, &p.Native.CreatedAt, &p.CredentialVersion) != nil {
			return nil, service.ErrGatewayNativeIdentity
		}
		publication, err := service.DecodeGatewayConnectionPublication(rawPublication)
		var mount service.GatewayConnectionMount
		if err != nil || len(rawMount) > 4096 || json.Unmarshal(rawMount, &mount) != nil || !service.GatewayConnectionMountValid(mount) || p.CredentialVersion < 1 {
			return nil, service.ErrGatewayNativeIdentity
		}
		p.Scope.Consumer = consumer
		p.Native.Publication = publication
		p.Native.Generation, p.Native.Profile = p.Scope.Generation, publication.Connection.ProfileID
		p.Native.BaseURL, p.Native.Model = mount.BaseURL, mount.Model
		if !service.GatewayNativeOAuthScopeValid(p.Scope) || !service.GatewayNativeCredentialRefValid(p.Operation) {
			return nil, service.ErrGatewayNativeIdentity
		}
		out = append(out, p)
	}
	if rows.Err() != nil || ctx.Err() != nil {
		return nil, service.ErrGatewayNativeIdentity
	}
	return out, nil
}

// The same snapshot transaction leaf serves old and generic native accounts.
// Only the physical proof differs. A new credential revision cannot inherit old
// windows or publish a late observation made with the previous sealed bundle.
func (r *accountRepository) StoreGatewayNativeConnectionQuota(ctx context.Context, expected service.GatewayNativeConnectionPhysical, observation service.GatewayProviderQuotaObservation, order int64) error {
	if expected.Account == nil || !service.GatewayConnectionScopeAuthorized(ctx, expected.Scope) ||
		!service.GatewayConnectionSelectionAuthorized(ctx, &expected.Native.Publication.Connection) {
		return service.ErrGatewayNativeIdentity
	}
	db, ok := nativeConnectionDatabase(r)
	if !ok {
		return service.ErrGatewayNativeIdentity
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return service.ErrGatewayNativeIdentity
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	if tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND created_at=$2 AND deleted_at IS NULL FOR UPDATE`,
		expected.Native.AccountID, expected.Native.CreatedAt).Scan(&id) != nil {
		return service.ErrGatewayNativeIdentity
	}
	current, err := nativeConnectionPhysicalTx(ctx, tx, expected.Scope, expected.Operation, false)
	if err != nil || current.Native.Publication != expected.Native.Publication ||
		!service.SameGatewayNativeDescriptor(current.Native.GatewayNativeRoute, expected.Native.GatewayNativeRoute) ||
		current.CredentialVersion != expected.CredentialVersion || !reflect.DeepEqual(current.Account.Credentials, expected.Account.Credentials) ||
		!reflect.DeepEqual(current.Account.Extra, expected.Account.Extra) {
		return service.ErrGatewayNativeIdentity
	}
	var safe bool
	if tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM gateway_oauth_refresh_attempts WHERE account_id=$1
 AND (state IN ('entered','unknown') OR state='prepared' AND deadline<=clock_timestamp()))`, id).Scan(&safe) != nil || !safe {
		return service.ErrGatewayNativeIdentity
	}
	physical := service.GatewayNativeConnectionInfoPhysical(current)
	var info *service.GatewayAccountInfoObservation
	if observation.Info != nil {
		if order < 1 || !service.GatewayAccountInfoPlanQuotaProfile(current.Native.Profile) {
			return service.ErrGatewayNativeIdentity
		}
		info = &service.GatewayAccountInfoObservation{Source: "plan_quota", Order: order, CredentialRevision: current.CredentialVersion, Fields: *observation.Info}
	}
	principal := service.GatewayNativeConnectionObservationPrincipal(current)
	// The exact canonical row is locked. Replacing its credential starts a new
	// independent cache principal, including a fresh monotonic revision space.
	if _, err = tx.ExecContext(ctx, `DELETE FROM gateway_account_limits_v2_snapshots WHERE account_id=$1 AND
 (native_created_at<>$2 OR generation<>$3 OR principal_version<>$4)`, id, current.Native.CreatedAt, current.Native.Generation, principal); err != nil {
		return service.ErrGatewayNativeIdentity
	}
	if err = storeGatewayAccountQuotaTx(ctx, tx, physical, principal, observation.Limits, observation.ObservedAt, info, service.ValidateGatewayAccountLimitsSnapshotV3); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return service.ErrGatewayNativeIdentity
	}
	return nil
}

var _ service.GatewayNativeConnectionQuotaRepository = (*accountRepository)(nil)
