package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// The native mobile importer uses this fixed client. Input never selects an
// issuer, audience, token endpoint or a different OAuth application.
const gatewayCodexMobileClientID = "app_LlGpXReQgckcGGUo2JrYvtJK"

var gatewayCodexIdentityID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

func gatewayCodexImportDocument(method string, input map[string]string) (map[string]any, GatewayCredentialAttestation, string, []byte, error) {
	deny := func() (map[string]any, GatewayCredentialAttestation, string, []byte, error) {
		return nil, GatewayCredentialAttestation{}, "", nil, ErrGatewayNativeIdentity
	}
	credential := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "none"}
	family := "codex_access_token"
	document := make(map[string]any)
	var claim []byte
	switch method {
	case "mobile_refresh_token":
		refresh := input["refresh_token"]
		if !gatewayImportTokenValid(refresh) {
			return deny()
		}
		document["refresh_token"], document["client_id"] = refresh, gatewayCodexMobileClientID
		credential.RefreshPolicy, family = "native", "codex_oauth"
		claim = []byte(refresh)
	case "codex_pat":
		access := input["access_token"]
		if !gatewayImportTokenValid(access) || !strings.HasPrefix(access, "at-") {
			return deny()
		}
		document["access_token"], document["auth_mode"] = access, OpenAIAuthModePersonalAccessToken
		credential.CredentialKind, family, claim = "native", "codex_pat", []byte(access)
	case "codex_session", "agent_identity":
		content := strings.TrimSpace(input["content"])
		if content == "" || len(content) > GatewayConnectionInputBytes {
			return deny()
		}
		var value any = content
		if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") || strings.HasPrefix(content, `"`) {
			// Validate duplicates/depth/one complete JSON document before selecting
			// the one record accepted by this owner-scoped operation.
			wrapped := append([]byte(`{"entry":`), []byte(content)...)
			wrapped = append(wrapped, '}')
			if _, err := gatewayOAuthJSON(wrapped); err != nil {
				return deny()
			}
			d := json.NewDecoder(strings.NewReader(content))
			d.UseNumber()
			if d.Decode(&value) != nil {
				return deny()
			}
			for depth := 0; depth < 16; depth++ {
				array, ok := value.([]any)
				if !ok {
					break
				}
				if len(array) != 1 {
					return deny()
				}
				value = array[0]
			}
		}
		parsed, err := ParseCodexSessionCredentialInput(value)
		if err != nil || method == "agent_identity" && !parsed.IsAgentIdentity {
			return deny()
		}
		if parsed.IsAgentIdentity {
			if !gatewayCodexIdentityID.MatchString(parsed.AgentRuntimeID) || !gatewayCodexIdentityID.MatchString(parsed.AccountID) || !gatewayCodexIdentityID.MatchString(parsed.UserID) ||
				parsed.AgentTaskID != "" && !gatewayCodexIdentityID.MatchString(parsed.AgentTaskID) {
				return deny()
			}
			document["auth_mode"], document["agent_runtime_id"], document["agent_private_key"] = OpenAIAuthModeAgentIdentity, parsed.AgentRuntimeID, parsed.AgentPrivateKey
			document["chatgpt_account_id"], document["chatgpt_user_id"] = parsed.AccountID, parsed.UserID
			document["chatgpt_account_is_fedramp"] = parsed.AgentFedRAMP
			// A supplied task ID is not trusted as an active provider task. Native
			// registers the runtime once, inside the already entered operation.
			credential = GatewayCredentialAttestation{CredentialKind: "native", CredentialPolicy: "delegated_identity", RefreshPolicy: "native"}
			key, err := agentIdentityPrivateKey(&Account{Credentials: map[string]any{"agent_private_key": parsed.AgentPrivateKey}})
			if err != nil {
				return deny()
			}
			// Equivalent PKCS#8/base64 spellings are the same secret. The
			// one-entry MAC must not change when the import adds line breaks.
			family, claim = "codex_agent_identity", key.Seed()
			clear(key)
		} else {
			if !gatewayImportTokenValid(parsed.AccessToken) || strings.HasPrefix(parsed.AccessToken, "at-") || parsed.RefreshToken != "" && !gatewayImportTokenValid(parsed.RefreshToken) {
				return deny()
			}
			document["access_token"], claim = parsed.AccessToken, []byte(parsed.AccessToken)
			var expiry time.Time
			if parsed.TokenExpiresAt != nil {
				expiry = *parsed.TokenExpiresAt
				if !expiry.After(time.Now()) {
					return deny()
				}
			}
			if claims, err := openai.DecodeIDToken(parsed.AccessToken); err == nil && claims.Exp > 0 {
				jwtExpiry := time.Unix(claims.Exp, 0).UTC()
				if expiry.IsZero() || jwtExpiry.Before(expiry) {
					expiry = jwtExpiry
				}
			}
			if !expiry.IsZero() {
				if !expiry.After(time.Now()) || expiry.Year() < 1 || expiry.Year() > 9999 {
					return deny()
				}
				// An unverified expiry is only a local denial bound, never display
				// metadata or an authenticated provider identity.
				document["expires_at"] = expiry.UTC().Format(time.RFC3339Nano)
			}
			if parsed.RefreshToken != "" {
				document["refresh_token"], document["client_id"] = parsed.RefreshToken, openai.ClientID
				credential.RefreshPolicy, family, claim = "native", "codex_oauth", []byte(parsed.RefreshToken)
			}
		}
	default:
		return deny()
	}
	return document, credential, family, claim, nil
}

func gatewayCodexImportDecode(raw json.RawMessage) (map[string]any, error) {
	document, _, err := gatewayConnectionDocument(raw)
	return document, err
}

func gatewayCodexImportAttestation(document map[string]any) (GatewayCredentialAttestation, string) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: document}
	if account.IsOpenAIAgentIdentity() {
		return GatewayCredentialAttestation{"native", "delegated_identity", "native"}, "codex_agent_identity"
	}
	if account.IsOpenAIPersonalAccessToken() {
		return GatewayCredentialAttestation{"native", "subscription_session", "none"}, "codex_pat"
	}
	if account.GetCredential("refresh_token") != "" {
		return GatewayCredentialAttestation{"oauth", "subscription_session", "native"}, "codex_oauth"
	}
	return GatewayCredentialAttestation{"oauth", "subscription_session", "none"}, "codex_access_token"
}

// NewGatewayNativeCodexImportMethods preserves single-record native grammars
// while routing custody, reconnect and refresh through the existing journal.
func NewGatewayNativeCodexImportMethods(generation string, mount GatewayConnectionMount, transport http.RoundTripper) ([]GatewayNativeConnectionMethodAdapter, error) {
	if !GatewayConnectionMountValid(mount) || mount.BaseURL != GatewayCodexOAuthBaseURL || mount.Model != GatewayCodexOAuthModel {
		return nil, ErrGatewayNativeIdentity
	}
	profile, _ := LookupGatewayConnectionProfile(GatewayCodexOAuthResponsesProfile)
	client, headers := newGatewayCodexAccountCheckClient(transport)
	verifier := NewGatewayNativeOAuthVerifier(transport)
	check := &GatewayNativeOAuthDispatch{client: client, accountCheckHeaders: headers}
	exchange := &GatewayNativeOAuthConnect{client: client, now: time.Now}
	var methods []GatewayNativeConnectionMethodAdapter
	for _, definition := range profile.Methods {
		switch definition.MethodID {
		case "mobile_refresh_token", "codex_session", "agent_identity", "codex_pat":
		default:
			continue
		}
		selection := GatewayConnectionSelection{GatewayCodexOAuthResponsesProfile, definition.MethodID, definition.Revision, generation}
		if !GatewayConnectionSelectionValid(selection) {
			return nil, ErrGatewayNativeIdentity
		}
		finish := func(ctx context.Context, in GatewayNativeConnectionScope, document map[string]any) (GatewayNativeConnectionCredential, error) {
			var empty GatewayNativeConnectionCredential
			if ctx == nil || ctx.Err() != nil || in.Connection != selection || !GatewayConnectionScopeAuthorized(ctx, in.Scope) || !GatewayConnectionSelectionAuthorized(ctx, &in.Connection) {
				return empty, ErrGatewayNativeIdentity
			}
			credential, family := gatewayCodexImportAttestation(document)
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: document}
			var observations []GatewayAccountInfoObservation
			switch family {
			case "codex_oauth":
				clientID := account.GetCredential("client_id")
				if clientID != openai.ClientID && clientID != gatewayCodexMobileClientID {
					return empty, ErrGatewayNativeIdentity
				}
				form := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {account.GetCredential("refresh_token")}, "scope": {openai.RefreshScopes}}
				bundle, err := exchange.exchangeConnectToken(ctx, form, account.GetCredential("refresh_token"))
				if err != nil {
					return empty, ErrGatewayNativeIdentity
				}
				identity, err := verifier.verifyAudience(ctx, bundle.IDToken, clientID)
				issuer, subject := identity.Principal()
				if err != nil || account.GetCredential("subject") != "" && (account.GetCredential("subject") != subject || account.GetCredential("issuer") != issuer) {
					return empty, ErrGatewayNativeIdentity
				}
				fields, err := gatewayOAuthJSON(bundle.SensitiveMetadata)
				seconds, valid := gatewayOAuthSeconds(fields["expires_in"])
				if err != nil || !valid || seconds > int64((365*24*time.Hour)/time.Second) {
					return empty, ErrGatewayNativeIdentity
				}
				expiry := bundle.requestStarted.Add(time.Duration(seconds) * time.Second)
				if !expiry.After(time.Now()) {
					return empty, ErrGatewayNativeIdentity
				}
				document["access_token"], document["refresh_token"], document["id_token"] = bundle.AccessToken, bundle.RefreshToken, bundle.IDToken
				document["issuer"], document["subject"], document["expires_at"] = issuer, subject, expiry.UTC().Format(time.RFC3339Nano)
				observations = append(observations, GatewayAccountInfoObservation{Source: "verified_claims", ValidUntil: &identity.expiresAt, Fields: identity.info}, GatewayAccountInfoObservation{Source: "credential", Fields: gatewayOAuthCredentialExpiryInfo(GatewayCodexOAuthResponsesProfile, expiry, bundle.requestStarted)})
				fallthrough
			case "codex_access_token":
				provider, fields, err := check.accountCheckInfo(ctx, account.GetCredential("access_token"))
				if err != nil || account.GetCredential("chatgpt_account_id") != "" && account.GetCredential("chatgpt_account_id") != provider {
					return empty, ErrGatewayNativeIdentity
				}
				document["chatgpt_account_id"] = provider
				observations = append(observations, GatewayAccountInfoObservation{Source: "account_check", Fields: fields})
				if training := gatewayCodexTrainingInfo(ctx, client, headers, account.GetCredential("access_token")); training != nil {
					observations = append(observations, GatewayAccountInfoObservation{Source: "training_settings", Fields: *training})
				}
			case "codex_pat":
				info, err := validateOpenAICodexPersonalAccessToken(ctx, account.GetCredential("access_token"), client)
				if err != nil {
					return empty, ErrGatewayNativeIdentity
				}
				document = (&OpenAIOAuthService{}).BuildAccountCredentials(info)
				defer clear(document)
				fields := GatewayAccountInfoEmptyFields(GatewayCodexOAuthResponsesProfile)
				at := time.Now().UTC()
				if gatewayInfoText(info.Email, 320) {
					fields.Email = gatewayKnownFact(info.Email, "provider_api", at)
				}
				if subscription := gatewayInfoSubscription(info.PlanType, "unknown", ""); subscription != nil {
					fields.Subscription = gatewayKnownFact(*subscription, "provider_api", at)
				}
				observations = append(observations, GatewayAccountInfoObservation{Source: "account_check", Fields: fields})
			case "codex_agent_identity":
				task, err := registerAgentIdentityTaskWithClient(ctx, account, client)
				if err != nil || !gatewayCodexIdentityID.MatchString(task) {
					return empty, ErrGatewayNativeIdentity
				}
				document["task_id"] = task
				// Task registration authenticates the runtime, not imported email,
				// plan or subscription claims. Those stay unknown.
			default:
				return empty, ErrGatewayNativeIdentity
			}
			raw, err := json.Marshal(document)
			if err != nil || len(raw) > GatewayConnectionInputBytes {
				return empty, ErrGatewayNativeIdentity
			}
			return GatewayNativeConnectionCredential{Document: raw, Credential: credential, AuthFamily: family, Observations: observations}, nil
		}
		methods = append(methods, GatewayNativeConnectionMethodAdapter{
			ProfileID: selection.ProfileID, MethodID: selection.MethodID, MethodRevision: selection.MethodRevision, ConfigurationGeneration: selection.ConfigurationGeneration,
			DispatchFamily: "codex_responses", AccountType: AccountTypeOAuth, Mount: mount,
			Prepare: func(input map[string]string) (GatewayNativeConnectionPrepared, error) {
				if ValidateGatewayConnectionInput(definition.Interaction.InputFields, input) != nil {
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				document, credential, family, claim, err := gatewayCodexImportDocument(definition.MethodID, input)
				if err != nil {
					return GatewayNativeConnectionPrepared{}, err
				}
				defer clear(document)
				raw, err := json.Marshal(document)
				if err != nil {
					clear(claim)
					return GatewayNativeConnectionPrepared{}, ErrGatewayNativeIdentity
				}
				return GatewayNativeConnectionPrepared{Material: raw, EntryClaim: claim, Credential: credential, AuthFamily: family}, nil
			},
			Complete: func(ctx context.Context, in GatewayNativeConnectionScope, material json.RawMessage, completion map[string]string) (GatewayNativeConnectionCredential, error) {
				if len(completion) != 0 {
					return GatewayNativeConnectionCredential{}, ErrGatewayNativeIdentity
				}
				document, err := gatewayCodexImportDecode(material)
				if err != nil {
					return GatewayNativeConnectionCredential{}, err
				}
				defer clear(document)
				return finish(ctx, in, document)
			},
			RefreshNeeded: func(raw json.RawMessage, now time.Time) (bool, error) {
				document, err := gatewayCodexImportDecode(raw)
				if err != nil {
					return false, err
				}
				defer clear(document)
				credential, family := gatewayCodexImportAttestation(document)
				account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: document}
				if credential.RefreshPolicy == "none" {
					return false, nil
				}
				if family == "codex_agent_identity" {
					return account.GetCredential("task_id") == "", nil
				}
				expiry, err := time.Parse(time.RFC3339Nano, account.GetCredential("expires_at"))
				return err == nil && !expiry.After(now.Add(time.Minute)), err
			},
			Refresh: func(ctx context.Context, in GatewayNativeConnectionScope, raw json.RawMessage) (GatewayNativeConnectionCredential, error) {
				document, err := gatewayCodexImportDecode(raw)
				if err != nil {
					return GatewayNativeConnectionCredential{}, err
				}
				defer clear(document)
				return finish(ctx, in, document)
			},
		})
	}
	if len(methods) != 4 {
		return nil, ErrGatewayNativeIdentity
	}
	return methods, nil
}
