package service

import (
	"context"
	"time"
)

// These are existing journal/custody ports, with no provider or refresh writer.
// Composition may retain them after the current OAuth mount is removed.
type GatewayNativeCodexRecoveryRepository interface {
	GatewayNativeOAuthDispatchRepository
	GatewayNativeOAuthConnectReadback
	GatewayNativeOAuthCleanupRepository
	GatewayNativeRetainedOAuthCustodyRepository
}

func gatewayLegacyRecoveryAdapter(profile, dispatchFamily, authFamily string, methods []string, ports gatewayBoundOAuthPorts) (GatewayNativeConnectionAdapter, error) {
	deny := func(context.Context, GatewayNativeConnectionScope, map[string]string) (GatewayNativeOAuthConnectResult, error) {
		return GatewayNativeOAuthConnectResult{}, ErrGatewayNativeIdentity
	}
	ports.begin, ports.submit = deny, deny
	ports.poll = func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthConnectResult, error) {
		return GatewayNativeOAuthConnectResult{}, ErrGatewayNativeIdentity
	}
	ports.refresh = func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthMaintenanceOutcome, error) {
		return GatewayNativeOAuthMaintenanceOutcome{}, ErrGatewayNativeIdentity
	}
	adapter, err := newGatewayBoundOAuthAdapter(profile, dispatchFamily, authFamily, methods, ports)
	if err != nil {
		return GatewayNativeConnectionAdapter{}, err
	}
	adapter.MethodIDs = []string{}
	adapter.RecoveryMethodIDs = append([]string(nil), methods...)
	return adapter, nil
}

func NewGatewayNativeCodexRecoveryConnectionAdapter(journal GatewayNativeOAuthConnectRepository, repo GatewayNativeCodexRecoveryRepository, custody *GatewayNativeCredentialCustody, cleanup GatewayNativeOAuthCleanupAuthorizer) (GatewayNativeConnectionAdapter, error) {
	if journal == nil || repo == nil || !GatewayNativeCredentialCustodyReady(custody) || cleanup == nil {
		return GatewayNativeConnectionAdapter{}, ErrGatewayNativeIdentity
	}
	// ReadConnect reconciles only already committed enrollment acknowledgements.
	// With no client, enrollment service, key or entry callback, no new grant can
	// be started through this object, even accidentally inside a recovery port.
	connect := &GatewayNativeOAuthConnect{repository: journal, readback: repo, now: time.Now}
	dispatch := &GatewayNativeOAuthDispatch{repository: repo, custody: custody}
	return gatewayLegacyRecoveryAdapter(GatewayCodexOAuthResponsesProfile, "codex_responses", "codex_oauth", []string{"browser", "device_code", "refresh_token", "auth_json"}, gatewayBoundOAuthPorts{
		read: connect.ReadConnect, describe: dispatch.retainedConnectionDescriptor, custody: dispatch.RetainedCustody,
		cleanup: func(ctx context.Context, request GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error) {
			return connect.Cleanup(ctx, request, cleanup)
		},
	})
}

