package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"time"
)

const GatewayAccountModelsContractV2 = "native-account-models/v2"

type GatewayAccountModelsAdmission struct {
	UnavailableFallback string `json:"unavailableFallback"`
}

// V2 names the native source and its unavailable policy without extending the
// closed source enum of v1. It carries no provider or credential document.
type GatewayAccountModelsResultV2 struct {
	Status             string                         `json:"status"`
	SourceID           string                         `json:"sourceId,omitempty"`
	ProfileID          string                         `json:"profileId,omitempty"`
	PhysicalGeneration string                         `json:"physicalGeneration,omitempty"`
	PrincipalVersion   string                         `json:"principalVersion,omitempty"`
	FetchedAt          string                         `json:"fetchedAt,omitempty"`
	ExpiresAt          string                         `json:"expiresAt,omitempty"`
	Admission          *GatewayAccountModelsAdmission `json:"admission,omitempty"`
	Models             *[]GatewayAccountModel         `json:"models,omitempty"`
}

type GatewayAccountModelsReaderV2 struct {
	native    *GatewayNativeConnections
	legacy    *GatewayAccountModelsReader
	authorize GatewayAccountLimitsAuthorizer
	oauth     *GatewayLegacyOAuthAccountModels
}

func NewGatewayAccountModelsReaderV2(native *GatewayNativeConnections, legacy *GatewayAccountModelsReader, authorize GatewayAccountLimitsAuthorizer, oauth ...*GatewayLegacyOAuthAccountModels) (*GatewayAccountModelsReaderV2, error) {
	if authorize == nil || len(oauth) > 1 {
		return nil, ErrGatewayNativeIdentity
	}
	var retained *GatewayLegacyOAuthAccountModels
	if len(oauth) == 1 {
		retained = oauth[0]
	}
	if native == nil && legacy == nil && retained == nil {
		return nil, ErrGatewayNativeIdentity
	}
	return &GatewayAccountModelsReaderV2{native: native, legacy: legacy, authorize: authorize, oauth: retained}, nil
}

func projectGatewayAccountModelsV2(profile string, result GatewayAccountModelsResult) GatewayAccountModelsResultV2 {
	if result.Status != "snapshot" {
		if result.Status == "unknown" || result.Status == "unsupported" {
			return GatewayAccountModelsResultV2{Status: result.Status}
		}
		return GatewayAccountModelsResultV2{Status: "unavailable"}
	}
	definition, found := LookupGatewayConnectionProfile(profile)
	fetched, fetchedErr := time.Parse(time.RFC3339Nano, result.FetchedAt)
	expires, expiresErr := time.Parse(time.RFC3339Nano, result.ExpiresAt)
	if !found || result.Models == nil || len(*result.Models) > gatewayLiveModelsMax ||
		!GatewayNativeCredentialRefValid(result.PhysicalGeneration) || !GatewayNativeCredentialRefValid(result.PrincipalVersion) ||
		fetchedErr != nil || expiresErr != nil || !fetched.Before(expires) || !time.Now().Before(expires) {
		return GatewayAccountModelsResultV2{Status: "unavailable"}
	}
	return GatewayAccountModelsResultV2{Status: "snapshot", SourceID: profile, ProfileID: profile,
		PhysicalGeneration: result.PhysicalGeneration, PrincipalVersion: result.PrincipalVersion,
		FetchedAt: result.FetchedAt, ExpiresAt: result.ExpiresAt,
		Admission: &GatewayAccountModelsAdmission{UnavailableFallback: definition.UnavailableFallback}, Models: result.Models}
}

