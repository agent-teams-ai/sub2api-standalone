package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Generic native credentials use the existing quota snapshot table. Reading
// cannot collect provider data or create an observation: a missing row is unknown.
// The journal and retained physical lock prove the original selection before
// selecting a snapshot; a changed credential revision has a distinct principal.
func (r *accountRepository) ReadGatewayNativeConnectionLimits(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string, native service.GatewayNativePublishedRoute) (service.GatewayAccountLimitsStoredV2, error) {
	var empty service.GatewayAccountLimitsStoredV2
	if !service.GatewayConnectionScopeAuthorized(ctx, scope) || !service.GatewayConnectionSelectionAuthorized(ctx, &native.Publication.Connection) {
		return empty, service.ErrGatewayNativeIdentity
	}
	db, ok := nativeConnectionDatabase(r)
	if !ok {
		return empty, service.ErrGatewayNativeIdentity
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return empty, service.ErrGatewayNativeIdentity
	}
	defer func() { _ = tx.Rollback() }()
	physical, err := nativeConnectionPhysicalTx(ctx, tx, scope, operation, false)
	if err != nil || physical.Native.Publication != native.Publication || !service.SameGatewayNativeDescriptor(physical.Native.GatewayNativeRoute, native.GatewayNativeRoute) {
		return empty, service.ErrGatewayNativeIdentity
	}
	out := service.GatewayAccountLimitsStoredV2{PrincipalVersion: service.GatewayNativeConnectionObservationPrincipal(physical)}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,snapshot FROM gateway_account_limits_v2_snapshots
 WHERE account_id=$1 AND native_created_at=$2 AND generation=$3 AND principal_version=$4`, native.AccountID, native.CreatedAt, native.Generation, out.PrincipalVersion).Scan(&out.Revision, &raw)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil || len(raw) > 8192 || out.Revision < 1 || out.Revision > 9007199254740991 {
		return empty, service.ErrGatewayNativeIdentity
	}
	var snapshot service.GatewayAccountLimitsSnapshotV2
	if json.Unmarshal(raw, &snapshot) != nil || !service.ValidateGatewayAccountLimitsSnapshotV3(snapshot, native.Profile) {
		return empty, service.ErrGatewayNativeIdentity
	}
	out.Snapshot = &snapshot
	return out, nil
}

var _ service.GatewayNativeConnectionLimitsCache = (*accountRepository)(nil)
