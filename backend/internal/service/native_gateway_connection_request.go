package service

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"github.com/google/uuid"
)

const GatewayNativeConnectionRequestBytes = 80 * 1024

// The private request carries selectors and one transient input envelope. It
// cannot choose a provider endpoint, dispatch family, attestation or owner grant.
type GatewayNativeConnectionRequest struct {
	Operation    string                       `json:"operation"`
	Owner        string                       `json:"owner_ref"`
	Account      string                       `json:"account_ref"`
	Generation   string                       `json:"generation"`
	Connection   GatewayConnectionSelection   `json:"connection"`
	InputFields  map[string]string            `json:"input_fields,omitempty"`
	Descriptor   *GatewayNativePublishedRoute `json:"descriptor,omitempty"`
	Action       string                       `json:"action,omitempty"`
	CleanupRef   string                       `json:"cleanup_ref,omitempty"`
	CleanupToken string                       `json:"cleanup_token,omitempty"`
}

// DecodeGatewayNativeConnectionRequest rejects ambiguous JSON before the
// method's own finite field validator. All secret-bearing bytes remain owned by
// the request handler; this DTO is never a response, journal or log value.
func DecodeGatewayNativeConnectionRequest(raw []byte, action string) (GatewayNativeConnectionRequest, bool) {
	var request GatewayNativeConnectionRequest
	if len(raw) == 0 || len(raw) > GatewayNativeConnectionRequestBytes {
		return request, false
	}
	fields, err := gatewayOAuthJSON(raw)
	if err != nil {
		return request, false
	}
	required := []string{"operation", "owner_ref", "account_ref", "generation", "connection"}
	switch action {
	case "begin", "submit":
		required = append(required, "input_fields")
	case "read", "poll", "describe", "refresh":
	case "custody":
		required = append(required, "descriptor")
	case "cleanup":
		required = append(required, "action", "cleanup_ref", "cleanup_token")
	default:
		return request, false
	}
	if len(fields) != len(required) {
		return request, false
	}
	for _, name := range required {
		if fields[name] == nil || bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return request, false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || !GatewayNativeCredentialRefValid(request.Operation) ||
		!GatewayNativeCredentialRefValid(request.Owner) || !GatewayNativeCredentialRefValid(request.Account) ||
		!GatewayConnectionSelectionValid(request.Connection) {
		return GatewayNativeConnectionRequest{}, false
	}
	generation, generationErr := uuid.Parse(request.Generation)
	selection, selectionErr := DecodeGatewayConnectionSelection(fields["connection"])
	if generationErr != nil || generation == uuid.Nil || generation.String() != request.Generation || selectionErr != nil || selection == nil || *selection != request.Connection {
		return GatewayNativeConnectionRequest{}, false
	}
	if len(request.InputFields) > GatewayConnectionInputFields {
		return GatewayNativeConnectionRequest{}, false
	}
	total := 0
	for key, value := range request.InputFields {
		total += len(key) + len(value)
		if !gatewayConnectionSemanticID.MatchString(key) || !utf8.ValidString(value) || total > GatewayConnectionInputBytes {
			return GatewayNativeConnectionRequest{}, false
		}
	}
	if action == "cleanup" && (request.Action != "read" && request.Action != "erase" ||
		!GatewayNativeCredentialRefValid(request.CleanupRef) || !GatewayNativeCredentialRefValid(request.CleanupToken)) {
		return GatewayNativeConnectionRequest{}, false
	}
	if request.Descriptor != nil && (request.Descriptor.Generation != request.Generation ||
		request.Descriptor.Profile != request.Connection.ProfileID || request.Descriptor.Publication.Connection != request.Connection ||
		!GatewayConnectionPublicationValid(request.Descriptor.Publication)) {
		return GatewayNativeConnectionRequest{}, false
	}
	return request, true
}
