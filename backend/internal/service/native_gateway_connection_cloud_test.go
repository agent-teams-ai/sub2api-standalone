package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func gatewayCloudFixtureKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "source-project", "private_key_id": "fixture-key",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		"client_email": "fixture@source-project.iam.gserviceaccount.com", "token_uri": "https://foreign.invalid/steal",
	})
	require.NoError(t, err)
	return key, string(raw)
}

func gatewayCloudCompleteFixture(t *testing.T, adapter GatewayNativeConnectionMethodAdapter, input map[string]string) (*Account, GatewayNativeConnectionCredential) {
	t.Helper()
	ctx, err := WithGatewayNativeConsumer(context.Background(), "fixture-consumer")
	require.NoError(t, err)
	ctx, err = WithGatewayNativeOAuthOwner(ctx, "fixture-owner")
	require.NoError(t, err)
	ctx, err = WithGatewayConnectionSelection(ctx, adapter.Selection())
	require.NoError(t, err)
	in := GatewayNativeConnectionScope{Scope: GatewayNativeCredentialScope{
		Consumer: "fixture-consumer", Owner: "fixture-owner", Account: "fixture-account", Generation: uuid.NewString(), Purpose: GatewayOAuthBundlePurpose,
	}, Operation: "fixture-connect", Connection: adapter.Selection()}
	prepared, err := adapter.Prepare(input)
	require.NoError(t, err)
	defer clear(prepared.Material)
	defer clear(prepared.EntryClaim)
	credential, err := adapter.Complete(ctx, in, prepared.Material, nil)
	require.NoError(t, err)
	require.Equal(t, "cloud_identity", credential.Credential.CredentialPolicy)
	require.Equal(t, "none", credential.Credential.RefreshPolicy)
	require.Empty(t, credential.Observations, "syntax validation does not prove provider identity or subscription")
	credentials := map[string]any{}
	require.NoError(t, json.Unmarshal(credential.Document, &credentials))
	profile, found := LookupGatewayConnectionProfile(adapter.ProfileID)
	require.True(t, found)
	foreign := in
	foreign.Scope.Owner = "another-owner"
	_, err = adapter.Complete(ctx, foreign, prepared.Material, nil)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity)
	_, err = adapter.Complete(ctx, in, prepared.Material, map[string]string{"project_id": "changed-project"})
	require.ErrorIs(t, err, ErrGatewayNativeIdentity)
	return &Account{Platform: profile.Platform, Type: adapter.AccountType, Credentials: credentials}, credential
}

