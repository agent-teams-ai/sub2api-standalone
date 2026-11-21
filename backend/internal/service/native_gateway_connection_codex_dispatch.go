package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

func NewGatewayNativeCodexImportDispatcher(gateway *OpenAIGatewayService) (GatewayNativeConnectionDispatchAdapter, error) {
	if gateway == nil || gateway.httpUpstream == nil {
		return GatewayNativeConnectionDispatchAdapter{}, ErrGatewayNativeIdentity
	}
	return GatewayNativeConnectionDispatchAdapter{Forward: gateway.forwardGatewayNativeCodexImport, ValidateRequest: gatewayCodexImportRequestValid}, nil
}

func gatewayCodexImportLive(account *Account, now time.Time) bool {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return false
	}
	if expires := account.GetCredential("expires_at"); expires != "" {
		at, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil || !at.After(now) {
			return false
		}
	}
	return GatewayNativeCredentialRefValid(account.GetCredential("chatgpt_account_id"))
}

func gatewayCodexImportRequestHeaders(ctx context.Context, account *Account, request *http.Request) error {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || !gatewayCodexImportLive(account, time.Now()) {
		return ErrGatewayNativeIdentity
	}
	if account.IsOpenAIAgentIdentity() {
		key, err := agentIdentityKeyFromAccount(account)
		if err != nil {
			return ErrGatewayNativeIdentity
		}
		assertion, err := buildAgentAssertion(key, time.Now())
		if err != nil {
			return ErrGatewayNativeIdentity
		}
		request.Header.Set("Authorization", assertion)
	} else {
		access := account.GetCredential("access_token")
		if !gatewayImportTokenValid(access) {
			return ErrGatewayNativeIdentity
		}
		request.Header.Set("Authorization", "Bearer "+access)
	}
	request.Header.Set("ChatGPT-Account-Id", account.GetCredential("chatgpt_account_id"))
	request.Header.Set("originator", resolveCodexOutboundIdentity("").originator)
	enforceCodexIdentityHeaders(request.Header)
	return nil
}

func gatewayCodexImportRequestValid(request *http.Request, account *Account, native GatewayNativePublishedRoute, model string) bool {
	if request == nil || request.URL == nil || request.Method != http.MethodPost || request.URL.String() != GatewayCodexOAuthBaseURL+"/responses" ||
		native.Profile != GatewayCodexOAuthResponsesProfile || native.Publication.DispatchFamily != "codex_responses" || !GatewayAccountModelIDValid(model) ||
		!gatewayCodexImportLive(account, time.Now()) || request.Header.Get("Cookie") != "" || request.Header.Get("x-api-key") != "" ||
		request.Header.Get("ChatGPT-Account-Id") != account.GetCredential("chatgpt_account_id") {
		return false
	}
	credential, family := gatewayCodexImportAttestation(account.Credentials)
	if credential != native.Publication.Credential || family != native.Publication.AuthFamily {
		return false
	}
	if !account.IsOpenAIAgentIdentity() {
		return request.Header.Get("Authorization") == "Bearer "+account.GetCredential("access_token")
	}
	value := strings.TrimPrefix(request.Header.Get("Authorization"), "AgentAssertion ")
	if value == request.Header.Get("Authorization") || len(value) > 8192 {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return false
	}
	fields, err := gatewayOAuthJSON(raw)
	if err != nil || len(fields) != 4 {
		return false
	}
	var envelope map[string]string
	if json.Unmarshal(raw, &envelope) != nil || envelope["agent_runtime_id"] != account.GetCredential("agent_runtime_id") || envelope["task_id"] != account.GetCredential("task_id") {
		return false
	}
	instant, err := time.Parse(time.RFC3339, envelope["timestamp"])
	if err != nil || time.Since(instant) < -time.Second || time.Since(instant) > time.Minute {
		return false
	}
	key, err := agentIdentityKeyFromAccount(account)
	if err != nil {
		return false
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope["signature"])
	return err == nil && ed25519.Verify(key.privateKey.Public().(ed25519.PublicKey), []byte(key.runtimeID+":"+key.taskID+":"+envelope["timestamp"]), signature)
}

func (s *OpenAIGatewayService) forwardGatewayNativeCodexImport(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || c == nil || c.Request == nil || route.Profile != GatewayCodexOAuthResponsesProfile || !gatewayCodexImportLive(account, time.Now()) {
		return nil, ErrGatewayNativeIdentity
	}
	// The existing Codex backend does not accept the local admission cap.
	// Original request bytes/tokens remain bounded by the common native fence.
	providerBody, err := sjson.DeleteBytes(body, "max_output_tokens")
	if err != nil {
		return nil, ErrGatewayNativeIdentity
	}
	return s.forwardGatewayNativeResponses(ctx, c, account, providerBody)
}
