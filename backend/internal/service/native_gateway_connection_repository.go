package service

import "context"

type GatewayNativeConnectionEnrollment struct {
	Intent       GatewayNativeOAuthConnectIntent
	Publication  GatewayConnectionPublication
	Envelope     string
	Observations []GatewayAccountInfoObservation
}

type GatewayNativeConnectionRepository interface {
	GatewayNativeConnectionPhysicalRepository
	GatewayNativeOAuthRefreshRepository
	StageGatewayNativeConnection(context.Context, GatewayNativeConnectionEnrollment) (GatewayNativePublishedRoute, error)
	ResolveGatewayNativeConnectionRefresh(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeConnectionRefreshResolution, error)
	CleanupGatewayNativeOAuth(context.Context, GatewayNativeOAuthCleanupRequest, GatewayNativeOAuthCleanupAuthorizer) (GatewayNativeOAuthCleanupResult, error)
}

type GatewayNativeConnectionRefreshResolution struct {
	Physical GatewayNativeConnectionPhysical
	Attempt  *GatewayNativeOAuthRefreshIntent
	State    string
}

// This principal is local custody identity only. It does not turn submitted
// email/account labels into verified provider identity or a merge authority.
func GatewayNativeConnectionInfoPhysical(p GatewayNativeConnectionPhysical) GatewayNativeOAuthPhysical {
	connection := p.Native.Publication.Connection
	return GatewayNativeOAuthPhysical{
		Scope: p.Scope, Operation: p.Operation, Route: p.Native.GatewayNativeRoute,
		Connection: &connection, Issuer: "native:" + p.Native.Publication.AuthFamily,
		Subject: p.Scope.Account, CredentialRevision: p.CredentialVersion,
	}
}
