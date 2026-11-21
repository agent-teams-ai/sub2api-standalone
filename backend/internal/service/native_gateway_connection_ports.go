package service

import "context"

// GatewayNativeConnectionScope contains only immutable authority selectors.
// InputFields are passed separately and never become a stored/readable scope.
type GatewayNativeConnectionScope struct {
	Scope      GatewayNativeCredentialScope
	Operation  string
	Connection GatewayConnectionSelection
}

func (in GatewayNativeConnectionScope) Authorize(ctx context.Context, purpose, profile string) (context.Context, error) {
	if in.Scope.Purpose != purpose || in.Connection.ProfileID != profile ||
		!GatewayConnectionScopeAuthorized(ctx, in.Scope) || !GatewayNativeCredentialRefValid(in.Operation) ||
		!GatewayConnectionSelectionValid(in.Connection) {
		return nil, ErrGatewayNativeIdentity
	}
	return WithGatewayConnectionSelection(ctx, in.Connection)
}

type GatewayNativeConnectionResult struct {
	GatewayNativeOAuthConnectResult
	Credential *GatewayCredentialAttestation `json:"credential,omitempty"`
}

type GatewayNativePublishedRoute struct {
	GatewayNativeRoute
	Publication GatewayConnectionPublication `json:"publication"`
}

type GatewayNativeConnectionDescriptorResult struct {
	Connection    GatewayConnectionSelection   `json:"connection"`
	Operation     string                       `json:"operation"`
	Account       string                       `json:"account_ref"`
	State         string                       `json:"state"`
	Qualification string                       `json:"qualification"`
	Native        *GatewayNativePublishedRoute `json:"native,omitempty"`
}

type GatewayNativeConnectionMaintenanceResult struct {
	GatewayNativeOAuthMaintenanceOutcome
	Connection GatewayConnectionSelection `json:"connection"`
}

type GatewayNativeConnectionCleanupResult struct {
	Connection        GatewayConnectionSelection   `json:"connection"`
	Operation         string                       `json:"operation"`
	Account           string                       `json:"account_ref"`
	CredentialsErased bool                         `json:"credentials_erased"`
	Closure           string                       `json:"closure"`
	Native            *GatewayNativePublishedRoute `json:"native,omitempty"`
}

type GatewayNativeConnectionCustodyResult struct {
	Contract        string                            `json:"contract"`
	Generation      string                            `json:"generation"`
	Native          GatewayNativePublishedRoute       `json:"native"`
	CredentialKinds []string                          `json:"credential_kinds"`
	Retention       GatewayNativeRetainedCustodyState `json:"retention"`
	Proof           GatewayNativeRetainedCustodyProof `json:"proof"`
}

// GatewayNativeConnectionAdapter is a composition-time set of existing native
// lifecycle ports. It contains no journal, state machine, submitted endpoint or
// arbitrary operation graph. Bootstrap supplies qualified method/configuration
// bindings and the actual execution adapter independently of this inventory.
type GatewayNativeConnectionAdapter struct {
	ProfileID      string
	Purpose        string
	MethodIDs      []string
	DispatchFamily string
	AuthFamily     string
	Begin          func(context.Context, GatewayNativeConnectionScope, map[string]string) (GatewayNativeConnectionResult, error)
	Read           func(context.Context, GatewayNativeConnectionScope) (GatewayNativeConnectionResult, error)
	Submit         func(context.Context, GatewayNativeConnectionScope, map[string]string) (GatewayNativeConnectionResult, error)
	Poll           func(context.Context, GatewayNativeConnectionScope) (GatewayNativeConnectionResult, error)
	Describe       func(context.Context, GatewayNativeConnectionScope) (GatewayNativeConnectionDescriptorResult, error)
	Refresh        func(context.Context, GatewayNativeConnectionScope) (GatewayNativeConnectionMaintenanceResult, error)
	Custody        func(context.Context, GatewayNativeConnectionScope, GatewayNativePublishedRoute) (GatewayNativeConnectionCustodyResult, error)
	Cleanup        func(context.Context, GatewayNativeConnectionScope, GatewayNativeOAuthCleanupRequest) (GatewayNativeConnectionCleanupResult, error)
}

// Supports verifies that every operation needed by this primitive and retained
// credential lifecycle is actually connected. Inventory membership is not
// sufficient to advertise the method, and nil operations are never successes.
func (a GatewayNativeConnectionAdapter) Supports(method GatewayConnectionMethodDefinition) bool {
	if a.ProfileID == "" || a.Purpose == "" || a.DispatchFamily == "" ||
		a.Begin == nil || a.Read == nil || a.Submit == nil || a.Describe == nil || a.Refresh == nil || a.Custody == nil || a.Cleanup == nil {
		return false
	}
	found := false
	for _, id := range a.MethodIDs {
		found = found || id == method.MethodID
	}
	return found && (method.Interaction.Kind != "device" || a.Poll != nil)
}
