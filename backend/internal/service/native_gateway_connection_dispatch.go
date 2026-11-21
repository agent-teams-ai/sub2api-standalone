package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"reflect"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

type GatewayNativeConnectionCanonicalRepository interface {
	ResolveGatewayNativeConnectionCanonical(context.Context, GatewayNativeRoute, string) (GatewayNativeCredentialScope, string, GatewayConnectionPublication, error)
	GatewayNativeConnectionRouteMarked(context.Context, GatewayNativeRoute) (bool, error)
}

// BindRoute distinguishes a stored native publication from a legacy physical
// row before choosing its decoder. An unavailable or mismatched row is an error,
// never permission to try another credential lifecycle.
func (s *GatewayNativeConnections) BindRoute(ctx context.Context, route GatewayNativeRoute, account string, authorizationEpoch, requestBytes, tokens int64) (context.Context, bool, error) {
	if s == nil || ctx == nil {
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
	bound, err := s.BindApproved(ctx, route, account, authorizationEpoch, requestBytes, tokens)
	return bound, true, err
}

type gatewayConnectionDispatchBinding struct {
	service      *GatewayNativeConnections
	in           GatewayNativeConnectionScope
	native       GatewayNativePublishedRoute
	requestBytes int64
	tokens       int64
}
type gatewayConnectionDispatchBindingKey struct{}

type gatewayConnectionDispatchState struct {
	binding        gatewayConnectionDispatchBinding
	physical       GatewayNativeConnectionPhysical
	account        *Account
	credentials    [32]byte
	requestedModel string
	entered        atomic.Bool
}
type gatewayConnectionDispatchStateKey struct{}

// BindApproved uses only the exact existing six-field physical descriptor and
// authenticated logical account. Publication is resolved from native custody,
// not copied from a caller's latest method/configuration or another credential.
func (s *GatewayNativeConnections) BindApproved(ctx context.Context, route GatewayNativeRoute, account string, authorizationEpoch, requestBytes, tokens int64) (context.Context, error) {
	if s == nil || ctx == nil || authorizationEpoch < 0 || requestBytes < 1 || requestBytes > 4<<20 || tokens < 1 {
		return nil, ErrGatewayNativeIdentity
	}
	repository, ok := s.repository.(GatewayNativeConnectionCanonicalRepository)
	if !ok {
		return nil, ErrGatewayNativeIdentity
	}
	scope, operation, publication, err := repository.ResolveGatewayNativeConnectionCanonical(ctx, route, account)
	consumer, consumerErr := GatewayNativeConsumer(ctx)
	if err != nil || consumerErr != nil || scope.Consumer != consumer || scope.Account != account || scope.Generation != route.Generation || publication.Connection.ProfileID != route.Profile {
		return nil, ErrGatewayNativeIdentity
	}
	ctx, err = WithGatewayNativeOAuthOwner(ctx, scope.Owner)
	if err != nil {
		return nil, err
	}
	ctx, err = WithGatewayConnectionSelection(ctx, publication.Connection)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, gatewayConnectionDispatchBindingKey{}, gatewayConnectionDispatchBinding{
		service: s, in: GatewayNativeConnectionScope{Scope: scope, Operation: operation, Connection: publication.Connection},
		native: GatewayNativePublishedRoute{GatewayNativeRoute: route, Publication: publication}, requestBytes: requestBytes, tokens: tokens,
	}), nil
}

func GatewayNativeConnectionDispatchBound(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	return ok
}

func gatewayConnectionDocument(raw json.RawMessage) (map[string]any, [32]byte, error) {
	var zero [32]byte
	if _, err := gatewayOAuthJSON(raw); err != nil {
		return nil, zero, err
	}
	var document map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&document) != nil || len(document) == 0 {
		return nil, zero, ErrGatewayNativeIdentity
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return nil, zero, ErrGatewayNativeIdentity
	}
	defer clear(canonical)
	return document, sha256.Sum256(canonical), nil
}

