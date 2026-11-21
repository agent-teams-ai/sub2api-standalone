package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Legacy OAuth accounts keep their original repository and encrypted custody.
// This reader has no OAuth client, current mount, refresh or provider HTTP port.
// Its fixed model is the same native-qualified anchor used by their dispatcher.
type GatewayLegacyOAuthAccountModels struct {
	claude *GatewayNativeClaudeOAuth
	grok   *GatewayNativeGrokOAuth
}

func NewGatewayLegacyOAuthAccountModels(claude GatewayNativeClaudeRepository, grok GatewayNativeGrokRepository, custody *GatewayNativeCredentialCustody) (*GatewayLegacyOAuthAccountModels, error) {
	if claude == nil || grok == nil || !GatewayNativeCredentialCustodyReady(custody) {
		return nil, ErrGatewayNativeIdentity
	}
	return &GatewayLegacyOAuthAccountModels{
		claude: &GatewayNativeClaudeOAuth{repository: claude, custody: custody},
		grok:   &GatewayNativeGrokOAuth{repository: grok, custody: custody},
	}, nil
}

func (s *GatewayLegacyOAuthAccountModels) read(ctx context.Context, in GatewayAccountModelsInput, before GatewayNativeRoute, authorize GatewayAccountLimitsAuthorizer) GatewayAccountModelsResultV2 {
	unavailable := GatewayAccountModelsResultV2{Status: "unavailable"}
	if s == nil || ctx == nil || authorize == nil || !GatewayAccountInfoRouteValid(before, in.GatewayAccountLimitsInput) {
		return unavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	ctx, err := WithGatewayNativeOAuthOwner(ctx, in.OwnerRef)
	if err != nil {
		return unavailable
	}
	var scope GatewayNativeCredentialScope
	var operation string
	switch in.ProfileID {
	case GatewayClaudeOAuthMessagesProfile:
		scope, operation, err = s.claude.repository.ResolveGatewayClaudeCanonical(ctx, before, in.AccountRef)
	case GatewayGrokOAuthResponsesProfile:
		scope, operation, err = s.grok.repository.ResolveGatewayGrokCanonical(ctx, before, in.AccountRef)
	default:
		return unavailable
	}
	if err != nil || !GatewayNativeOAuthScopeAuthorized(ctx, scope) || scope.Owner != in.OwnerRef || scope.Account != in.AccountRef || scope.Generation != before.Generation || !GatewayNativeCredentialRefValid(operation) {
		return unavailable
	}
	physical, envelope, err := s.snapshot(ctx, scope, operation, before)
	if err != nil {
		return unavailable
	}
	models := GatewayNativeConnectionModels(before.Profile, before.Model)
	if len(models) != 1 || models[0].ModelID != before.Model {
		return unavailable
	}
	// No SQL lock is held across the owner callback. Reacquiring the exact birth
	// and authenticating custody again discards a concurrent erase or rotation.
	after, err := authorize(ctx, in.GatewayAccountLimitsInput)
	if err != nil || ctx.Err() != nil || !SameGatewayNativeDescriptor(before, after) {
		return unavailable
	}
	fresh, currentEnvelope, err := s.snapshot(ctx, scope, operation, after)
	if err != nil || !sameGatewayOAuthPhysical(physical, fresh) || physical.CredentialRevision != fresh.CredentialRevision || envelope != currentEnvelope || ctx.Err() != nil {
		return unavailable
	}
	// Only immutable native identity and credential revision enter this digest;
	// no token, provider instruction or encrypted credential is projected.
	raw, _ := json.Marshal([]any{GatewayAccountInfoPrincipal(physical), operation, physical.CredentialRevision})
	digest := sha256.Sum256(append([]byte("native-account-models/legacy-oauth/v1\x00"), raw...))
	now := time.Now().UTC()
	return projectGatewayAccountModelsV2(in.ProfileID, GatewayAccountModelsResult{
		Status: "snapshot", PhysicalGeneration: scope.Generation, PrincipalVersion: hex.EncodeToString(digest[:]),
		FetchedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(gatewayAccountModelsSnapshotTTL).Format(time.RFC3339Nano), Models: &models,
	})
}

func (s *GatewayLegacyOAuthAccountModels) snapshot(ctx context.Context, scope GatewayNativeCredentialScope, operation string, route GatewayNativeRoute) (GatewayNativeOAuthPhysical, string, error) {
	var physical GatewayNativeOAuthPhysical
	var envelope string
	var release func()
	var err error
	switch route.Profile {
	case GatewayClaudeOAuthMessagesProfile:
		physical, envelope, release, err = s.claude.repository.LockGatewayClaudePhysical(ctx, scope, operation, &route)
	case GatewayGrokOAuthResponsesProfile:
		physical, envelope, release, err = s.grok.repository.LockGatewayGrokPhysical(ctx, scope, operation, &route)
	default:
		return physical, "", ErrGatewayNativeIdentity
	}
	if release != nil {
		defer release()
	}
	if err != nil || release == nil || physical.Scope != scope || physical.Operation != operation || !SameGatewayNativeDescriptor(physical.Route, route) || physical.CredentialRevision < 1 {
		return GatewayNativeOAuthPhysical{}, "", ErrGatewayNativeIdentity
	}
	if selected := gatewayConnectionSelectionFromContext(ctx); selected != nil && !SameGatewayConnectionSelection(selected, physical.Connection) {
		return GatewayNativeOAuthPhysical{}, "", ErrGatewayNativeIdentity
	}
	if physical.Connection != nil {
		selection := *physical.Connection
		if selection.ProfileID != route.Profile || selection.MethodID != "browser" {
			return GatewayNativeOAuthPhysical{}, "", ErrGatewayNativeIdentity
		}
		ctx, err = WithGatewayConnectionSelection(ctx, selection)
		if err != nil {
			return GatewayNativeOAuthPhysical{}, "", ErrGatewayNativeIdentity
		}
		physical.Connection = &selection
	}
	if !GatewayConnectionSelectionAuthorized(ctx, physical.Connection) {
		return GatewayNativeOAuthPhysical{}, "", ErrGatewayNativeIdentity
	}
	switch route.Profile {
	case GatewayClaudeOAuthMessagesProfile:
		_, err = s.claude.bundle(scope, envelope, physical)
	case GatewayGrokOAuthResponsesProfile:
		_, err = s.grok.retainedBundle(scope, envelope, physical)
	}
	if err != nil || ctx.Err() != nil {
		return GatewayNativeOAuthPhysical{}, "", ErrGatewayNativeIdentity
	}
	return physical, envelope, nil
}
