package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) ReadGatewayNativeConnectionInfo(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string, native service.GatewayNativePublishedRoute) (service.GatewayAccountInfoStored, error) {
	var empty service.GatewayAccountInfoStored
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
	return readGatewayAccountInfoTx(ctx, tx, service.GatewayNativeConnectionInfoPhysical(physical))
}

var _ service.GatewayNativeConnectionInfoCache = (*accountRepository)(nil)
