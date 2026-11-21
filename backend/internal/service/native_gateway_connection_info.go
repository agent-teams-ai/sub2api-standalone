package service

import (
	"context"
	"time"
)

type GatewayNativeConnectionInfoCache interface {
	ReadGatewayNativeConnectionInfo(context.Context, GatewayNativeCredentialScope, string, GatewayNativePublishedRoute) (GatewayAccountInfoStored, error)
}

// Account reads use the authenticated original publication even when its method
// is no longer mounted. Only the existing committed observations are projected;
// this path never refreshes credentials, fetches a provider or hydrates a cache.
func (s *GatewayNativeConnections) readAccountInfo(ctx context.Context, in GatewayAccountLimitsInput, now time.Time) GatewayAccountInfoResult {
	unavailable := GatewayAccountInfoResult{Info: GatewayAccountInfo{Status: "unavailable"}}
	binding, ok := s.accountReadBinding(ctx, in)
	if !ok {
		return unavailable
	}
	cache, ok := s.repository.(GatewayNativeConnectionInfoCache)
	if !ok {
		return unavailable
	}
	p := binding.physical
	stored, err := cache.ReadGatewayNativeConnectionInfo(ctx, p.Scope, p.Operation, p.Native)
	if err != nil || ctx.Err() != nil || stored.CredentialRevision != p.CredentialVersion ||
		stored.PrincipalVersion != GatewayAccountInfoPrincipal(GatewayNativeConnectionInfoPhysical(p)) {
		return unavailable
	}
	return projectGatewayAccountInfoResult(in.ProfileID, stored, p.Scope.Generation, now)
}
