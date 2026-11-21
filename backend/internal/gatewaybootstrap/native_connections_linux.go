//go:build linux

package gatewaybootstrap

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func connectionMaterialKey(c Config) ([]byte, error) {
	if c.OAuth != nil {
		key, err := base64.StdEncoding.Strict().DecodeString(c.OAuth.IntentKey)
		if err != nil || len(key) != 32 {
			return nil, ErrDenied
		}
		return key, nil
	}
	for _, candidate := range c.Custody.Keys {
		if candidate.ID != c.Custody.ActiveKeyID {
			continue
		}
		key, err := base64.StdEncoding.Strict().DecodeString(candidate.Key)
		if err != nil || len(key) != 32 {
			return nil, ErrDenied
		}
		mac := hmac.New(sha256.New, key)
		clear(key)
		_, _ = mac.Write([]byte("account-gateway/native/connection-material-key/v1"))
		return mac.Sum(nil), nil
	}
	return nil, ErrDenied
}

// Compose the finite native method implementations against existing lifecycle
// journals. Every advertised method has its own exact deployment generation;
// disabled inventory entries are never substituted by another credential kind.
func composePrivateNativeConnections(c Config, repo service.AccountRepository, db *sql.DB, custody *service.GatewayNativeCredentialCustody,
	gateway *service.OpenAIGatewayService, oauth *privateOAuth, profiles []gatewaytransport.QualifiedProfile) ([]service.GatewayNativeConnectionAdapter, error) {
	physical, ok := repo.(service.GatewayNativeConnectionRepository)
	if !ok || oauth == nil || oauth.owners == nil || oauth.cleanup == nil {
		return nil, ErrDenied
	}
	journal, err := repository.NewGatewayNativeOAuthConnectRepository(db)
	if err != nil {
		return nil, ErrDenied
	}
	key, err := connectionMaterialKey(c)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	dispatchers, err := service.NewGatewayNativeAPIKeyDispatchers(gateway)
	if err != nil {
		return nil, ErrDenied
	}
	cloud, err := service.NewGatewayNativeCloudDispatchers(gateway)
	if err != nil {
		return nil, ErrDenied
	}
	for family, dispatch := range cloud {
		if _, duplicate := dispatchers[family]; duplicate {
			return nil, ErrDenied
		}
		dispatchers[family] = dispatch
	}
	codex, err := service.NewGatewayNativeCodexImportDispatcher(gateway)
	if err != nil {
		return nil, ErrDenied
	}
	dispatchers["codex_responses"] = codex
	claude, err := service.NewGatewayNativeClaudeImportDispatcher(gateway)
	if err != nil {
		return nil, ErrDenied
	}
	dispatchers["claude_messages"] = claude
	grok, err := service.NewGatewayNativeGrokImportDispatcher(gateway, dispatchers["grok_responses"])
	if err != nil {
		return nil, ErrDenied
	}
	dispatchers["grok_responses"] = grok
	gemini, err := service.NewGatewayNativeGeminiDispatchers(gateway)
	if err != nil {
		return nil, ErrDenied
	}
	for family, dispatch := range gemini {
		if _, duplicate := dispatchers[family]; duplicate {
			return nil, ErrDenied
		}
		dispatchers[family] = dispatch
	}
	antiDispatchers, err := service.NewGatewayNativeAntigravityDispatchers(gateway)
	if err != nil {
		return nil, ErrDenied
	}
	for family, dispatch := range antiDispatchers {
		if _, duplicate := dispatchers[family]; duplicate {
			return nil, ErrDenied
		}
		dispatchers[family] = dispatch
	}
	methods := []service.GatewayNativeConnectionMethodAdapter{}
	for _, mounted := range profiles {
		definition, found := service.LookupGatewayConnectionProfile(mounted.Profile)
		generation, generationErr := connectionConfigurationGeneration(c, mounted)
		if !found || generationErr != nil {
			return nil, ErrDenied
		}
		if _, bound := dispatchers[definition.DispatchFamily]; !bound {
			continue
		}
		mount := service.GatewayConnectionMount{BaseURL: mounted.BaseURL, Model: mounted.Model, QualificationRef: mounted.QualificationRef}
		switch {
		case definition.DispatchFamily == "codex_responses":
			imports, err := service.NewGatewayNativeCodexImportMethods(generation, mount, nil)
			if err != nil {
				return nil, ErrDenied
			}
			methods = append(methods, imports...)
			continue
		case definition.DispatchFamily == "claude_messages":
			imports, err := service.NewGatewayNativeClaudeImportMethods(generation, mount, nil)
			if err != nil {
				return nil, ErrDenied
			}
			methods = append(methods, imports...)
			continue
		case definition.ProfileID == service.GatewayGrokOAuthResponsesProfile:
			if c.Grok == nil {
				continue
			}
			qualification := service.GatewayNativeGrokOAuthQualification{ClientID: c.Grok.ClientID, RedirectURI: c.Grok.RedirectURI, DeploymentQualified: c.Grok.DeploymentQualified}
			imports, err := service.NewGatewayNativeGrokImportMethods(generation, mount, qualification, nil)
			if err != nil {
				return nil, ErrDenied
			}
			methods = append(methods, imports...)
			continue
		case definition.DispatchFamily == "gemini_oauth":
			if oauth.gemini == nil {
				nativeConfig := privateGatewayServiceConfig()
				if c.GeminiOAuth != nil {
					nativeConfig.Gemini.OAuth.ClientID = c.GeminiOAuth.ClientID
					nativeConfig.Gemini.OAuth.ClientSecret = c.GeminiOAuth.ClientSecret
					nativeConfig.Gemini.OAuth.Scopes = c.GeminiOAuth.Scopes
				}
				oauth.gemini = service.NewGeminiOAuthService(nil, repository.NewGatewayNativeGeminiOAuthClient(nativeConfig), nil, nil, nativeConfig)
			}
			if definition.ProfileID == "gemini-ai-studio-responses-oauth-v1" && !oauth.gemini.GetOAuthConfig().AIStudioOAuthEnabled {
				continue
			}
			adapter, err := service.NewGatewayNativeGeminiConnectionMethod(mounted.Profile, generation, mount, oauth.gemini)
			if err != nil {
				return nil, ErrDenied
			}
			methods = append(methods, adapter)
			continue
		}
		for _, method := range definition.Methods {
			var adapter service.GatewayNativeConnectionMethodAdapter
			var adapterErr error
			switch {
			case method.MethodID == "api_key" && len(method.ConfigurationRequirements) == 0 && len(method.AllowedCredentialPolicies) == 1 && method.AllowedCredentialPolicies[0] == "provider_key":
				adapter, adapterErr = service.NewGatewayNativeAPIKeyConnectionMethod(mounted.Profile, generation, mount, gateway)
			case definition.DispatchFamily == "bedrock_messages" || definition.DispatchFamily == "vertex_messages" || definition.DispatchFamily == "vertex_gemini":
				adapter, adapterErr = service.NewGatewayNativeCloudConnectionMethod(mounted.Profile, method.MethodID, generation, mount)
			case definition.DispatchFamily == "antigravity":
				if method.MethodID != "api_key" {
					if _, err := antigravity.OAuthConfigurationFingerprint(); err != nil {
						continue
					}
				}
				adapter, adapterErr = service.NewGatewayNativeAntigravityConnectionMethod(mounted.Profile, method.MethodID, generation, mount)
			default:
				continue
			}
			if adapterErr != nil {
				return nil, ErrDenied
			}
			methods = append(methods, adapter)
		}
	}
	authorize := func(refresh bool) service.GatewayNativeOAuthEntryAuthorizer {
		return func(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string) error {
			var owner string
			var err error
			if refresh {
				owner, err = oauth.owners.AuthorizeNativeOAuthRefreshOwner(ctx, scope.Consumer, operation, scope.Account, scope.Generation)
			} else {
				owner, err = oauth.owners.AuthorizeNativeOAuthOwner(ctx, scope.Consumer, operation, scope.Account, scope.Generation)
			}
			if err != nil || ctx.Err() != nil || owner != scope.Owner {
				return ErrDenied
			}
			return nil
		}
	}
	oauth.nativeConnections, err = service.NewGatewayNativeConnections(service.GatewayNativeConnectionsDependencies{
		Journal: journal, Repository: physical, Custody: custody, MaterialKey: key, Methods: methods,
		Connect: authorize(false), Refresh: authorize(true), Cleanup: oauth.cleanup, Dispatchers: dispatchers, Gateway: gateway, Codex: oauth.dispatch,
	})
	if err != nil {
		return nil, ErrDenied
	}
	adapters := []service.GatewayNativeConnectionAdapter{}
	for _, definition := range service.GatewayNativeConnectionInventory() {
		if _, bound := dispatchers[definition.DispatchFamily]; !bound {
			continue
		}
		adapter := oauth.nativeConnections.RecoveryAdapter(definition.ProfileID)
		switch definition.ProfileID {
		case service.GatewayCodexOAuthResponsesProfile:
			adapter.RecoveryMethodIDs = []string{"mobile_refresh_token", "codex_session", "agent_identity", "codex_pat"}
		case service.GatewayClaudeOAuthMessagesProfile:
			adapter.RecoveryMethodIDs = []string{"cookie_oauth", "setup_token_code", "setup_token_cookie"}
		case service.GatewayGrokOAuthResponsesProfile:
			adapter.RecoveryMethodIDs = []string{"refresh_token", "sso"}
		}
		adapters = append(adapters, adapter)
	}
	// Retained legacy operations remain readable and erasable after their
	// creation configuration is removed. These ports have no provider client,
	// grant entry, refresh or dispatch authority.
	if oauth.connect == nil || oauth.dispatch == nil || oauth.refresh == nil {
		source, ok := repo.(service.GatewayNativeCodexRecoveryRepository)
		if !ok {
			return nil, ErrDenied
		}
		adapter, err := service.NewGatewayNativeCodexRecoveryConnectionAdapter(journal, source, custody, oauth.cleanup)
		if err != nil {
			return nil, ErrDenied
		}
		adapters = append(adapters, adapter)
	}
	if oauth.claude == nil {
		source, err := repository.NewGatewayNativeClaudeRepository(db)
		if err != nil {
			return nil, ErrDenied
		}
		adapter, err := service.NewGatewayNativeClaudeRecoveryConnectionAdapter(source, custody, oauth.cleanup)
		if err != nil {
			return nil, ErrDenied
		}
		adapters = append(adapters, adapter)
	}
	if oauth.grok == nil {
		source, err := repository.NewGatewayNativeGrokRepository(db)
		if err != nil {
			return nil, ErrDenied
		}
		adapter, err := service.NewGatewayNativeGrokRecoveryConnectionAdapter(source, custody, oauth.cleanup)
		if err != nil {
			return nil, ErrDenied
		}
		adapters = append(adapters, adapter)
	}
	return adapters, nil
}