// ForwardGatewayNativeConnection is the generic private transport hook. The
// selected native family owns protocol translation; the common path owns exact
// custody, limits, request lifetime and the existing last-HTTP entered CAS.
func ForwardGatewayNativeConnection(ctx context.Context, c *gin.Context, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, bool, error) {
	if ctx == nil {
		return nil, false, ErrGatewayNativeIdentity
	}
	binding, ok := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	life := gatewayNativeLifetime(ctx)
	if !ok || binding.service == nil || c == nil || c.Request == nil || life == nil || !life.matchesScope(binding.in.Scope) ||
		!SameGatewayNativeDescriptor(route, binding.native.GatewayNativeRoute) || !GatewayConnectionScopeAuthorized(ctx, binding.in.Scope) || int64(len(body)) > binding.requestBytes {
		return nil, false, ErrGatewayNativeIdentity
	}
	fields, valid := GatewayNativePayloadFields(body, "model", "stream", "store", "service_tier", "previous_response_id", "max_output_tokens")
	var model, tier string
	cap, capped := gatewayOAuthSeconds(fields["max_output_tokens"])
	if !valid || json.Unmarshal(fields["model"], &model) != nil || !GatewayAccountModelIDValid(model) || string(fields["stream"]) != "true" || string(fields["store"]) != "false" ||
		fields["previous_response_id"] != nil || !capped || cap > binding.tokens ||
		(fields["service_tier"] != nil && (json.Unmarshal(fields["service_tier"], &tier) != nil || tier != "default")) {
		return nil, false, ErrGatewayNativeIdentity
	}
	s := binding.service
	if !s.GatewayNativeConnectionModelAllowed(ctx, binding.native, model) {
		return nil, false, ErrGatewayNativeIdentity
	}
	dispatch, exists := s.dispatchers[binding.native.Publication.DispatchFamily]
	if !exists || dispatch.Forward == nil || dispatch.ValidateRequest == nil {
		return nil, false, ErrGatewayNativeIdentity
	}
	physical, release, err := s.repository.LockGatewayNativeConnection(ctx, binding.in.Scope, binding.in.Operation, &binding.native)
	if err != nil || release == nil {
		return nil, false, ErrGatewayNativeIdentity
	}
	raw, err := AuthenticateGatewayNativeConnectionPhysical(ctx, s.custody, physical)
	if err != nil {
		release()
		return nil, false, err
	}
	credentials, digest, err := gatewayConnectionDocument(raw)
	clear(raw)
	if err != nil {
		release()
		return nil, false, err
	}
	// The original row stays encrypted. No clone is inserted into a repository
	// or account cache; this credential view lives only through this native call.
	transient := new(Account)
	*transient = *physical.Account
	transient.Extra = make(map[string]any, len(physical.Account.Extra))
	for name, value := range physical.Account.Extra {
		transient.Extra[name] = value
	}
	transient.Extra[GatewayCredentialScopeExtraKey] = physical.Scope.Metadata()
	transient.Credentials = credentials
	defer clear(credentials)
	release()
	state := &gatewayConnectionDispatchState{binding: binding, physical: physical, account: transient, credentials: digest, requestedModel: model}
	ctx, finishUsage, err := withGatewayNativeConnectionUsage(ctx, physical.Native.Publication, model, &state.entered)
	if err != nil {
		return nil, false, err
	}
	defer finishUsage()
	ctx = WithHTTPUpstreamRedirectsDisabled(context.WithValue(ctx, gatewayConnectionDispatchStateKey{}, state))
	request := c.Request.Clone(ctx)
	request.Header = make(http.Header)
	c.Request = request
	result, err := dispatch.Forward(ctx, c, transient, route, body)
	entered := state.entered.Load()
	if err == nil && !entered {
		return nil, false, ErrGatewayNativeIdentity
	}
	if err != nil && err != ErrGatewayNativeIdentity && err != ErrGatewayNativeReplay {
		if entered {
			err = ErrGatewayNativeEffectUnknown
		} else {
			err = ErrGatewayNativeIdentity
		}
	}
	if err == nil {
		life.success()
	}
	return result, entered, err
}

