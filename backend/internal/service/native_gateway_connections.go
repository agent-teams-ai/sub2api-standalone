package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
)

// Connections composes native method ports onto the existing connect/refresh
// journals. It creates no second lifecycle store or provider operation graph.
type GatewayNativeConnections struct {
	journal     GatewayNativeOAuthConnectRepository
	repository  GatewayNativeConnectionRepository
	custody     *GatewayNativeCredentialCustody
	aead        cipher.AEAD
	macKey      []byte
	methods     map[GatewayConnectionSelection]GatewayNativeConnectionMethodAdapter
	connect     GatewayNativeOAuthEntryAuthorizer
	refresh     GatewayNativeOAuthEntryAuthorizer
	cleanup     GatewayNativeOAuthCleanupAuthorizer
	dispatchers map[string]GatewayNativeConnectionDispatchAdapter
	gateway     *OpenAIGatewayService
	codex       *GatewayNativeOAuthDispatch
}

type GatewayNativeConnectionsDependencies struct {
	Journal     GatewayNativeOAuthConnectRepository
	Repository  GatewayNativeConnectionRepository
	Custody     *GatewayNativeCredentialCustody
	MaterialKey []byte
	Methods     []GatewayNativeConnectionMethodAdapter
	Connect     GatewayNativeOAuthEntryAuthorizer
	Refresh     GatewayNativeOAuthEntryAuthorizer
	Cleanup     GatewayNativeOAuthCleanupAuthorizer
	Dispatchers map[string]GatewayNativeConnectionDispatchAdapter
	Gateway     *OpenAIGatewayService
	Codex       *GatewayNativeOAuthDispatch
}

func NewGatewayNativeConnections(d GatewayNativeConnectionsDependencies) (*GatewayNativeConnections, error) {
	if d.Journal == nil || d.Repository == nil || !GatewayNativeCredentialCustodyReady(d.Custody) || len(d.MaterialKey) != 32 || d.Connect == nil || d.Refresh == nil || d.Cleanup == nil {
		return nil, ErrGatewayNativeIdentity
	}
	block, err := aes.NewCipher(append([]byte(nil), d.MaterialKey...))
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	// The existing import key commitment and same-secret claim remain shared
	// with legacy Codex RT imports. A different MAC-key runtime cannot re-enter
	// unresolved material through another connection method.
	derive := hmac.New(sha256.New, d.MaterialKey)
	_, _ = derive.Write([]byte("account-gateway/native/oauth-import-mac-key/v1"))
	s := &GatewayNativeConnections{journal: d.Journal, repository: d.Repository, custody: d.Custody, aead: aead, macKey: derive.Sum(nil),
		methods: map[GatewayConnectionSelection]GatewayNativeConnectionMethodAdapter{}, connect: d.Connect, refresh: d.Refresh, cleanup: d.Cleanup, dispatchers: map[string]GatewayNativeConnectionDispatchAdapter{}, gateway: d.Gateway}
	s.codex = d.Codex
	if s.codex == nil {
		client, headers := newGatewayCodexAccountCheckClient(nil)
		s.codex = &GatewayNativeOAuthDispatch{client: client, accountCheckHeaders: headers, modelVersions: NewGatewayCodexVersionResolver(nil)}
	}
	for family, dispatch := range d.Dispatchers {
		if !gatewayConnectionSemanticID.MatchString(family) || dispatch.Forward == nil || dispatch.ValidateRequest == nil {
			return nil, ErrGatewayNativeIdentity
		}
		s.dispatchers[family] = dispatch
	}
	for _, method := range d.Methods {
		selection := method.Selection()
		profile, found := LookupGatewayConnectionProfile(method.ProfileID)
		if !found || !GatewayConnectionSelectionValid(selection) || !GatewayConnectionMountValid(method.Mount) || !GatewayNativeConnectionAccountTypeValid(method.AccountType) ||
			method.DispatchFamily != profile.DispatchFamily || method.Prepare == nil || method.Complete == nil || s.dispatchers[method.DispatchFamily].Forward == nil || s.dispatchers[method.DispatchFamily].ValidateRequest == nil {
			return nil, ErrGatewayNativeIdentity
		}
		if _, exists := s.methods[selection]; exists {
			return nil, ErrGatewayNativeIdentity
		}
		definition, err := method.definition()
		if err != nil {
			return nil, err
		}
		if method.Definition != nil {
			method.Definition = &definition
		}
		s.methods[selection] = method
	}
	return s, nil
}

