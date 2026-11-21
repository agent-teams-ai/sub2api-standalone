package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// GatewayConnectionPublication is safe immutable native identity evidence.
// Family IDs are compared, never used by consumers to guess provider behavior.
type GatewayConnectionPublication struct {
	Connection     GatewayConnectionSelection   `json:"connection"`
	Credential     GatewayCredentialAttestation `json:"credential"`
	DispatchFamily string                       `json:"dispatchFamily"`
	AuthFamily     string                       `json:"authFamily"`
}

type gatewayConnectionSelectionContextKey struct{}

func GatewayConnectionScopeAuthorized(ctx context.Context, scope GatewayNativeCredentialScope) bool {
	if ctx == nil || (!scope.valid() && !GatewayNativeOAuthScopeValid(scope)) {
		return false
	}
	consumer, err := GatewayNativeConsumer(ctx)
	owner, _ := ctx.Value(gatewayOAuthOwnerKey{}).(string)
	return err == nil && consumer == scope.Consumer && owner == scope.Owner
}

// WithGatewayConnectionSelection is called by qualified native composition,
// after owner authorization. It does not grant authority or replace live fences.
func WithGatewayConnectionSelection(ctx context.Context, selection GatewayConnectionSelection) (context.Context, error) {
	if ctx == nil || !GatewayConnectionSelectionValid(selection) {
		return nil, ErrGatewayNativeIdentity
	}
	return context.WithValue(ctx, gatewayConnectionSelectionContextKey{}, selection), nil
}

func gatewayConnectionSelectionFromContext(ctx context.Context) *GatewayConnectionSelection {
	if selection, ok := ctx.Value(gatewayConnectionSelectionContextKey{}).(GatewayConnectionSelection); ok {
		return &selection
	}
	return nil
}

func SameGatewayConnectionSelection(a, b *GatewayConnectionSelection) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func GatewayConnectionSelectionAuthorized(ctx context.Context, stored *GatewayConnectionSelection) bool {
	return SameGatewayConnectionSelection(gatewayConnectionSelectionFromContext(ctx), stored)
}

func gatewayConnectionMatchesContext(ctx context.Context, profile, method string) bool {
	selection := gatewayConnectionSelectionFromContext(ctx)
	return selection == nil || GatewayConnectionSelectionValid(*selection) && selection.ProfileID == profile && selection.MethodID == method
}

// Stored bindings have one strict decoder. Empty is an explicit legacy record;
// malformed new data never falls back to an unbound old operation.
func DecodeGatewayConnectionSelection(raw []byte) (*GatewayConnectionSelection, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > 1024 {
		return nil, ErrGatewayNativeIdentity
	}
	var selection GatewayConnectionSelection
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&selection) != nil || !GatewayConnectionSelectionValid(selection) {
		return nil, ErrGatewayNativeIdentity
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrGatewayNativeIdentity
	}
	return &selection, nil
}

func EncodeGatewayConnectionSelection(selection *GatewayConnectionSelection) (string, error) {
	if selection == nil {
		return "", nil
	}
	if !GatewayConnectionSelectionValid(*selection) {
		return "", ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(selection)
	if err != nil {
		return "", ErrGatewayNativeIdentity
	}
	return string(raw), nil
}

// Existing legacy AAD remains byte-for-byte identical. New intents bind their
// immutable method/configuration as an additional framed domain. This helper
// does not create another key or cryptographic storage mechanism.
func gatewayConnectionBoundBytes(legacy []byte, selection *GatewayConnectionSelection) []byte {
	if selection == nil {
		return legacy
	}
	raw, _ := json.Marshal(selection)
	out := append(append([]byte(nil), legacy...), []byte("\x00native-connection-binding/v1\x00")...)
	return append(out, raw...)
}
