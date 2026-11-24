package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

const GatewayConnectionPublicationCredentialKey = "connection_publication"
const GatewayConnectionBundleCredentialKey = "connection_bundle"
const GatewayConnectionRevisionCredentialKey = "connection_revision"
const gatewayConnectionEnvelopePrefix = "gcb1"

// Publication is safe provenance, not a credential or permission. Its authority
// comes from exact comparison with the sealed credential and locked birth.
func GatewayConnectionPublicationValid(v GatewayConnectionPublication) bool {
	return GatewayConnectionSelectionValid(v.Connection) && GatewayCredentialAttestationValid(v.Credential) &&
		gatewayConnectionSemanticID.MatchString(v.DispatchFamily) && gatewayConnectionSemanticID.MatchString(v.AuthFamily)
}

func EncodeGatewayConnectionPublication(v GatewayConnectionPublication) ([]byte, error) {
	if !GatewayConnectionPublicationValid(v) {
		return nil, ErrGatewayNativeIdentity
	}
	return json.Marshal(v)
}

func DecodeGatewayConnectionPublication(raw []byte) (GatewayConnectionPublication, error) {
	var v GatewayConnectionPublication
	fields, err := gatewayOAuthJSON(raw)
	if err != nil || len(fields) != 4 {
		return v, ErrGatewayNativeIdentity
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || !GatewayConnectionPublicationValid(v) {
		return GatewayConnectionPublication{}, ErrGatewayNativeIdentity
	}
	return v, nil
}

func GatewayNativeStoredConnectionPublication(a *Account) (GatewayConnectionPublication, error) {
	if a == nil || a.Credentials == nil {
		return GatewayConnectionPublication{}, ErrGatewayNativeIdentity
	}
	value, exists := a.Credentials[GatewayConnectionPublicationCredentialKey]
	if !exists || value == nil {
		return GatewayConnectionPublication{}, ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return GatewayConnectionPublication{}, ErrGatewayNativeIdentity
	}
	return DecodeGatewayConnectionPublication(raw)
}

// GatewayConnectionPublicationCredentials is used only while constructing a
// protected row. JSON projection avoids storing Go pointers or mutable aliases.
func GatewayConnectionPublicationCredentials(v GatewayConnectionPublication) (map[string]any, error) {
	raw, err := EncodeGatewayConnectionPublication(v)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return nil, ErrGatewayNativeIdentity
	}
	return value, nil
}

// GatewayConnectionCredentialAAD preserves the complete native attestation as
// well as the immutable selection. A credential cannot be re-labelled as a key,
// refreshable session, signing identity, or another dispatch family in storage.
func GatewayConnectionCredentialAAD(scope GatewayNativeCredentialScope, publication GatewayConnectionPublication, revision int64) ([]byte, error) {
	if (!scope.valid() && !GatewayNativeOAuthScopeValid(scope)) || !GatewayConnectionPublicationValid(publication) || revision < 1 {
		return nil, ErrGatewayNativeIdentity
	}
	return json.Marshal(struct {
		Scope       GatewayNativeCredentialScope `json:"scope"`
		Publication GatewayConnectionPublication `json:"publication"`
		Revision    int64                        `json:"revision"`
	}{scope, publication, revision})
}

func (c *GatewayNativeCredentialCustody) SealConnection(scope GatewayNativeCredentialScope, publication GatewayConnectionPublication, revision int64, document json.RawMessage) (string, error) {
	if c == nil || c.keys[c.active] == nil {
		return "", ErrGatewayNativeIdentity
	}
	aad, err := GatewayConnectionCredentialAAD(scope, publication, revision)
	fields, documentErr := gatewayOAuthJSON(document)
	if err != nil || documentErr != nil || len(fields) == 0 || len(document) > GatewayConnectionInputBytes {
		return "", ErrGatewayNativeIdentity
	}
	aead := c.keys[c.active]
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", ErrGatewayNativeIdentity
	}
	aad = append([]byte("account-gateway/native/connection/gcb1/"+c.active+"\x00"), aad...)
	sealed := aead.Seal(nil, nonce, document, aad)
	return gatewayConnectionEnvelopePrefix + "." + c.active + "." + base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// OpenConnection returns transient native-only provider input. Callers own and
