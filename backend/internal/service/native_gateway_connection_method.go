package service

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Prepared contains transient local parsing/PKCE material. Its attestation is
// only a routing proposal; discovery and durable completion never publish it as
// a credential fact. Prepare must not make a provider request or mutate a row.
type GatewayNativeConnectionPrepared struct {
	Material json.RawMessage
	// EntryClaim is a canonical secret identity for rotating/imported material,
	// used only by the existing keyed same-secret entry fence. It is never a
	// provider principal, observable fingerprint, or stored plaintext value.
	EntryClaim   []byte
	Credential   GatewayCredentialAttestation
	AuthFamily   string
	AuthorizeURL string
}

// Credential is produced by the actual native method after durable effect
// entry. Document is the existing provider's credential representation and is
// sealed immediately; no provider-specific grammar crosses the native boundary.
type GatewayNativeConnectionCredential struct {
	Document     json.RawMessage
	Credential   GatewayCredentialAttestation
	AuthFamily   string
	Observations []GatewayAccountInfoObservation
}

// A method adapter is a finite native implementation, not an operation graph.
// Configuration and mount are trusted composition. Browser Complete must use
// its sealed original client/redirect/PKCE, or deny continuation after change.
// Key Complete performs bounded native syntax/qualification only; it must not
// introduce a paid probe merely to populate a credential metadata field.
type GatewayNativeConnectionMethodAdapter struct {
	ProfileID               string
	MethodID                string
	MethodRevision          int
	ConfigurationGeneration string
	DispatchFamily          string
	AccountType             string
	Mount                   GatewayConnectionMount
	Prepare                 func(map[string]string) (GatewayNativeConnectionPrepared, error)
	Complete                func(context.Context, GatewayNativeConnectionScope, json.RawMessage, map[string]string) (GatewayNativeConnectionCredential, error)
	RefreshNeeded           func(json.RawMessage, time.Time) (bool, error)
	Refresh                 func(context.Context, GatewayNativeConnectionScope, json.RawMessage) (GatewayNativeConnectionCredential, error)
}

func (a GatewayNativeConnectionMethodAdapter) Selection() GatewayConnectionSelection {
	return GatewayConnectionSelection{ProfileID: a.ProfileID, MethodID: a.MethodID, MethodRevision: a.MethodRevision, ConfigurationGeneration: a.ConfigurationGeneration}
}

// Dispatch receives a transient, already decrypted Account only while the
// original native physical lock and admission claim are held. Implementations
// reuse the native provider service; they cannot change scope or select a row.
type GatewayNativeConnectionDispatchFunc func(context.Context, *gin.Context, *Account, GatewayNativeRoute, []byte) (*OpenAIForwardResult, error)

type GatewayNativeConnectionDispatchAdapter struct {
	Forward         GatewayNativeConnectionDispatchFunc
	ValidateRequest func(*http.Request, *Account, GatewayNativePublishedRoute, string) bool
}
