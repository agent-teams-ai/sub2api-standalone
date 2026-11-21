//go:build linux

package gatewaybootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const privateConnectionPrefix = "/private/native/v2/connections/"

// ServeHTTP is mounted behind the existing bounded management/cleanup lanes.
// It delegates every lifecycle operation to native ports and retains no state.
func (s *privateConnections) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	deny := func(status int) { http.Error(w, "native connection unavailable", status) }
	if s == nil || r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, privateConnectionPrefix) ||
		r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.Header.Get("Content-Type") != "application/json" ||
		r.Header.Get("Content-Encoding") != "" || r.ContentLength > service.GatewayNativeConnectionRequestBytes {
		deny(http.StatusBadRequest)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, privateConnectionPrefix)
	peer, err := s.authorize(r)
	role := "management"
	if action == "cleanup" {
		role = "cleanup"
	}
	if err != nil || peer.Role != role {
		deny(http.StatusForbidden)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, service.GatewayNativeConnectionRequestBytes+1))
	defer clear(raw)
	request, valid := service.DecodeGatewayNativeConnectionRequest(raw, action)
	if err != nil || !valid {
		deny(http.StatusBadRequest)
		return
	}
	defer clear(request.InputFields)
	adapter, exists := s.recovery[request.Connection.ProfileID][request.Connection.MethodID]
	if !exists {
		deny(http.StatusConflict)
		return
	}
	if action == "begin" || action == "submit" || action == "poll" {
		current, configured := s.bindings[request.Connection.ProfileID][request.Connection.MethodID]
		if !configured || current.selection != request.Connection {
			deny(http.StatusConflict)
			return
		}
	}
	bounded, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	scope := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{
		Consumer: peer.ConsumerID, Owner: request.Owner, Account: request.Account, Generation: request.Generation, Purpose: adapter.Purpose,
	}, Operation: request.Operation, Connection: request.Connection}
	cleanup := service.GatewayNativeOAuthCleanupRequest{Scope: scope.Scope, Operation: request.Operation,
		Action: request.Action, CleanupRef: request.CleanupRef, CleanupToken: request.CleanupToken}
	var owner string
	switch action {
	case "refresh":
		owner, err = s.owners.AuthorizeNativeOAuthRefreshOwner(bounded, peer.ConsumerID, request.Operation, request.Account, request.Generation)
	case "custody":
		authority, ok := s.owners.(NativeOAuthCustodyAuthority)
		if !ok {
			deny(http.StatusForbidden)
			return
		}
		owner, err = authority.AuthorizeNativeOAuthCustodyOwner(bounded, peer.ConsumerID, request.Operation, request.Account, request.Generation)
	case "cleanup":
		if s.cleanup == nil {
			deny(http.StatusForbidden)
			return
		}
		var grant service.GatewayNativeOAuthCleanupAuthority
		grant, err = s.cleanup(bounded, cleanup)
		owner = grant.Owner
	default:
		owner, err = s.owners.AuthorizeNativeOAuthOwner(bounded, peer.ConsumerID, request.Operation, request.Account, request.Generation)
	}
	if err != nil || bounded.Err() != nil || owner != request.Owner {
		deny(http.StatusForbidden)
		return
	}
	ctx, err := service.WithGatewayNativeConsumer(bounded, peer.ConsumerID)
	if err == nil {
		ctx, err = service.WithGatewayNativeOAuthOwner(ctx, owner)
	}
	if err != nil {
		deny(http.StatusForbidden)
		return
	}
	ctx = service.WithGatewayNativeCustody(ctx, s.custody)
	var result any
	switch action {
	case "begin":
		result, err = adapter.Begin(ctx, scope, request.InputFields)
	case "read":
		result, err = adapter.Read(ctx, scope)
	case "submit":
		result, err = adapter.Submit(ctx, scope, request.InputFields)
	case "poll":
		if adapter.Poll == nil {
			deny(http.StatusConflict)
			return
		}
		result, err = adapter.Poll(ctx, scope)
	case "describe":
		result, err = adapter.Describe(ctx, scope)
	case "refresh":
		result, err = adapter.Refresh(ctx, scope)
	case "custody":
		result, err = adapter.Custody(ctx, scope, *request.Descriptor)
	case "cleanup":
		result, err = adapter.Cleanup(ctx, scope, cleanup)
	default:
		deny(http.StatusNotFound)
		return
	}
	if err != nil || ctx.Err() != nil {
		deny(http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