func (s *GatewayNativeConnections) authorize(ctx context.Context, in GatewayNativeConnectionScope) (context.Context, error) {
	if s == nil {
		return nil, ErrGatewayNativeIdentity
	}
	return in.Authorize(ctx, GatewayOAuthBundlePurpose, in.Connection.ProfileID)
}

func (s *GatewayNativeConnections) mac(domain string, values any) string {
	raw, _ := json.Marshal(values)
	defer clear(raw)
	h := hmac.New(sha256.New, s.macKey)
	_, _ = h.Write([]byte(domain + "\x00"))
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

func (s *GatewayNativeConnections) keyCommitment() string {
	h := hmac.New(sha256.New, s.macKey)
	_, _ = h.Write([]byte("account-gateway/native/oauth-import-key/v1"))
	return hex.EncodeToString(h.Sum(nil))
}

func nativeConnectionMaterialAAD(in GatewayNativeOAuthConnectIntent) []byte {
	// PostgreSQL may return the same instant in the session's configured zone.
	// Ciphertext identity binds the instant, not its presentation offset.
	raw, _ := json.Marshal([]any{in.Scope, in.Operation, in.EnrollmentOperation, in.ClientID, in.Deadline.UTC(), in.StateHash,
		in.ImportInputMAC, in.ImportClaimMAC, in.ImportKeyCommitment, in.Connection, in.NativeMount, in.NativeDispatchFamily, in.NativePlatform, in.NativeAccountType})
	return append([]byte("account-gateway/native/connection-material/gcm1\x00"), raw...)
}

func (s *GatewayNativeConnections) sealMaterial(in GatewayNativeOAuthConnectIntent, material json.RawMessage) (string, error) {
	if _, err := gatewayOAuthJSON(material); err != nil || !GatewayNativeOAuthConnectIntentValid(in) || !GatewayNativeConnectionIntent(in) {
		return "", ErrGatewayNativeIdentity
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrGatewayNativeIdentity
	}
	sealed := s.aead.Seal(nonce, nonce, material, nativeConnectionMaterialAAD(in))
	return "gcm1." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *GatewayNativeConnections) openMaterial(in GatewayNativeOAuthConnectIntent) (json.RawMessage, error) {
	if !GatewayNativeOAuthConnectIntentValid(in) || !GatewayNativeConnectionIntent(in) || !strings.HasPrefix(in.Envelope, "gcm1.") || len(in.Envelope) > 96*1024 {
		return nil, ErrGatewayNativeIdentity
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(in.Envelope, "gcm1."))
	n := s.aead.NonceSize()
	if err != nil || len(sealed) < n+s.aead.Overhead() {
		return nil, ErrGatewayNativeIdentity
	}
	raw, err := s.aead.Open(nil, sealed[:n], sealed[n:], nativeConnectionMaterialAAD(in))
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	if _, err := gatewayOAuthJSON(raw); err != nil {
		clear(raw)
		return nil, ErrGatewayNativeIdentity
	}
	return raw, nil
}

func nativeConnectionView(in GatewayNativeOAuthConnectIntent) GatewayNativeConnectionResult {
	out := GatewayNativeConnectionResult{GatewayNativeOAuthConnectResult: GatewayNativeOAuthConnectResult{Connection: in.Connection, Operation: in.Operation, Account: in.Scope.Account, State: in.State}}
	if in.State == "completed" && in.NativePublication != nil {
		credential := in.NativePublication.Credential
		out.Outcome, out.Credential = "staged", &credential
	}
	return out
}

func (s *GatewayNativeConnections) readIntent(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeOAuthConnectIntent, error) {
	saved, err := s.journal.ReadConnectIntent(ctx, in.Scope, in.Operation)
	if err != nil || !GatewayNativeConnectionIntent(saved) || saved.Scope != in.Scope || saved.Operation != in.Operation || !SameGatewayConnectionSelection(saved.Connection, &in.Connection) {
		return GatewayNativeOAuthConnectIntent{}, ErrGatewayNativeIdentity
	}
	if !time.Now().Before(saved.Deadline) {
		state := ""
		switch saved.State {
		case "prepared":
			state = "expired"
		case "entered":
			state = "unknown"
		}
		if state != "" {
			saved, err = s.journal.FinishConnect(ctx, saved, state, GatewayNativeOAuthOutcome{})
		}
	}
	return saved, err
}

func (s *GatewayNativeConnections) Read(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeConnectionResult, error) {
	ctx, err := s.authorize(ctx, in)
	if err != nil {
		return GatewayNativeConnectionResult{}, err
	}
	saved, err := s.readIntent(ctx, in)
	return nativeConnectionView(saved), err
}

func (s *GatewayNativeConnections) Begin(ctx context.Context, in GatewayNativeConnectionScope, fields map[string]string) (GatewayNativeConnectionResult, error) {
	ctx, err := s.authorize(ctx, in)
	if err != nil {
		return GatewayNativeConnectionResult{}, err
	}
	method, found := s.methods[in.Connection]
	definition, definitionErr := method.definition()
	if !found || definitionErr != nil || gatewayConnectionDefinitionInput(definition, fields, false) != nil {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	prepared, err := method.Prepare(fields)
	if err != nil {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	defer clear(prepared.Material)
	defer clear(prepared.EntryClaim)
	if !GatewayCredentialAttestationValid(prepared.Credential) || !gatewayConnectionSemanticID.MatchString(prepared.AuthFamily) ||
		len(prepared.EntryClaim) > GatewayConnectionInputBytes || len(prepared.Material) > GatewayConnectionInputBytes ||
		(definition.Interaction.Kind == "credential_form" && len(prepared.EntryClaim) == 0) {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	canonical, err := CanonicalGatewayConnectionInput(in.Connection, fields)
	if err != nil {
		return GatewayNativeConnectionResult{}, err
	}
	defer clear(canonical)
	profile, found := LookupGatewayConnectionProfile(in.Connection.ProfileID)
	if !found {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	selection, mount := in.Connection, method.Mount
	intent := GatewayNativeOAuthConnectIntent{Scope: in.Scope, Operation: in.Operation, Connection: &selection,
		ClientID: GatewayNativeConnectionJournalClient, Method: GatewayNativeConnectionJournalMethod, State: "prepared",
		EnrollmentOperation: "connect-" + uuid.NewString(), Deadline: time.Now().UTC().Truncate(time.Microsecond).Add(GatewayOAuthConnectTTL - time.Second), StateHash: connectStateHash(hex.EncodeToString(random)),
		NativeMount: &mount, NativeDispatchFamily: method.DispatchFamily, NativePlatform: profile.Platform, NativeAccountType: method.AccountType,
		ImportInputMAC: s.mac("account-gateway/native/connection-input/v1", []any{in.Scope, in.Operation, string(canonical)}), ImportKeyCommitment: s.keyCommitment()}
	if len(prepared.EntryClaim) == 0 {
		intent.ImportClaimMAC = s.mac("account-gateway/native/connection-browser-claim/v1", []any{in.Scope, in.Operation, in.Connection})
	} else if prepared.AuthFamily == "codex_oauth" {
		intent.ImportClaimMAC = s.mac("account-gateway/native/oauth-import-claim/v1", []string{GatewayOAuthIssuer, openai.ClientID, string(prepared.EntryClaim)})
	} else {
		// Configuration, method and optional project/tier fields must not let
		// unresolved rotating material enter again under a different selection.
		// Those fields already belong to the independent operation input MAC.
		intent.ImportClaimMAC = s.mac("account-gateway/native/connection-claim/v1", []any{prepared.AuthFamily, string(prepared.EntryClaim)})
	}
	intent.Envelope, err = s.sealMaterial(intent, prepared.Material)
	if err != nil {
		return GatewayNativeConnectionResult{}, err
	}
	saved, err := s.journal.PrepareConnect(ctx, intent)
	if err != nil || !SameGatewayNativeOAuthConnectIntent(intent, saved) {
		return GatewayNativeConnectionResult{}, ErrGatewayOAuthConflict
	}
	if saved.State != "prepared" || !time.Now().Before(saved.Deadline) {
		return s.Read(ctx, in)
	}
	if definition.Interaction.Kind == "credential_form" {
		return s.complete(ctx, in, saved, method, nil)
	}
	result := nativeConnectionView(saved)
	if saved.StateHash == intent.StateHash {
		u, urlErr := url.Parse(prepared.AuthorizeURL)
		if urlErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || len(prepared.AuthorizeURL) > 8192 {
			return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
		}
		result.AuthorizeURL = prepared.AuthorizeURL
		result.Method = "browser"
	}
	return result, nil
}

func (s *GatewayNativeConnections) Submit(ctx context.Context, in GatewayNativeConnectionScope, fields map[string]string) (GatewayNativeConnectionResult, error) {
	ctx, err := s.authorize(ctx, in)
	if err != nil {
		return GatewayNativeConnectionResult{}, err
	}
	method, found := s.methods[in.Connection]
	definition, definitionErr := method.definition()
	if !found || definitionErr != nil || gatewayConnectionDefinitionInput(definition, fields, true) != nil {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	if definition.Interaction.Kind == "credential_form" {
		return s.Begin(ctx, in, fields)
	}
	saved, err := s.readIntent(ctx, in)
	if err != nil || saved.State != "prepared" {
		return nativeConnectionView(saved), err
	}
	return s.complete(ctx, in, saved, method, fields)
}

func (s *GatewayNativeConnections) complete(ctx context.Context, in GatewayNativeConnectionScope, saved GatewayNativeOAuthConnectIntent, method GatewayNativeConnectionMethodAdapter, fields map[string]string) (GatewayNativeConnectionResult, error) {
	material, err := s.openMaterial(saved)
	defer clear(material)
	if err != nil || s.connect(ctx, in.Scope, in.Operation) != nil {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	won, err := s.journal.EnterConnect(ctx, saved)
	if err != nil {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	if !won {
		return s.Read(ctx, in)
	}
	saved.State = "entered"
	// Native grants include bounded device polling (for example Grok SSO).
	// Preserve the caller and durable intent deadlines while permitting the
	// native grant's existing maximum instead of cutting it off mid-exchange.
	deadline := time.Now().Add(90 * time.Second)
	if saved.Deadline.Before(deadline) {
		deadline = saved.Deadline
	}
	bounded, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	credential, effectErr := method.Complete(bounded, in, material, fields)
	defer clear(credential.Document)
	if effectErr == nil && bounded.Err() == nil && time.Now().Before(saved.Deadline) && s.connect(bounded, in.Scope, in.Operation) == nil {
		publication := GatewayConnectionPublication{Connection: in.Connection, Credential: credential.Credential, DispatchFamily: saved.NativeDispatchFamily, AuthFamily: credential.AuthFamily}
		definition, definitionErr := gatewayConnectionDefinition(in)
		allowed := false
		for _, policy := range definition.AllowedCredentialPolicies {
			allowed = allowed || policy == credential.Credential.CredentialPolicy
		}
		if definitionErr == nil && allowed && GatewayConnectionPublicationValid(publication) &&
			(credential.Credential.RefreshPolicy != "native" || method.Refresh != nil && method.RefreshNeeded != nil) {
			envelope, sealErr := s.custody.SealConnection(in.Scope, publication, 1, credential.Document)
			if sealErr == nil {
				mac := s.mac("account-gateway/native/connection-enrollment/v1", []any{in.Scope, in.Operation, in.Connection, publication, string(credential.Document)})
				saved, err = s.journal.BindConnectEnrollment(bounded, saved, mac)
				if err == nil {
					_, err = s.repository.StageGatewayNativeConnection(bounded, GatewayNativeConnectionEnrollment{Intent: saved, Publication: publication, Envelope: envelope, Observations: credential.Observations})
					if err == nil {
						return s.Read(bounded, in)
					}
				}
			}
		}
	}
	// Unknown keeps the existing same-secret claim. Readback never invokes the
	// provider again, and a failed ACK may still find the committed exact birth.
	recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	current, readErr := s.journal.ReadConnectIntent(recovery, in.Scope, in.Operation)
	if readErr == nil && current.State == "completed" && SameGatewayConnectionSelection(current.Connection, &in.Connection) {
		return nativeConnectionView(current), nil
	}
	if readErr == nil && current.State == "entered" {
		current, readErr = s.journal.FinishConnect(recovery, current, "unknown", GatewayNativeOAuthOutcome{})
	}
	if readErr != nil {
		return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
	}
	return nativeConnectionView(current), nil
}

func (s *GatewayNativeConnections) Describe(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeConnectionDescriptorResult, error) {
	var out GatewayNativeConnectionDescriptorResult
	ctx, err := s.authorize(ctx, in)
	if err != nil {
		return out, err
	}
	saved, err := s.readIntent(ctx, in)
	if err != nil {
		return out, err
	}
	out = GatewayNativeConnectionDescriptorResult{Connection: in.Connection, Operation: in.Operation, Account: in.Scope.Account, State: saved.State, Qualification: "controlled-source-v1"}
	if saved.State != "completed" {
		return out, nil
	}
	p, release, err := s.repository.LockGatewayNativeConnection(ctx, in.Scope, in.Operation, nil)
	if err != nil || release == nil {
		return GatewayNativeConnectionDescriptorResult{}, ErrGatewayNativeIdentity
	}
	defer release()
	raw, err := AuthenticateGatewayNativeConnectionPhysical(ctx, s.custody, p)
	if err != nil {
		return GatewayNativeConnectionDescriptorResult{}, err
	}
	clear(raw)
	out.State, out.Native = "staged", &p.Native
	return out, nil
}

func (s *GatewayNativeConnections) Custody(ctx context.Context, in GatewayNativeConnectionScope, route GatewayNativePublishedRoute) (GatewayNativeConnectionCustodyResult, error) {
	var out GatewayNativeConnectionCustodyResult
	ctx, err := s.authorize(ctx, in)
	if err != nil || route.Publication.Connection != in.Connection {
		return out, ErrGatewayNativeIdentity
	}
	p, release, err := s.repository.LockGatewayNativeConnection(ctx, in.Scope, in.Operation, &route)
	if err != nil || release == nil {
		return out, ErrGatewayNativeIdentity
	}
	defer release()
	raw, err := AuthenticateGatewayNativeConnectionPhysical(ctx, s.custody, p)
	if err != nil {
		return out, err
	}
	clear(raw)
	return GatewayNativeConnectionCustodyResult{Contract: "native-retained-custody/v2", Generation: in.Scope.Generation, Native: p.Native,
		CredentialKinds: []string{p.Native.Publication.Credential.CredentialKind}, Retention: GatewayNativeRetainedCustodyState{State: "inactive", Credential: "retained"}, Proof: GatewayNativeRetainedCustodyProof{Authenticated: true}}, nil
}

func (s *GatewayNativeConnections) Cleanup(ctx context.Context, in GatewayNativeConnectionScope, request GatewayNativeOAuthCleanupRequest) (GatewayNativeConnectionCleanupResult, error) {
	var out GatewayNativeConnectionCleanupResult
	ctx, err := s.authorize(ctx, in)
	if err != nil || request.Scope != in.Scope || request.Operation != in.Operation {
		return out, ErrGatewayNativeIdentity
	}
	if _, err = s.readIntent(ctx, in); err != nil {
		return out, err
	}
	result, err := s.repository.CleanupGatewayNativeOAuth(ctx, request, s.cleanup)
	if err != nil {
		return out, err
	}
	out = GatewayNativeConnectionCleanupResult{Connection: in.Connection, Operation: result.Operation, Account: result.Account, CredentialsErased: result.CredentialsErased, Closure: result.Closure}
	if result.Native != nil {
		saved, err := s.journal.ReadConnectIntent(ctx, in.Scope, in.Operation)
		if err != nil || saved.NativePublication == nil || !SameGatewayConnectionSelection(saved.Connection, &in.Connection) {
			return GatewayNativeConnectionCleanupResult{}, ErrGatewayNativeIdentity
		}
		out.Native = &GatewayNativePublishedRoute{GatewayNativeRoute: *result.Native, Publication: *saved.NativePublication}
	}
	return out, nil
}

// RecoveryAdapter remains usable for stored operations after methods disappear
// from today's catalog. Only new Begin/Submit resolves a current exact binding.
func (s *GatewayNativeConnections) RecoveryAdapter(profile string) GatewayNativeConnectionAdapter {
	adapter := GatewayNativeConnectionAdapter{ProfileID: profile, Purpose: GatewayOAuthBundlePurpose,
		Begin: s.Begin, Read: s.Read, Submit: s.Submit, Describe: s.Describe, Refresh: s.Maintain, Custody: s.Custody, Cleanup: s.Cleanup,
		MethodDefinitions: map[string]GatewayConnectionMethodDefinition{}}
	for selection, method := range s.methods {
		if selection.ProfileID == profile {
			adapter.MethodIDs = append(adapter.MethodIDs, selection.MethodID)
			adapter.DispatchFamily = method.DispatchFamily
			if method.Definition != nil {
				adapter.MethodDefinitions[selection.MethodID] = *method.Definition
			}
		}
	}
	if definition, found := LookupGatewayConnectionProfile(profile); found {
		adapter.DispatchFamily = definition.DispatchFamily
		for _, method := range definition.Methods {
			adapter.RecoveryMethodIDs = append(adapter.RecoveryMethodIDs, method.MethodID)
		}
	}
	return adapter
}
