//go:build unit

package service

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestGatewayNativeGrokSSOChecksEveryRedirectAuthority(t *testing.T) {
	calls := 0
	transport := gatewayClaudeRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusSeeOther, Header: http.Header{"Location": {"https://foreign.example.test/steal"}},
			Body: io.NopCloser(strings.NewReader("{}")), Request: request}, nil
	})
	// Deliberately use a client whose default redirect policy would follow the
	// Location. The protected wrapper must independently disable that policy.
	client := &gatewayGrokSSOImportClient{client: &http.Client{Transport: transport}}
	request, err := http.NewRequest(http.MethodGet, xai.OAuthIssuer+"/device", nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, calls)
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.URL.Host = "foreign.example.test" },
		func(r *http.Request) { r.URL.Host = "auth.x.ai:443" },
		func(r *http.Request) { r.Host = "foreign.example.test" },
		func(r *http.Request) { r.URL.User = url.User("foreign") },
		func(r *http.Request) { r.URL.Scheme = "http" },
		func(r *http.Request) { r.URL.Opaque = "//foreign.example.test/steal" },
		func(r *http.Request) { r.URL.RawPath = "/%64evice" },
		func(r *http.Request) { r.RequestURI = "https://foreign.example.test/steal" },
		func(r *http.Request) { r.Method = http.MethodPost },
		func(r *http.Request) { r.URL.Host, r.URL.Path = "accounts.x.ai", "/unknown" },
		func(r *http.Request) { r.URL.Host, r.URL.Path, r.Method = "accounts.x.ai", "/", http.MethodPost },
	} {
		request, err := http.NewRequest(http.MethodGet, xai.OAuthIssuer+"/device", nil)
		require.NoError(t, err)
		mutate(request)
		_, err = client.Do(request)
		require.ErrorIs(t, err, ErrGatewayNativeIdentity)
		require.Equal(t, 1, calls, "untrusted authority reached the transport")
	}
}
