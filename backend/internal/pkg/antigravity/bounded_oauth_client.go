package antigravity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

var errBoundedOAuth = errors.New("antigravity bounded OAuth request failed")

// NewBoundedOAuthClient retains the native token grammar while disabling
// redirect/replay and bounding responses. A transport override is trusted
// composition (for controlled TLS fixtures), never account or request input.
func NewBoundedOAuthClient(transports ...http.RoundTripper) (*Client, error) {
	if len(transports) > 1 {
		return nil, errBoundedOAuth
	}
	var transport http.RoundTripper = &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		MaxResponseHeaderBytes: 16384, MaxConnsPerHost: 2, DisableKeepAlives: true,
	}
	if len(transports) == 1 {
		if transports[0] == nil {
			return nil, errBoundedOAuth
		}
		transport = transports[0]
	}
	return &Client{httpClient: &http.Client{Transport: boundedOAuthTransport{transport}, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type boundedOAuthTransport struct{ source http.RoundTripper }

func (t boundedOAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request.GetBody = nil
	request.Header.Del("Idempotency-Key")
	request.Header.Del("X-Idempotency-Key")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := t.source.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, errBoundedOAuth
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(response.Body)
	if response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, errBoundedOAuth
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 || response.StatusCode != http.StatusOK {
		clear(raw)
		return nil, errBoundedOAuth
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}

// OAuthConfigurationFingerprint is private continuation evidence. It carries
// no credential and must never be presented as a verified provider principal.
func OAuthConfigurationFingerprint() (string, error) {
	secret, err := getClientSecret()
	if err != nil {
		return "", errBoundedOAuth
	}
	digest := sha256.Sum256([]byte(ClientID + "\x00" + secret + "\x00" + RedirectURI + "\x00" + Scopes))
	return hex.EncodeToString(digest[:]), nil
}

// LoadCodeAssistFromURL is the fixed-endpoint form used by a qualified private
// connection. Unlike the scheduler reader it never switches hosts or writes
// global URL-availability state after an account-specific observation.
func (c *Client) LoadCodeAssistFromURL(ctx context.Context, baseURL, accessToken string) (*LoadCodeAssistResponse, error) {
	input := LoadCodeAssistRequest{}
	input.Metadata.IDEType, input.Metadata.IDEVersion, input.Metadata.IDEName = "ANTIGRAVITY", GetUserAgentVersionForContext(ctx), "antigravity"
	var out LoadCodeAssistResponse
	if err := c.boundedCodeAssist(ctx, baseURL, accessToken, "loadCodeAssist", input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OnboardUserFromURL(ctx context.Context, baseURL, accessToken, tierID string) (*OnboardUserResponse, error) {
	if tierID == "" {
		return nil, errBoundedOAuth
	}
	input := OnboardUserRequest{TierID: tierID}
	input.Metadata.IDEType, input.Metadata.Platform, input.Metadata.PluginType = "ANTIGRAVITY", "PLATFORM_UNSPECIFIED", "GEMINI"
	var out OnboardUserResponse
	if err := c.boundedCodeAssist(ctx, baseURL, accessToken, "onboardUser", input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) boundedCodeAssist(ctx context.Context, baseURL, token, action string, input, output any) error {
	if c == nil || c.httpClient == nil || ctx == nil || ctx.Err() != nil || token == "" ||
		(baseURL != antigravityProdBaseURL && baseURL != antigravityDailyBaseURL) || action != "loadCodeAssist" && action != "onboardUser" {
		return errBoundedOAuth
	}
	body, err := json.Marshal(input)
	if err != nil {
		return errBoundedOAuth
	}
	request, err := NewAPIRequestWithURL(ctx, baseURL, action, token, body)
	if err != nil {
		return errBoundedOAuth
	}
	response, err := c.httpClient.Do(request)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil || response == nil || response.Body == nil || response.StatusCode != http.StatusOK {
		return errBoundedOAuth
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 65537))
	if decoder.Decode(output) != nil || decoder.Decode(new(any)) != io.EOF {
		return errBoundedOAuth
	}
	return nil
}

func OfficialBaseURL(baseURL string) bool {
	return strings.TrimSpace(baseURL) == baseURL && (baseURL == antigravityProdBaseURL || baseURL == antigravityDailyBaseURL)
}