// clear it after use; the value never enters discovery, receipts or read DTOs.
func (c *GatewayNativeCredentialCustody) OpenConnection(scope GatewayNativeCredentialScope, publication GatewayConnectionPublication, revision int64, envelope string) (json.RawMessage, error) {
	aad, err := GatewayConnectionCredentialAAD(scope, publication, revision)
	parts := strings.Split(envelope, ".")
	if err != nil || c == nil || len(envelope) > 90*1024 || len(parts) != 4 || parts[0] != gatewayConnectionEnvelopePrefix || !gatewayCredentialKeyIDValid(parts[1]) || c.keys[parts[1]] == nil {
		return nil, ErrGatewayNativeIdentity
	}
	aead := c.keys[parts[1]]
	nonce, nerr := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	sealed, serr := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if nerr != nil || serr != nil || len(nonce) != aead.NonceSize() || len(sealed) < aead.Overhead() || base64.RawURLEncoding.EncodeToString(nonce) != parts[2] || base64.RawURLEncoding.EncodeToString(sealed) != parts[3] {
		return nil, ErrGatewayNativeIdentity
	}
	aad = append([]byte("account-gateway/native/connection/gcb1/"+parts[1]+"\x00"), aad...)
	raw, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	fields, err := gatewayOAuthJSON(raw)
	if err != nil || len(fields) == 0 {
		clear(raw)
		return nil, ErrGatewayNativeIdentity
	}
	return raw, nil
}

func GatewayNativeConnectionCredentialRevision(a *Account) (int64, error) {
	if a == nil {
		return 0, ErrGatewayNativeIdentity
	}
	raw, ok := a.Credentials[GatewayConnectionRevisionCredentialKey].(string)
	n, err := strconv.ParseInt(raw, 10, 64)
	if !ok || err != nil || n < 1 || strconv.FormatInt(n, 10) != raw {
		return 0, ErrGatewayNativeIdentity
	}
	return n, nil
}

func GatewayNativeConnectionStoredScope(a *Account) (GatewayNativeCredentialScope, error) {
	if a == nil || a.Extra[GatewayNativeConnectionAccountExtraKey] != true {
		return GatewayNativeCredentialScope{}, ErrGatewayNativeIdentity
	}
	return gatewayOAuthScopeForAccount(a)
}

// The repository proves owner, original operation, immutable physical birth and
// current credential version under one retained lock. Every new native read
// surface uses this port instead of inventing provider-specific account lookup.
type GatewayNativeConnectionPhysical struct {
	Account           *Account
	Scope             GatewayNativeCredentialScope
	Operation         string
	Native            GatewayNativePublishedRoute
	CredentialVersion int64
}

type GatewayNativeConnectionPhysicalRepository interface {
	LockGatewayNativeConnection(context.Context, GatewayNativeCredentialScope, string, *GatewayNativePublishedRoute) (GatewayNativeConnectionPhysical, func(), error)
}

// AuthenticateGatewayNativeConnectionPhysical authenticates the locked native
// bundle. Legacy key/OAuth readers retain their codecs and attest the same safe
// publication only after opening their actual custody, never from this helper.
func AuthenticateGatewayNativeConnectionPhysical(ctx context.Context, custody *GatewayNativeCredentialCustody, physical GatewayNativeConnectionPhysical) (json.RawMessage, error) {
	a := physical.Account
	if !GatewayConnectionScopeAuthorized(ctx, physical.Scope) || a == nil || physical.Operation == "" ||
		physical.Native.AccountID != a.ID || !physical.Native.CreatedAt.Equal(a.CreatedAt) || physical.Native.Generation != physical.Scope.Generation ||
		physical.Native.Profile != physical.Native.Publication.Connection.ProfileID || !GatewayConnectionSelectionAuthorized(ctx, &physical.Native.Publication.Connection) {
		return nil, ErrGatewayNativeIdentity
	}
	publication, err := GatewayNativeStoredConnectionPublication(a)
	revision, revisionErr := GatewayNativeConnectionCredentialRevision(a)
	if err != nil || revisionErr != nil || publication != physical.Native.Publication || revision != physical.CredentialVersion {
		return nil, ErrGatewayNativeIdentity
	}
	return custody.OpenConnection(physical.Scope, publication, revision, a.GetCredential(GatewayConnectionBundleCredentialKey))
}