func (s *GatewayAccountModelsReaderV2) Read(ctx context.Context, in GatewayAccountModelsInput) GatewayAccountModelsResultV2 {
	unavailable := GatewayAccountModelsResultV2{Status: "unavailable"}
	if s == nil || ctx == nil || !in.Valid() {
		return unavailable
	}
	before, err := s.authorize(ctx, in.GatewayAccountLimitsInput)
	if err != nil || ctx.Err() != nil {
		return unavailable
	}
	if s.native != nil {
		bound, marked, err := s.native.BindAccountRead(ctx, in.GatewayAccountLimitsInput, before)
		if err != nil {
			return unavailable
		}
		if marked {
			result := s.native.ReadModels(bound, in)
			after, err := s.authorize(ctx, in.GatewayAccountLimitsInput)
			if err != nil || !SameGatewayNativeDescriptor(before, after) || !s.native.CheckAccountRead(bound, in.GatewayAccountLimitsInput, after) {
				return unavailable
			}
			return result
		}
	}
	if in.ProfileID == GatewayClaudeOAuthMessagesProfile || in.ProfileID == GatewayGrokOAuthResponsesProfile {
		return s.oauth.read(ctx, in, before, s.authorize)
	}
	if s.legacy == nil {
		return unavailable
	}
	// The old reader retains its own before/after owner and physical fences.
	result := s.legacy.Read(ctx, in)
	if result.Status == "snapshot" && result.PhysicalGeneration != before.Generation {
		return unavailable
	}
	return projectGatewayAccountModelsV2(in.ProfileID, result)
}

type gatewayConnectionAccountReadBinding struct {
	service  *GatewayNativeConnections
	input    GatewayAccountLimitsInput
	physical GatewayNativeConnectionPhysical
}
type gatewayConnectionAccountReadKey struct{}

// BindAccountRead follows a freshly authorized logical owner/epoch callback.
// The stored publication is the authority even after today's method is removed.
func (s *GatewayNativeConnections) BindAccountRead(ctx context.Context, in GatewayAccountLimitsInput, route GatewayNativeRoute) (context.Context, bool, error) {
	if s == nil || ctx == nil || !in.Valid() || route.Profile != in.ProfileID {
		return nil, false, ErrGatewayNativeIdentity
	}
	repository, ok := s.repository.(GatewayNativeConnectionCanonicalRepository)
	if !ok {
		return nil, false, ErrGatewayNativeIdentity
	}
	marked, err := repository.GatewayNativeConnectionRouteMarked(ctx, route)
	if err != nil || !marked {
		return ctx, marked, err
	}
	scope, operation, publication, err := repository.ResolveGatewayNativeConnectionCanonical(ctx, route, in.AccountRef)
	consumer, consumerErr := GatewayNativeConsumer(ctx)
	if err != nil || consumerErr != nil || scope.Consumer != consumer || scope.Owner != in.OwnerRef || scope.Account != in.AccountRef ||
		scope.Generation != route.Generation || publication.Connection.ProfileID != route.Profile {
		return nil, true, ErrGatewayNativeIdentity
	}
	ctx, err = WithGatewayNativeOAuthOwner(ctx, scope.Owner)
	if err == nil {
		ctx, err = WithGatewayConnectionSelection(ctx, publication.Connection)
	}
	if err != nil {
		return nil, true, err
	}
	native := GatewayNativePublishedRoute{GatewayNativeRoute: route, Publication: publication}
	physical, release, err := s.repository.LockGatewayNativeConnection(ctx, scope, operation, &native)
	if err != nil || release == nil {
		return nil, true, ErrGatewayNativeIdentity
	}
	defer release()
	raw, err := AuthenticateGatewayNativeConnectionPhysical(ctx, s.custody, physical)
	clear(raw)
	if err != nil || ctx.Err() != nil {
		return nil, true, ErrGatewayNativeIdentity
	}
	snapshot := *physical.Account
	snapshot.Credentials = maps.Clone(physical.Account.Credentials)
	snapshot.Extra = maps.Clone(physical.Account.Extra)
	physical.Account = &snapshot
	return context.WithValue(ctx, gatewayConnectionAccountReadKey{}, gatewayConnectionAccountReadBinding{s, in, physical}), true, nil
}

func (s *GatewayNativeConnections) accountReadBinding(ctx context.Context, in GatewayAccountLimitsInput) (gatewayConnectionAccountReadBinding, bool) {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return gatewayConnectionAccountReadBinding{}, false
	}
	binding, ok := ctx.Value(gatewayConnectionAccountReadKey{}).(gatewayConnectionAccountReadBinding)
	return binding, ok && binding.service == s && binding.input == in &&
		GatewayConnectionScopeAuthorized(ctx, binding.physical.Scope) && GatewayConnectionSelectionAuthorized(ctx, &binding.physical.Native.Publication.Connection)
}

