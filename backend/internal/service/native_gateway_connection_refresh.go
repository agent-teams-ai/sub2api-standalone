package service

import (
	"context"
	"time"
)

func (s *GatewayNativeConnections) Maintain(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeConnectionMaintenanceResult, error) {
	out := GatewayNativeConnectionMaintenanceResult{Connection: in.Connection, GatewayNativeOAuthMaintenanceOutcome: GatewayNativeOAuthMaintenanceOutcome{Operation: in.Operation, AccountRef: in.Scope.Account, State: "idle"}}
	ctx, err := s.authorize(ctx, in)
	if err != nil {
		return out, err
	}
	current, err := s.repository.ResolveGatewayNativeConnectionRefresh(ctx, in.Scope, in.Operation)
	if err != nil || current.Physical.Native.Publication.Connection != in.Connection {
		return out, ErrGatewayNativeIdentity
	}
	publication := current.Physical.Native.Publication
	if publication.Credential.RefreshPolicy == "none" {
		return out, nil
	}
	// Reconstruct the old attempt first. Neither a changed catalog nor a later
	// expiry can grant another entry for an already entered/unknown version.
	if current.Attempt != nil && current.State != "completed" && current.State != "prepared" {
		prepared, err := s.repository.PrepareGatewayNativeOAuthRefresh(ctx, *current.Attempt)
		if err != nil {
			return out, err
		}
		out.State, out.RefreshRef = prepared.Outcome.State, current.Attempt.Operation
		return out, nil
	}
	method, found := s.methods[in.Connection]
	if !found || method.Refresh == nil || method.RefreshNeeded == nil {
		// Old selection is still readable/erasable. A changed client or target
		// cannot be silently substituted for this credential's refresh protocol.
		return out, ErrGatewayNativeIdentity
	}
	raw, err := AuthenticateGatewayNativeConnectionPhysical(ctx, s.custody, current.Physical)
	if err != nil {
		return out, err
	}
	due, err := method.RefreshNeeded(raw, time.Now())
	clear(raw)
	if err != nil {
		return out, ErrGatewayNativeIdentity
	}
	if !due && (current.Attempt == nil || current.State == "completed") {
		return out, nil
	}
	attempt := gatewayOAuthRotation(in.Scope, current.Physical.Native.AccountID, current.Physical.Native.CreatedAt, current.Physical.CredentialVersion)
	attempt.ConnectOperation, attempt.NativePublication = in.Operation, &publication
	if current.Attempt != nil && current.State == "prepared" {
		attempt = *current.Attempt
	}
	prepared, err := s.repository.PrepareGatewayNativeOAuthRefresh(ctx, attempt)
	if err != nil {
		return out, err
	}
	out.State, out.RefreshRef = prepared.Outcome.State, attempt.Operation
	if !prepared.Claimed {
		return out, nil
	}
	previous, err := s.custody.OpenConnection(in.Scope, publication, attempt.ExpectedVersion, prepared.Envelope)
	defer clear(previous)
	if err != nil || s.refresh(ctx, in.Scope, in.Operation) != nil {
		return out, ErrGatewayNativeIdentity
	}
	infoOrder, err := s.reserveAccountInfoOrder(ctx, in.Connection.ProfileID)
	if err != nil {
		return out, err
	}
	won, err := s.repository.EnterGatewayNativeOAuthRefresh(ctx, attempt, prepared.Fence)
	if err != nil || !won {
		return out, err
	}
	unknown := func() (GatewayNativeConnectionMaintenanceResult, error) {
		recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		err := s.repository.UnknownGatewayNativeOAuthRefresh(recovery, attempt, prepared.Fence)
		out.State = "unknown"
		return out, err
	}
	bounded, cancel := context.WithDeadline(ctx, prepared.Deadline)
	defer cancel()
	credential, err := method.Refresh(bounded, in, previous)
	defer clear(credential.Document)
	completeCtx, stopComplete := context.WithTimeout(context.WithoutCancel(bounded), 2*time.Second)
	defer stopComplete()
	if err != nil || credential.Credential != publication.Credential || credential.AuthFamily != publication.AuthFamily || s.refresh(completeCtx, in.Scope, in.Operation) != nil {
		return unknown()
	}
	envelope, err := s.custody.SealConnection(in.Scope, publication, attempt.ExpectedVersion+1, credential.Document)
	if err != nil {
		return unknown()
	}
	attempt.nativeInfo = append([]GatewayAccountInfoObservation(nil), credential.Observations...)
	for i := range attempt.nativeInfo {
		if attempt.nativeInfo[i].Order == 0 {
			attempt.nativeInfo[i].Order = infoOrder
		}
	}
	result, err := s.repository.CompleteGatewayNativeOAuthRefresh(completeCtx, attempt, prepared.Fence, envelope)
	if err != nil {
		return unknown()
	}
	out.State = result.State
	return out, nil
}