func TestGatewayNativeCloudMethodsUseExistingCloudGrammarAndAuthority(t *testing.T) {
	_, serviceAccount := gatewayCloudFixtureKey(t)
	cases := []struct {
		profile, method, base, model string
		input                        map[string]string
		check                        func(*testing.T, *Account, GatewayNativeConnectionCredential)
	}{
		{"anthropic-bedrock-messages-v1", "aws_sigv4", "https://bedrock-runtime.us-east-1.amazonaws.com", "claude-sonnet-4-6",
			map[string]string{"access_key_id": "AKIAFIXTURE", "secret_access_key": "fixture-secret", "session_token": "fixture-session", "region": "eu-west-1"},
			func(t *testing.T, account *Account, credential GatewayNativeConnectionCredential) {
				require.Equal(t, "native", credential.Credential.CredentialKind)
				signer, err := NewBedrockSignerFromAccount(account)
				require.NoError(t, err)
				request, err := http.NewRequest(http.MethodPost, BuildBedrockURL("eu-west-1", "fixture-model", true), strings.NewReader(`{}`))
				require.NoError(t, err)
				require.NoError(t, signer.SignRequest(context.Background(), request, []byte(`{}`)))
				require.Contains(t, request.Header.Get("Authorization"), "/eu-west-1/bedrock/aws4_request")
				require.Equal(t, "fixture-session", request.Header.Get("X-Amz-Security-Token"))
			}},
		{"anthropic-bedrock-messages-v1", "bedrock_api_key", "https://bedrock-runtime.us-east-1.amazonaws.com", "claude-sonnet-4-6",
			map[string]string{"api_key": "fixture-bedrock-key", "region": "us-east-1"},
			func(t *testing.T, account *Account, credential GatewayNativeConnectionCredential) {
				require.True(t, account.IsBedrockAPIKey())
				require.Equal(t, "api_key", credential.Credential.CredentialKind)
			}},
		{"anthropic-vertex-messages-v1", "vertex_service_account", "https://aiplatform.googleapis.com", "claude-sonnet-4-6",
			map[string]string{"service_account_json": serviceAccount, "project_id": "selected-project", "location": "us-east5"},
			func(t *testing.T, account *Account, credential GatewayNativeConnectionCredential) {
				require.Equal(t, "native", credential.Credential.CredentialKind)
				require.Equal(t, "selected-project", account.VertexProjectID())
				require.Equal(t, "us-east5", account.VertexLocation("claude-sonnet-4-6"))
				require.NotContains(t, string(credential.Document), "foreign.invalid")
			}},
		{"gemini-vertex-chat-v1", "vertex_service_account", "https://aiplatform.googleapis.com", "gemini-3.1-pro-preview",
			map[string]string{"service_account_json": serviceAccount},
			func(t *testing.T, account *Account, _ GatewayNativeConnectionCredential) {
				require.Equal(t, "source-project", account.VertexProjectID())
				require.False(t, account.IsVertexAPIKey())
			}},
		{"gemini-vertex-chat-v1", "vertex_api_key", "https://aiplatform.googleapis.com", "gemini-3.1-pro-preview",
			map[string]string{"api_key": "fixture-express-key"},
			func(t *testing.T, account *Account, _ GatewayNativeConnectionCredential) {
				endpoint, err := buildVertexGeminiAccountURL(account, "gemini-3.1-pro-preview", "streamGenerateContent", true, "")
				require.NoError(t, err)
				require.Equal(t, "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-3.1-pro-preview:streamGenerateContent?alt=sse", endpoint)
			}},
		{"gemini-vertex-chat-v1", "vertex_api_key", "https://aiplatform.googleapis.com", "gemini-3.1-pro-preview",
			map[string]string{"api_key": "fixture-project-key", "project_id": "selected-project", "location": "us-central1"},
			func(t *testing.T, account *Account, _ GatewayNativeConnectionCredential) {
				endpoint, err := buildVertexGeminiAccountURL(account, "gemini-3.1-pro-preview", "streamGenerateContent", true, "")
				require.NoError(t, err)
				require.Contains(t, endpoint, "/projects/selected-project/locations/us-central1/")
			}},
	}
	for _, row := range cases {
		t.Run(row.profile+"/"+row.method+"/"+row.input["project_id"], func(t *testing.T) {
			adapter, err := NewGatewayNativeCloudConnectionMethod(row.profile, row.method, "fixture-config", GatewayConnectionMount{BaseURL: row.base, Model: row.model, QualificationRef: "fixture-qualified"})
			require.NoError(t, err)
			account, credential := gatewayCloudCompleteFixture(t, adapter, row.input)
			defer clear(credential.Document)
			row.check(t, account, credential)
			input := map[string]string{}
			for key, value := range row.input {
				input[key] = value
			}
			input["base_url"] = "https://foreign.invalid"
			_, err = adapter.Prepare(input)
			require.ErrorIs(t, err, ErrGatewayNativeIdentity)
		})
	}
}

func TestGatewayNativeCloudTokenEndpointAndRedirectAreBounded(t *testing.T) {
	privateKey, serviceAccount := gatewayCloudFixtureKey(t)
	var calls, foreignCalls atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { foreignCalls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.NoError(t, r.ParseForm())
		token, err := jwt.Parse(r.PostForm.Get("assertion"), func(token *jwt.Token) (any, error) { return &privateKey.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}))
		require.NoError(t, err)
		require.True(t, token.Valid)
		if calls.Load() == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"fixture-access","token_type":"Bearer","expires_in":3600}`))
			return
		}
		http.Redirect(w, r, foreign.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	previous := vertexServiceAccountTokenURL
	vertexServiceAccountTokenURL = server.URL
	t.Cleanup(func() { vertexServiceAccountTokenURL = previous })
	adapter, err := NewGatewayNativeCloudConnectionMethod("anthropic-vertex-messages-v1", "vertex_service_account", "fixture-config", GatewayConnectionMount{BaseURL: "https://aiplatform.googleapis.com", Model: "claude-sonnet-4-6", QualificationRef: "fixture-qualified"})
	require.NoError(t, err)
	account, credential := gatewayCloudCompleteFixture(t, adapter, map[string]string{"service_account_json": serviceAccount})
	defer clear(credential.Document)
	require.Zero(t, calls.Load(), "prepare/complete perform no unapproved provider probe")
	key, err := parseVertexServiceAccountKey(account)
	require.NoError(t, err)
	require.Equal(t, server.URL, key.TokenURI)
	ctx := WithHTTPUpstreamRedirectsDisabled(context.Background())
	token, _, err := exchangeVertexServiceAccountToken(ctx, key, "")
	require.NoError(t, err)
	require.Equal(t, "fixture-access", token)
	_, _, err = exchangeVertexServiceAccountToken(ctx, key, "")
	require.Error(t, err)
	require.EqualValues(t, 2, calls.Load())
	require.Zero(t, foreignCalls.Load())
	account.Extra = map[string]any{GatewayNativeConnectionAccountExtraKey: true}
	_, err = getVertexServiceAccountAccessToken(context.Background(), nil, account)
	require.ErrorIs(t, err, ErrGatewayNativeIdentity, "an ordinary admin path cannot use a protected service-account view")
	require.EqualValues(t, 2, calls.Load())
}