// CheckGatewayNativeConnectionDispatch is called at the same last HTTP seam as
// old protected keys/OAuth. A recognized native row without its exact private
// context is denied; ordinary admin/scheduler routes cannot expose this secret.
func CheckGatewayNativeConnectionDispatch(request *http.Request, account *Account) (release func(), handled bool, err error) {
	noop := func() {}
	marked := account != nil && account.Extra[GatewayNativeConnectionAccountExtraKey] == true
	if request == nil || request.Context() == nil {
		if marked {
			return noop, true, ErrGatewayNativeIdentity
		}
		return noop, false, nil
	}
	state, _ := request.Context().Value(gatewayConnectionDispatchStateKey{}).(*gatewayConnectionDispatchState)
	if state == nil {
		if marked {
			return noop, true, ErrGatewayNativeIdentity
		}
		return noop, false, nil
	}
	if !marked || account != state.account || request.Method != http.MethodPost || request.URL == nil ||
		account.ID != state.physical.Native.AccountID || !account.CreatedAt.Equal(state.physical.Native.CreatedAt) ||
		account.Platform != state.physical.Account.Platform || account.Type != state.physical.Account.Type || !reflect.DeepEqual(account.Extra, state.physical.Account.Extra) {
		return noop, true, ErrGatewayNativeIdentity
	}
	canonical, marshalErr := json.Marshal(account.Credentials)
	if marshalErr != nil {
		return noop, true, ErrGatewayNativeIdentity
	}
	digest := sha256.Sum256(canonical)
	clear(canonical)
	if digest != state.credentials {
		return noop, true, ErrGatewayNativeIdentity
	}
	s := state.binding.service
	if !s.GatewayNativeConnectionModelAllowed(request.Context(), state.binding.native, state.requestedModel) {
		return noop, true, ErrGatewayNativeIdentity
	}
	dispatch := s.dispatchers[state.binding.native.Publication.DispatchFamily]
	if dispatch.ValidateRequest == nil || !dispatch.ValidateRequest(request, account, state.binding.native, state.requestedModel) {
		return noop, true, ErrGatewayNativeIdentity
	}
	fresh, unlock, lockErr := s.repository.LockGatewayNativeConnection(request.Context(), state.binding.in.Scope, state.binding.in.Operation, &state.binding.native)
	if lockErr != nil || unlock == nil {
		return noop, true, ErrGatewayNativeIdentity
	}
	if fresh.CredentialVersion != state.physical.CredentialVersion || !reflect.DeepEqual(fresh.Account.Credentials, state.physical.Account.Credentials) || !reflect.DeepEqual(fresh.Account.Extra, state.physical.Account.Extra) {
		unlock()
		return noop, true, ErrGatewayNativeIdentity
	}
	raw, openErr := AuthenticateGatewayNativeConnectionPhysical(request.Context(), s.custody, fresh)
	if openErr != nil {
		unlock()
		return noop, true, ErrGatewayNativeIdentity
	}
	freshDocument, freshDigest, decodeErr := gatewayConnectionDocument(raw)
	clear(freshDocument)
	clear(raw)
	if decodeErr != nil || freshDigest != state.credentials {
		unlock()
		return noop, true, ErrGatewayNativeIdentity
	}
	if !gatewayNativeEntered(request.Context(), &state.entered) {
		unlock()
		return noop, true, ErrGatewayNativeReplay
	}
	request.GetBody = nil
	request.Header.Del("Idempotency-Key")
	request.Header.Del("X-Idempotency-Key")
	return unlock, true, nil
}

// Native auth builders may use this exact transient view instead of invoking
// ordinary scheduler token caches, refresh writers or account-penalty paths.
func GatewayNativeConnectionAccountAuthorized(ctx context.Context, account *Account) bool {
	if ctx == nil || account == nil {
		return false
	}
	state, _ := ctx.Value(gatewayConnectionDispatchStateKey{}).(*gatewayConnectionDispatchState)
	if state == nil || state.account != account || ctx.Err() != nil || !GatewayConnectionScopeAuthorized(ctx, state.binding.in.Scope) || !GatewayConnectionSelectionAuthorized(ctx, &state.binding.in.Connection) {
		return false
	}
	canonical, err := json.Marshal(account.Credentials)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(canonical)
	clear(canonical)
	if digest != state.credentials || !reflect.DeepEqual(account.Extra, state.physical.Account.Extra) {
		return false
	}
	fresh, release, err := state.binding.service.repository.LockGatewayNativeConnection(ctx, state.binding.in.Scope, state.binding.in.Operation, &state.binding.native)
	if err != nil || release == nil {
		return false
	}
	defer release()
	return fresh.CredentialVersion == state.physical.CredentialVersion && reflect.DeepEqual(fresh.Account.Credentials, state.physical.Account.Credentials) && reflect.DeepEqual(fresh.Account.Extra, state.physical.Account.Extra)
}
