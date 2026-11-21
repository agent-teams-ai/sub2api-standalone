//go:build integration || gatewaycustody

package repository

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

type nativeCodexImportFixture struct {
	t                         *testing.T
	method, variant, clientID string
	access, refresh, account  string
	agent                     ed25519.PrivateKey
	key                       *rsa.PrivateKey
	phase                     atomic.Int32
	tokens, checks, tasks     atomic.Int32
	models, inference, quotas atomic.Int32
}

func (f *nativeCodexImportFixture) bearer() string {
	if n := f.tokens.Load(); n != 0 {
		return fmt.Sprintf("rotated-%s-%d", f.access, n)
	}
	return f.access
}

func (f *nativeCodexImportFixture) signedID() string {
	claims, err := json.Marshal(map[string]any{"iss": service.GatewayOAuthIssuer, "sub": "subject-" + f.account, "aud": f.clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "email": "verified-codex@example.test"})
	require.NoError(f.t, err)
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	require.NoError(f.t, err)
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (f *nativeCodexImportFixture) assertHeaders(r *http.Request) {
	f.t.Helper()
	assert.Equal(f.t, f.account, r.Header.Get("ChatGPT-Account-Id"))
	assert.Equal(f.t, service.CodexCanonicalUserAgent(), r.Header.Get("User-Agent"))
	assert.Equal(f.t, service.CodexCanonicalClientVersion(), r.Header.Get("Version"))
	assert.Empty(f.t, r.Header.Get("Cookie"))
	assert.Empty(f.t, r.Header.Get("X-Codex-Turn-State"))
	assert.Empty(f.t, r.Header.Get("x-api-key"))
	if len(f.agent) == 0 {
		assert.Equal(f.t, "Bearer "+f.bearer(), r.Header.Get("Authorization"))
		return
	}
	encoded := strings.TrimPrefix(r.Header.Get("Authorization"), "AgentAssertion ")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(f.t, err)
	var assertion map[string]string
	require.NoError(f.t, json.Unmarshal(raw, &assertion))
	assert.Equal(f.t, "runtime-"+f.account, assertion["agent_runtime_id"])
	assert.Equal(f.t, fmt.Sprintf("task-%d", f.tasks.Load()), assertion["task_id"])
	signature, err := base64.StdEncoding.DecodeString(assertion["signature"])
	require.NoError(f.t, err)
	assert.True(f.t, ed25519.Verify(ed25519.PublicKey(f.agent[32:]), []byte(assertion["agent_runtime_id"]+":"+assertion["task_id"]+":"+assertion["timestamp"]), signature))
}

func (f *nativeCodexImportFixture) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Host + r.URL.Path {
	case "auth.openai.com/.well-known/jwks.json":
		assert.Equal(f.t, http.MethodGet, r.Method)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kid": "fixture", "kty": "RSA", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}})
	case "auth.openai.com/oauth/token":
		assert.Equal(f.t, http.MethodPost, r.Method)
		require.NoError(f.t, r.ParseForm())
		assert.Equal(f.t, "refresh_token", r.PostForm.Get("grant_type"))
		assert.Equal(f.t, f.clientID, r.PostForm.Get("client_id"))
		n := f.tokens.Add(1)
		previous := f.refresh
		if n > 1 {
			previous = fmt.Sprintf("%s-%d", f.refresh, n-1)
		}
		assert.Equal(f.t, previous, r.PostForm.Get("refresh_token"))
		expires := 3600
		if n == 1 {
			expires = 45 // Real native expiry triggers the first maintenance.
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.bearer(), "refresh_token": fmt.Sprintf("%s-%d", f.refresh, n), "id_token": f.signedID(), "expires_in": expires, "token_type": "Bearer"})
	case "chatgpt.com/backend-api/accounts/check/v4-2023-04-27":
		f.checks.Add(1)
		assert.Equal(f.t, "Bearer "+f.bearer(), r.Header.Get("Authorization"))
		assert.Empty(f.t, r.Header.Get("ChatGPT-Account-Id"))
		_ = json.NewEncoder(w).Encode(map[string]any{"accounts": map[string]any{"workspace-key-is-not-the-account": map[string]any{"account": map[string]any{"account_id": f.account, "is_default": true, "plan_type": "plus"}, "entitlement": map[string]any{"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}}})
	case "auth.openai.com/api/accounts/v1/user-auth-credential/whoami":
		f.checks.Add(1)
		assert.Equal(f.t, "Bearer "+f.access, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "pat@example.test", "chatgpt_user_id": "user-" + f.account, "chatgpt_account_id": f.account, "chatgpt_plan_type": "plus", "chatgpt_account_is_fedramp": false})
	case "chatgpt.com/backend-api/codex/models":
		f.models.Add(1)
		assert.Equal(f.t, http.MethodGet, r.Method)
		assert.Equal(f.t, service.CodexCanonicalClientVersion(), r.URL.Query().Get("client_version"))
		f.assertHeaders(r)
		_, _ = io.WriteString(w, `{"models":[{"slug":"future-native-model","visibility":"list","supported_endpoints":["responses"],"input_modalities":["text"],"instructions":"PRIVATE-NATIVE-INSTRUCTIONS"}]}`)
	case "chatgpt.com/backend-api/wham/usage":
		f.quotas.Add(1)
		assert.Equal(f.t, http.MethodGet, r.Method)
		f.assertHeaders(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"account_id": f.account, "rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": 75, "limit_window_seconds": 18000}}})
	case "chatgpt.com/backend-api/codex/responses":
		f.inference.Add(1)
		assert.Equal(f.t, http.MethodPost, r.Method)
		f.assertHeaders(r)
		raw, err := io.ReadAll(r.Body)
		require.NoError(f.t, err)
		assert.NotContains(f.t, string(raw), "max_output_tokens")
		assert.Contains(f.t, string(raw), `"model":"future-native-model"`)
		if f.phase.Load() == 3 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_task_id","message":"private provider task rejection"}}`)
			return
		}
		w.Header().Set("X-Codex-Primary-Used-Percent", "25")
		w.Header().Set("X-Codex-Primary-Window-Minutes", "300")
		second := f.phase.Load() == 2
		if second {
			assert.Contains(f.t, string(raw), "synthetic file contents")
		}
		nativeConnectionWriteProtocol(f.t, w, &nativeConnectionWireCase{model: "future-native-model", protocol: service.APIProtocolResponses}, second)
	default:
		if r.Host != "auth.openai.com" || r.URL.Path != "/api/accounts/v1/agent/runtime-"+f.account+"/task/register" {
			f.t.Errorf("unexpected external endpoint: %s%s", r.Host, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		assert.Equal(f.t, http.MethodPost, r.Method)
		assert.Empty(f.t, r.Header.Get("Authorization"))
		var registration map[string]string
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&registration))
		signature, err := base64.StdEncoding.DecodeString(registration["signature"])
		require.NoError(f.t, err)
		assert.True(f.t, ed25519.Verify(ed25519.PublicKey(f.agent[32:]), []byte("runtime-"+f.account+":"+registration["timestamp"]), signature))
		// Exercise the existing native sealed task response, including decryption.
		digest := sha512.Sum512(f.agent.Seed())
		digest[0] &= 248
		digest[31] &= 127
		digest[31] |= 64
		curve, err := curve25519.X25519(digest[:32], curve25519.Basepoint)
		require.NoError(f.t, err)
		var public [32]byte
		copy(public[:], curve)
		encrypted, err := box.SealAnonymous(nil, []byte(fmt.Sprintf("task-%d", f.tasks.Add(1))), &public, rand.Reader)
		require.NoError(f.t, err)
		_ = json.NewEncoder(w).Encode(map[string]string{"encrypted_task_id": base64.StdEncoding.EncodeToString(encrypted)})
	}
}
