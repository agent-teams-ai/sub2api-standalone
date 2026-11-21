package repository

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// The native journal owns the one permitted token request. The regular OAuth
// client may follow redirects and includes provider error bodies in errors;
// neither behavior is suitable after a private connection entered its effect.
// This uses the same native Google OAuth grammar and fixed source endpoint.
type gatewayNativeGeminiOAuthClient struct {
	cfg    *config.Config
	client *http.Client
}

// Google One's native account requires the project assigned by Code Assist.
// Retain the source's load/onboard grammar, with one request per step and no
// endpoint fallback. An unfinished onboarding operation remains ambiguous;
// this method never repeats the already consumed authorization code.
func (c *gatewayNativeGeminiOAuthClient) GatewayNativeGeminiProject(ctx context.Context, token string) (string, error) {
	var loaded geminicli.LoadCodeAssistResponse
	if err := c.codeAssist(ctx, token, "loadCodeAssist", defaultLoadCodeAssistRequest(), &loaded); err != nil {
		return "", err
	}
	if loaded.CloudAICompanionProject != "" {
		return loaded.CloudAICompanionProject, nil
	}
	if loaded.GetTier() != "" {
		return "", service.ErrGatewayNativeEffectUnknown
	}
	request := defaultOnboardUserRequest()
	for _, tier := range loaded.AllowedTiers {
		if tier.IsDefault && tier.ID != "" {
			request.TierID = tier.ID
			break
		}
	}
	var onboarded geminicli.OnboardUserResponse
	if err := c.codeAssist(ctx, token, "onboardUser", request, &onboarded); err != nil || !onboarded.Done || onboarded.Response == nil {
		return "", service.ErrGatewayNativeEffectUnknown
	}
	switch value := onboarded.Response.CloudAICompanionProject.(type) {
	case string:
		return value, nil
	case map[string]any:
		if id, valid := value["id"].(string); valid {
			return id, nil
		}
	}
	return "", service.ErrGatewayNativeEffectUnknown
}

func (c *gatewayNativeGeminiOAuthClient) codeAssist(ctx context.Context, token, action string, input, output any) error {
	if c == nil || c.client == nil || ctx == nil || ctx.Err() != nil || token == "" || action != "loadCodeAssist" && action != "onboardUser" {
		return service.ErrGatewayNativeIdentity
	}
	body, err := json.Marshal(input)
	if err != nil {
		return service.ErrGatewayNativeIdentity
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, http.MethodPost, geminicli.GeminiCliBaseURL+"/v1internal:"+action, bytes.NewReader(body))
	if err != nil {
		return service.ErrGatewayNativeIdentity
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)
	response, err := c.client.Do(request)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil || response.Body == nil || response.StatusCode != http.StatusOK ||
		(response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		return service.ErrGatewayNativeEffectUnknown
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	defer clear(raw)
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, output) != nil {
		return service.ErrGatewayNativeEffectUnknown
	}
	return nil
}

func NewGatewayNativeGeminiOAuthClient(cfg *config.Config, transports ...http.RoundTripper) service.GeminiOAuthClient {
	if len(transports) > 1 || len(transports) == 1 && transports[0] == nil {
		return nil
	}
	client := &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
			MaxResponseHeaderBytes: 16384, MaxConnsPerHost: 2, DisableKeepAlives: true,
		},
	}
	if len(transports) == 1 {
		client.Transport = transports[0]
	}
	return &gatewayNativeGeminiOAuthClient{cfg: cfg, client: client}
}

func (c *gatewayNativeGeminiOAuthClient) ExchangeCode(ctx context.Context, oauthType, code, verifier, redirect, proxy string) (*geminicli.TokenResponse, error) {
	return c.exchange(ctx, oauthType, proxy, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect},
	})
}

func (c *gatewayNativeGeminiOAuthClient) RefreshToken(ctx context.Context, oauthType, refresh, proxy string) (*geminicli.TokenResponse, error) {
	return c.exchange(ctx, oauthType, proxy, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
}

func (c *gatewayNativeGeminiOAuthClient) exchange(ctx context.Context, oauthType, proxy string, form url.Values) (*geminicli.TokenResponse, error) {
	if c == nil || c.cfg == nil || c.client == nil || proxy != "" || ctx == nil || ctx.Err() != nil ||
		(oauthType != "google_one" && oauthType != "code_assist" && oauthType != "ai_studio") {
		return nil, service.ErrGatewayNativeIdentity
	}
	input := geminicli.OAuthConfig{ClientID: c.cfg.Gemini.OAuth.ClientID, ClientSecret: c.cfg.Gemini.OAuth.ClientSecret, Scopes: c.cfg.Gemini.OAuth.Scopes}
	if oauthType != "ai_studio" {
		input.ClientID, input.ClientSecret = "", ""
	}
	effective, err := geminicli.EffectiveOAuthConfig(input, oauthType)
	if err != nil {
		return nil, service.ErrGatewayNativeIdentity
	}
	form.Set("client_id", effective.ClientID)
	form.Set("client_secret", effective.ClientSecret)
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, http.MethodPost, geminicli.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, service.ErrGatewayNativeIdentity
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := c.client.Do(request)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil || response.Body == nil || response.StatusCode != http.StatusOK ||
		(response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		return nil, service.ErrGatewayNativeEffectUnknown
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	defer clear(raw)
	if err != nil || len(raw) > 65536 {
		return nil, service.ErrGatewayNativeEffectUnknown
	}
	var out geminicli.TokenResponse
	if json.Unmarshal(raw, &out) != nil || out.AccessToken == "" {
		return nil, service.ErrGatewayNativeEffectUnknown
	}
	return &out, nil
}