// The second retained-lock check discards a result observed before credentials,
// publication, scope or physical birth changed while an external call ran.
func (s *GatewayNativeConnections) CheckAccountRead(ctx context.Context, in GatewayAccountLimitsInput, route GatewayNativeRoute) bool {
	binding, ok := s.accountReadBinding(ctx, in)
	if !ok || !SameGatewayNativeDescriptor(binding.physical.Native.GatewayNativeRoute, route) {
		return false
	}
	before := binding.physical
	fresh, release, err := s.repository.LockGatewayNativeConnection(ctx, before.Scope, before.Operation, &before.Native)
	if err != nil || release == nil {
		return false
	}
	defer release()
	if fresh.CredentialVersion != before.CredentialVersion || !reflect.DeepEqual(fresh.Account.Credentials, before.Account.Credentials) || !reflect.DeepEqual(fresh.Account.Extra, before.Account.Extra) {
		return false
	}
	raw, err := AuthenticateGatewayNativeConnectionPhysical(ctx, s.custody, fresh)
	clear(raw)
	return err == nil && ctx.Err() == nil
}

// This identity is local native custody provenance, never provider billing or
// entitlement. Selection and credential revision prevent cross-method reuse.
func GatewayNativeConnectionObservationPrincipal(p GatewayNativeConnectionPhysical) string {
	raw, _ := json.Marshal([]any{p.Scope, p.Native, p.CredentialVersion})
	digest := sha256.Sum256(append([]byte("native-account-observation/v1\x00"), raw...))
	return hex.EncodeToString(digest[:])
}

func gatewayConnectionOpenRouterPrincipal(p GatewayNativeConnectionPhysical) string {
	return GatewayNativeConnectionObservationPrincipal(p)
}

func (s *GatewayNativeConnections) ReadModels(ctx context.Context, in GatewayAccountModelsInput) GatewayAccountModelsResultV2 {
	binding, ok := s.accountReadBinding(ctx, in.GatewayAccountLimitsInput)
	if !ok || !s.CheckAccountRead(ctx, in.GatewayAccountLimitsInput, binding.physical.Native.GatewayNativeRoute) {
		return GatewayAccountModelsResultV2{Status: "unavailable"}
	}
	p := binding.physical
	// SystemOne has neither the Responses grammar nor streaming/tool semantics.
	// A renderer-compatible credential form cannot make that execution supported.
	if p.Native.Publication.DispatchFamily == "typesafe_systemone" {
		return GatewayAccountModelsResultV2{Status: "unsupported"}
	}
	var result GatewayAccountModelsResult
	if p.Native.Publication.DispatchFamily == "codex_responses" {
		result = s.codexImportModels(ctx, p, in.ForceRefresh)
	} else if p.Native.Profile == GatewayOpenRouterResponsesProfile {
		if s.gateway == nil || s.gateway.gatewayOpenRouterModels == nil {
			return GatewayAccountModelsResultV2{Status: "unavailable"}
		}
		result = s.gateway.gatewayOpenRouterModels.read(ctx, gatewayConnectionOpenRouterPrincipal(p), in.ForceRefresh)
	} else {
		models := GatewayNativeConnectionModels(p.Native.Profile, p.Native.Model)
		if GatewayModelCatalogProfile(p.Native.Profile) && s.gateway != nil {
			models = s.connectionCatalogueModels(ctx, in, p)
		}
		if models == nil {
			return GatewayAccountModelsResultV2{Status: "unsupported"}
		}
		now := time.Now().UTC()
		result = GatewayAccountModelsResult{Status: "snapshot", Models: &models, FetchedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(gatewayAccountModelsSnapshotTTL).Format(time.RFC3339Nano)}
	}
	result.PhysicalGeneration = p.Scope.Generation
	result.PrincipalVersion = GatewayNativeConnectionObservationPrincipal(p)
	return projectGatewayAccountModelsV2(in.ProfileID, result)
}