// Unlike Descriptor this read can never perform JWT/JWKS/account-check HTTP or
// promote a pending row. Only a saved qualification and authenticated custody
// permit returning the original physical descriptor.
func (s *GatewayNativeOAuthDispatch) retainedConnectionDescriptor(ctx context.Context, scope GatewayNativeCredentialScope, operation string) (GatewayNativeOAuthDescriptorResult, error) {
	out := GatewayNativeOAuthDescriptorResult{Operation: operation, Account: scope.Account, State: "staged", Qualification: "pending"}
	if s == nil || !GatewayNativeOAuthScopeAuthorized(ctx, scope) || !GatewayNativeCredentialRefValid(operation) {
		return out, ErrGatewayNativeIdentity
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	row, err := s.repository.ReadGatewayNativeOAuthDispatch(bounded, scope, operation)
	if err != nil || row.Operation != operation || row.Generation != scope.Generation {
		return out, ErrGatewayNativeIdentity
	}
	if row.State != "completed" {
		switch row.State {
		case "prepared", "entered", "unknown", "expired":
			return out, nil
		default:
			return out, ErrGatewayNativeIdentity
		}
	}
	if row.AccountID <= 0 {
		return out, ErrGatewayNativeIdentity
	}
	account, physical, release, err := s.repository.LockGatewayNativeOAuthDispatch(bounded, scope, operation, row.AccountID)
	if err != nil || release == nil {
		return out, ErrGatewayNativeIdentity
	}
	defer release()
	if physical.Scope != scope || physical.Operation != operation || !GatewayConnectionSelectionAuthorized(bounded, physical.Connection) || gatewayOAuthPhysicalValid(account, physical) != nil {
		return out, ErrGatewayNativeIdentity
	}
	version, err := gatewayOAuthCredentialVersion(account)
	if err != nil {
		return out, ErrGatewayNativeIdentity
	}
	bundle, err := s.custody.openOAuthVersion(account.GetCredential("oauth_bundle"), scope, version)
	defer clear(bundle.SensitiveMetadata)
	if err != nil || !SameGatewayConnectionSelection(bundle.Connection, physical.Connection) {
		return out, ErrGatewayNativeIdentity
	}
	if !physical.QualifiedAt.IsZero() {
		out.Qualification, out.Native = GatewayCodexOAuthQualification, &physical.Route
	}
	return out, nil
}

func NewGatewayNativeClaudeRecoveryConnectionAdapter(repo GatewayNativeClaudeRepository, custody *GatewayNativeCredentialCustody, cleanup GatewayNativeOAuthCleanupAuthorizer) (GatewayNativeConnectionAdapter, error) {
	if repo == nil || !GatewayNativeCredentialCustodyReady(custody) || cleanup == nil {
		return GatewayNativeConnectionAdapter{}, ErrGatewayNativeIdentity
	}
	native := &GatewayNativeClaudeOAuth{repository: repo, custody: custody}
	return gatewayLegacyRecoveryAdapter(GatewayClaudeOAuthMessagesProfile, "claude_messages", "claude_oauth", []string{"browser"}, gatewayBoundOAuthPorts{
		read: native.ReadConnect, describe: native.Descriptor, custody: native.RetainedCustody,
		cleanup: func(ctx context.Context, request GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error) {
			return native.Cleanup(ctx, request, cleanup)
		},
	})
}

func NewGatewayNativeGrokRecoveryConnectionAdapter(repo GatewayNativeGrokRepository, custody *GatewayNativeCredentialCustody, cleanup GatewayNativeOAuthCleanupAuthorizer) (GatewayNativeConnectionAdapter, error) {
	if repo == nil || !GatewayNativeCredentialCustodyReady(custody) || cleanup == nil {
		return GatewayNativeConnectionAdapter{}, ErrGatewayNativeIdentity
	}
	native := &GatewayNativeGrokOAuth{repository: repo, custody: custody}
	return gatewayLegacyRecoveryAdapter(GatewayGrokOAuthResponsesProfile, "grok_responses", "grok_oauth", []string{"browser"}, gatewayBoundOAuthPorts{
		read: native.ReadConnect,
		describe: func(ctx context.Context, scope GatewayNativeCredentialScope, operation string) (GatewayNativeOAuthDescriptorResult, error) {
			physical, _, err := native.retainedConnectionBundle(ctx, scope, operation, nil)
			if err != nil {
				return GatewayNativeOAuthDescriptorResult{}, err
			}
			return GatewayNativeOAuthDescriptorResult{Operation: operation, Account: scope.Account, State: "staged", Qualification: "controlled-source-v1", Native: &physical.Route}, nil
		},
		custody: func(ctx context.Context, scope GatewayNativeCredentialScope, operation string, route GatewayNativeRoute) (GatewayNativeRetainedCustodyResult, error) {
			_, expiry, err := native.retainedConnectionBundle(ctx, scope, operation, &route)
			if err != nil {
				return GatewayNativeRetainedCustodyResult{}, err
			}
			out := retainedCustodyResult(route, GatewayNativeRetainedCustodyOAuthKind, "inactive")
			eligibility := "not-due-by-existing-policy"
			if !time.Now().Add(5 * time.Minute).Before(expiry) {
				eligibility = "eligible-by-existing-policy"
			}
			out.OAuthPolicy = &GatewayNativeOAuthRetentionPolicy{ProviderValidity: "unasserted", ExpiredButRefreshable: eligibility}
			return out, nil
		},
		cleanup: func(ctx context.Context, request GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error) {
			return native.Cleanup(ctx, request, cleanup)
		},
	})
}

func (s *GatewayNativeGrokOAuth) retainedConnectionBundle(ctx context.Context, scope GatewayNativeCredentialScope, operation string, route *GatewayNativeRoute) (GatewayNativeOAuthPhysical, time.Time, error) {
	var empty GatewayNativeOAuthPhysical
	if s == nil || !grokScope(ctx, scope, operation) {
		return empty, time.Time{}, ErrGatewayNativeIdentity
	}
	physical, envelope, release, err := s.repository.LockGatewayGrokPhysical(ctx, scope, operation, route)
	if err != nil || release == nil {
		return empty, time.Time{}, ErrGatewayNativeIdentity
	}
	defer release()
	if !grokPhysicalValid(physical, scope, operation, route) || !GatewayConnectionSelectionAuthorized(ctx, physical.Connection) {
		return empty, time.Time{}, ErrGatewayNativeIdentity
	}
	var saved gatewayGrokBundle
	if s.open(scope, envelope, &saved) != nil || !grokPrincipal(saved.ClientID) {
		return empty, time.Time{}, ErrGatewayNativeIdentity
	}
	// This client identity comes from authenticated, scope-bound sealed custody.
	// It is used only to reuse the existing validator; no current configuration,
	// browser continuation, refresh or dispatch capability is manufactured.
	retained := *s
	retained.qualification = GatewayNativeGrokOAuthQualification{ClientID: saved.ClientID}
	saved, err = retained.bundle(scope, envelope, physical)
	if err != nil {
		return empty, time.Time{}, ErrGatewayNativeIdentity
	}
	return physical, saved.ExpiresAt, nil
}
