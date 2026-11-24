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

// Reserve before a method can send credentials. Publication assigns the physical
// credential revision later, but cannot reorder observations by HTTP completion.
func (s *GatewayNativeConnections) reserveAccountInfoOrder(ctx context.Context, profile string) (int64, error) {
	definition, exists := LookupGatewayConnectionProfile(profile)
	if !exists || len(definition.MetadataSources) == 0 {
		return 0, nil
	}
	writer, ok := s.repository.(interface {
		ReserveGatewayAccountInfoOrder(context.Context) (int64, error)
	})
	if !ok {
		return 0, nil
	}
	return writer.ReserveGatewayAccountInfoOrder(ctx)
}
