//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// These are test-peer instructions, never a production provider registry. The
// exact selection, method revision and configuration generation are discovered
// by RR through the real mounted capability endpoint, not supplied in a row.
// Fields and completion material are written only to the private 0600 fixture.
type nativeConnectionJoinedRow struct {
	ID                  string                             `json:"id"`
	ProfileID           string                             `json:"profileId"`
	MethodID            string                             `json:"methodId"`
	Variant             string                             `json:"variant"`
	Fields              map[string]string                  `json:"fields"`
	Completion          []nativeConnectionJoinedCompletion `json:"completion,omitempty"`
	ReconnectFields     map[string]string                  `json:"reconnectFields,omitempty"`
	ReconnectCompletion []nativeConnectionJoinedCompletion `json:"reconnectCompletion,omitempty"`
	ReconnectCustody    bool                               `json:"reconnectCustody,omitempty"`
	CredentialKind      string                             `json:"credentialKind"`
	CredentialPolicy    string                             `json:"credentialPolicy"`
	RefreshPolicy       string                             `json:"refreshPolicy"`
	Provider            string                             `json:"provider"`
	Model               string                             `json:"model"`
	ExecutionFamily     string                             `json:"executionFamily"`
	ReviewProof         *nativeConnectionJoinedReviewProof `json:"reviewProof,omitempty"`
	Execute             bool                               `json:"execute"`
	Blocker             string                             `json:"blocker,omitempty"`
}

type nativeConnectionJoinedReviewProof struct {
	ReasoningEffort string `json:"reasoningEffort"`
	MaxOutputTokens int    `json:"maxOutputTokens"`
}

type nativeConnectionJoinedCompletion struct {
	Action               string            `json:"action"`
	Fields               map[string]string `json:"fields,omitempty"`
	FromAuthorizationURL bool              `json:"fromAuthorizationURL,omitempty"`
}

// A peer owns provider authentication and metadata, while the common fixture
// owns protocol replies and observes actual RR tool/schema continuations. A
// peer handles only its fixed native endpoints; false delegates to inference.
// InferenceAuth must check the exact native endpoint and provider credential.
// It must never emit credential values in assertion diagnostics.
type nativeConnectionJoinedPeer struct {
	Row                 nativeConnectionJoinedRow
	Protocol            string
	ObserveAuthorizeURL func(string) (map[string]string, error)
	Handle              func(http.ResponseWriter, *http.Request) bool
	InferenceAuth       func(*http.Request) bool
	Select              func()
	Verify              func()
	// Safe source counters only. No provider bodies or credentials belong here.
	Counters func() map[string]int64
}

type nativeConnectionJoinedBuild struct {
	Context     context.Context
	DB          *sql.DB
	Repository  service.AccountRepository
	Journal     service.GatewayNativeOAuthConnectRepository
	Custody     *service.GatewayNativeCredentialCustody
	Gateway     *service.OpenAIGatewayService
	Config      *Config
	Owners      *nativeConnectionJoinedOwner
	Transport   *http.Transport
	OAuth       *privateOAuth
	MaterialKey []byte
}

type nativeConnectionJoinedFamily struct {
	Profiles []gatewaytransport.QualifiedProfile
	Peers    []*nativeConnectionJoinedPeer
	// Private contains bounded negative-test inputs, written only to the same
	// 0600 fixture and erased with it. They are not additional inventory rows.
	Private map[string]string
	// Hosts are exact TLS authorities (host:443), never consumer supplied URLs.
	Hosts []string
	// Compose uses production native factories and may mount legacy ports in
	// OAuth. All families share the same real journal, custody and authorities.
	Compose func(nativeConnectionJoinedBuild) ([]service.GatewayNativeConnectionMethodAdapter, map[string]service.GatewayNativeConnectionDispatchAdapter, error)
}

func nativeConnectionJoinedRowID(profile, method, variant string) string {
	return profile + "/" + method + "/" + variant
}
