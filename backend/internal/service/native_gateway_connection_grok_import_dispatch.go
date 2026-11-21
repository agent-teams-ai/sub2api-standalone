package service

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
)

// Grok API keys and subscription OAuth share a native protocol family. Dispatch
// retains their distinct credential and account types through the final fence.
func NewGatewayNativeGrokImportDispatcher(gateway *OpenAIGatewayService, apiKey GatewayNativeConnectionDispatchAdapter) (GatewayNativeConnectionDispatchAdapter, error) {
	if gateway == nil || gateway.httpUpstream == nil || apiKey.Forward == nil || apiKey.ValidateRequest == nil {
		return GatewayNativeConnectionDispatchAdapter{}, ErrGatewayNativeIdentity
	}
	return GatewayNativeConnectionDispatchAdapter{
		Forward: func(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
			if account != nil && account.Type == AccountTypeAPIKey {
				return apiKey.Forward(ctx, c, account, route, body)
			}
			return gateway.forwardGatewayNativeGrokImport(ctx, c, account, route, body)
		},
		ValidateRequest: func(request *http.Request, account *Account, native GatewayNativePublishedRoute, model string) bool {
			if account != nil && account.Type == AccountTypeAPIKey {
				return apiKey.ValidateRequest(request, account, native, model)
			}
			return gatewayGrokImportRequestValid(request, account, native, model)
		},
	}, nil
}

func gatewayGrokImportAccount(account *Account, selection GatewayConnectionSelection) (gatewayGrokImportCredential, error) {
	var credential gatewayGrokImportCredential
	if account == nil || account.Type != AccountTypeOAuth || account.Platform != PlatformGrok || selection.ProfileID != GatewayGrokOAuthResponsesProfile || selection.MethodID != "refresh_token" && selection.MethodID != "sso" {
		return credential, ErrGatewayNativeIdentity
	}
	raw, err := json.Marshal(account.Credentials)
	if err != nil {
		return credential, ErrGatewayNativeIdentity
	}
	defer clear(raw)
	if json.Unmarshal(raw, &credential) != nil {
		return credential, ErrGatewayNativeIdentity
	}
	qualification := GatewayNativeGrokOAuthQualification{ClientID: credential.ClientID, RedirectURI: credential.RedirectURI, DeploymentQualified: true}
	if !qualification.valid() || selection.MethodID == "sso" && qualification.ClientID != xai.DefaultClientID {
		return credential, ErrGatewayNativeIdentity
	}
	credential, err = gatewayGrokImportDecode(raw, selection, qualification)
	if err != nil || !credential.ExpiresAt.After(time.Now()) {
		return credential, ErrGatewayNativeIdentity
	}
	return credential, nil
}

func gatewayGrokImportRequestHeaders(ctx context.Context, account *Account, request *http.Request) error {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || request == nil || request.URL == nil || request.URL.String() != GatewayGrokOAuthBaseURL+"/v1/responses" {
		return ErrGatewayNativeIdentity
	}
	binding, valid := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	if !valid {
		return ErrGatewayNativeIdentity
	}
	credential, err := gatewayGrokImportAccount(account, binding.native.Publication.Connection)
	if err != nil {
		return err
	}
	request.Header = make(http.Header)
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("X-XAI-Token-Auth", xai.CLITokenAuth)
	request.Header.Set("x-grok-client-identifier", xai.ProtectedClientIdentifier)
	request.Header.Set("x-authenticateresponse", "authenticate-response")
	request.Header.Set("User-Agent", xai.ProtectedUserAgent)
	request.Header.Set("Accept-Encoding", "identity")
	request.GetBody = nil
	return nil
}

func gatewayGrokImportRequestValid(request *http.Request, account *Account, native GatewayNativePublishedRoute, model string) bool {
	if request == nil || request.URL == nil || request.Method != http.MethodPost || request.URL.String() != GatewayGrokOAuthBaseURL+"/v1/responses" || native.Profile != GatewayGrokOAuthResponsesProfile ||
		native.Publication.DispatchFamily != "grok_responses" || native.Publication.AuthFamily != "grok_oauth" || !GatewayAccountModelIDValid(model) ||
		native.Publication.Credential != (GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native"}) {
		return false
	}
	credential, err := gatewayGrokImportAccount(account, native.Publication.Connection)
	return err == nil && request.Header.Get("Authorization") == "Bearer "+credential.AccessToken && request.Header.Get("Cookie") == "" && request.Header.Get("x-api-key") == "" &&
		request.Header.Get("X-XAI-Token-Auth") == xai.CLITokenAuth && request.Header.Get("x-grok-client-identifier") == xai.ProtectedClientIdentifier &&
		request.Header.Get("x-authenticateresponse") == "authenticate-response" && request.Header.Get("User-Agent") == xai.ProtectedUserAgent
}

func (s *OpenAIGatewayService) forwardGatewayNativeGrokImport(ctx context.Context, c *gin.Context, account *Account, route GatewayNativeRoute, body []byte) (*OpenAIForwardResult, error) {
	if !GatewayNativeConnectionAccountAuthorized(ctx, account) || c == nil || c.Request == nil || route.Profile != GatewayGrokOAuthResponsesProfile {
		return nil, ErrGatewayNativeIdentity
	}
	binding, valid := ctx.Value(gatewayConnectionDispatchBindingKey{}).(gatewayConnectionDispatchBinding)
	if !valid {
		return nil, ErrGatewayNativeIdentity
	}
	if _, err := gatewayGrokImportAccount(account, binding.native.Publication.Connection); err != nil {
		return nil, err
	}
	canonical, mapping, err := gatewayNativePrepareProviderResponses(route.Profile, body, binding.requestBytes)
	if err != nil {
		return nil, err
	}
	setGrokResponsesClientToolMapping(c, mapping)
	defer clearGrokResponsesClientToolMapping(c)
	return s.forwardGatewayNativeResponses(ctx, c, account, canonical)
}