func (s *GatewayNativeConnections) connectionCatalogueModels(ctx context.Context, in GatewayAccountModelsInput, p GatewayNativeConnectionPhysical) []GatewayAccountModel {
	profile := p.Native.Profile
	if GatewayNativeOfficialAPIProfileIDValid(profile) {
		return gatewayCatalogAccountModels(profile, p.Native.Model, nil)
	}
	live := s.gateway.liveModels()
	key := gatewayLiveModelsKey(profile, p.Scope, p.Account.GetCredential(GatewayConnectionBundleCredentialKey))
	_, public := gatewayLiveModelsSource(profile)
	live.refresh(ctx, key, in.ForceRefresh, func(fetchCtx context.Context) ([]string, error) {
		bounded, cancel := context.WithTimeout(fetchCtx, gatewayLiveModelsTimeout)
		defer cancel()
		if public {
			return live.client.FetchModels(bounded, profile, nil)
		}
		fresh, release, err := s.repository.LockGatewayNativeConnection(bounded, p.Scope, p.Operation, &p.Native)
		if err != nil || release == nil {
			return nil, ErrGatewayNativeIdentity
		}
		defer release()
		if fresh.CredentialVersion != p.CredentialVersion {
			return nil, ErrGatewayNativeIdentity
		}
		raw, err := AuthenticateGatewayNativeConnectionPhysical(bounded, s.custody, fresh)
		defer clear(raw)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		var credential string
		if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["api_key"], &credential) != nil {
			return nil, ErrGatewayNativeIdentity
		}
		keyBytes := []byte(credential)
		defer clear(keyBytes)
		return live.client.FetchModels(bounded, profile, keyBytes)
	})
	return gatewayCatalogAccountModels(profile, p.Native.Model, live.peek(key))
}

// Dispatch reads the same native sources without network, refresh or a
// consumer-owned fallback. A cache miss cannot authorize an arbitrary model.
func (s *GatewayNativeConnections) GatewayNativeConnectionModelAllowed(ctx context.Context, native GatewayNativePublishedRoute, model string) bool {
	if s == nil || ctx == nil || !GatewayAccountModelIDValid(model) || native.Publication.DispatchFamily == "typesafe_systemone" {
		return false
	}
	binding, ok := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	if !ok || binding.service != s || binding.native.Publication != native.Publication || !SameGatewayNativeDescriptor(binding.native.GatewayNativeRoute, native.GatewayNativeRoute) {
		return false
	}
	p, release, err := s.repository.LockGatewayNativeConnection(ctx, binding.in.Scope, binding.in.Operation, &native)
	if err != nil || release == nil {
		return false
	}
	defer release()
	raw, err := AuthenticateGatewayNativeConnectionPhysical(ctx, s.custody, p)
	clear(raw)
	if err != nil || ctx.Err() != nil {
		return false
	}
	models := GatewayNativeConnectionModels(native.Profile, native.Model)
	if native.Publication.DispatchFamily == "codex_responses" {
		models = s.cachedCodexImportModels(p)
	} else if native.Profile == GatewayOpenRouterResponsesProfile {
		if s.gateway == nil || s.gateway.gatewayOpenRouterModels == nil {
			return false
		}
		cache := &s.gateway.gatewayOpenRouterModels.cache
		cache.Lock()
		entry, found := cache.entries[gatewayConnectionOpenRouterPrincipal(p)]
		cache.Unlock()
		if !found || !time.Now().Before(entry.expires) || entry.result.Status != "snapshot" || entry.result.Models == nil {
			return false
		}
		models = *entry.result.Models
	} else if GatewayModelCatalogProfile(native.Profile) && s.gateway != nil {
		key := gatewayLiveModelsKey(native.Profile, p.Scope, p.Account.GetCredential(GatewayConnectionBundleCredentialKey))
		models = gatewayCatalogAccountModels(native.Profile, native.Model, s.gateway.liveModels().peek(key))
	}
	return slices.ContainsFunc(models, func(candidate GatewayAccountModel) bool { return candidate.ModelID == model })
}
