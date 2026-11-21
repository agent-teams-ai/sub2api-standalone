//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeConnectionJoinedServiceAccount(t *testing.T, email, project string) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]string{"type": "service_account", "project_id": project, "private_key_id": "joined-key",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		"client_email": email, "token_uri": "https://untrusted.invalid/must-never-receive-assertions"})
	require.NoError(t, err)
	return key, string(raw)
}

func nativeConnectionJoinedServiceAccountToken(w http.ResponseWriter, r *http.Request, key *rsa.PrivateKey, email, access string) bool {
	if r.Host != "oauth2.googleapis.com" || r.URL.Path != "/token" {
		return false
	}
	if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		w.WriteHeader(http.StatusForbidden)
		return true
	}
	token, err := jwt.Parse(r.PostForm.Get("assertion"), func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience("https://oauth2.googleapis.com/token"), jwt.WithIssuer(email), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		w.WriteHeader(http.StatusForbidden)
		return true
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["scope"] != "https://www.googleapis.com/auth/cloud-platform" {
		w.WriteHeader(http.StatusForbidden)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600})
	return true
}

func newNativeConnectionJoinedCloudFamily(t *testing.T, _ *Config) nativeConnectionJoinedFamily {
	t.Helper()
	family := nativeConnectionJoinedFamily{Hosts: []string{"bedrock-runtime.eu-west-1.amazonaws.com:443", "us-east5-aiplatform.googleapis.com:443", "oauth2.googleapis.com:443"}}
	bedrock := nativeConnectionJoinedProfile("anthropic-bedrock-messages-v1", "https://bedrock-runtime.eu-west-1.amazonaws.com", "claude-sonnet-4-6")
	vertex := nativeConnectionJoinedProfile("anthropic-vertex-messages-v1", "https://us-east5-aiplatform.googleapis.com", "claude-sonnet-4-6")
	family.Profiles = append(family.Profiles, bedrock, vertex)
	for _, variant := range []string{"aws_access_key", "aws_temporary_session_credentials", "bedrock_bearer_key", "google_service_account_json"} {
		profile, method, kind, protocol := bedrock, "aws_sigv4", "native", "bedrock"
		secret, access, session := "joined-cloud-secret-"+uuid.NewString(), "AKIA"+strings.ReplaceAll(uuid.NewString(), "-", ""), ""
		fields := map[string]string{"access_key_id": access, "secret_access_key": secret, "region": "eu-west-1"}
		var key *rsa.PrivateKey
		const email = "fixture@source-project.iam.gserviceaccount.com"
		if variant == "aws_temporary_session_credentials" {
			session = "joined-cloud-session-" + uuid.NewString()
			fields["session_token"] = session
		} else if variant == "bedrock_bearer_key" {
			method, kind = "bedrock_api_key", "api_key"
			fields = map[string]string{"api_key": secret, "region": "eu-west-1"}
		} else if variant == "google_service_account_json" {
			profile, method, protocol, access = vertex, "vertex_service_account", "anthropic", "joined-vertex-access-"+uuid.NewString()
			var document string
			key, document = nativeConnectionJoinedServiceAccount(t, email, "source-project")
			fields = map[string]string{"service_account_json": document, "project_id": "selected-project", "location": "us-east5"}
		}
		definition, ok := service.LookupGatewayConnectionProfile(profile.Profile)
		require.True(t, ok)
		var tokens atomic.Int64
		peer := &nativeConnectionJoinedPeer{Protocol: protocol, Row: nativeConnectionJoinedRow{
			ID: nativeConnectionJoinedRowID(profile.Profile, method, variant), ProfileID: profile.Profile, MethodID: method, Variant: variant, Fields: fields,
			CredentialKind: kind, CredentialPolicy: "cloud_identity", RefreshPolicy: "none", Provider: definition.Provider.ID, Model: profile.Model, ExecutionFamily: definition.DispatchFamily, Execute: true,
		}}
		if key != nil {
			peer.Handle = func(w http.ResponseWriter, r *http.Request) bool {
				if r.Host == "oauth2.googleapis.com" && r.URL.Path == "/token" {
					tokens.Add(1)
				}
				return nativeConnectionJoinedServiceAccountToken(w, r, key, email, access)
			}
		}
		peer.InferenceAuth = func(r *http.Request) bool {
			if r.Method != http.MethodPost || r.Header.Get("Cookie") != "" || r.Header.Get("X-Codex-Turn-State") != "" || r.Header.Get("x-api-key") != "" {
				return false
			}
			raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if err != nil || len(raw) > 1<<20 {
				return false
			}
			if method == "vertex_service_account" {
				return "https://"+r.Host+r.URL.RequestURI() == profile.BaseURL+"/v1/projects/selected-project/locations/us-east5/publishers/anthropic/models/"+profile.Model+":streamRawPredict" &&
					r.Header.Get("Authorization") == "Bearer "+access && bytes.Contains(raw, []byte(`"anthropic_version":"vertex-2023-10-16"`))
			}
			account := &service.Account{Type: service.AccountTypeBedrock, Credentials: map[string]any{"aws_region": "eu-west-1"}}
			model, ok := service.ResolveBedrockModelID(account, profile.Model)
			if !ok || "https://"+r.Host+r.URL.RequestURI() != service.BuildBedrockURL("eu-west-1", model, true) || !bytes.Contains(raw, []byte(`"anthropic_version":"bedrock-2023-05-31"`)) {
				return false
			}
			if method == "bedrock_api_key" {
				return r.Header.Get("Authorization") == "Bearer "+secret && r.Header.Get("X-Amz-Security-Token") == ""
			}
			if r.Header.Get("X-Amz-Security-Token") != session {
				return false
			}
			signed, err := http.NewRequest(http.MethodPost, "https://"+r.Host+r.URL.RequestURI(), bytes.NewReader(raw))
			if err != nil {
				return false
			}
			signed.Header = r.Header.Clone()
			signed.Header.Del("Authorization")
			_, tail, found := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
			names, _, foundNames := strings.Cut(tail, ",")
			if !found || !foundNames {
				return false
			}
			allowed := map[string]bool{}
			for _, name := range strings.Split(names, ";") {
				allowed[name] = true
			}
			if !allowed["host"] || !allowed["content-type"] || !allowed["x-amz-date"] {
				return false
			}
			for name := range signed.Header {
				if !allowed[strings.ToLower(name)] {
					signed.Header.Del(name)
				}
			}
			instant, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
			if err != nil {
				return false
			}
			digest := sha256.Sum256(raw)
			if v4.NewSigner().SignHTTP(r.Context(), aws.Credentials{AccessKeyID: access, SecretAccessKey: secret, SessionToken: session}, signed, hex.EncodeToString(digest[:]), "bedrock", "eu-west-1", instant) != nil {
				return false
			}
			return signed.Header.Get("Authorization") == r.Header.Get("Authorization")
		}
		peer.Counters = func() map[string]int64 { return map[string]int64{"tokenExchanges": tokens.Load()} }
		peer.Verify = func() {
			if key == nil {
				require.Zero(t, tokens.Load(), "AWS credentials cannot enter Google OAuth")
			} else {
				require.Positive(t, tokens.Load(), "service account assertion must reach the fixed token peer")
			}
		}
		family.Peers = append(family.Peers, peer)
	}
	family.Compose = func(b nativeConnectionJoinedBuild) ([]service.GatewayNativeConnectionMethodAdapter, map[string]service.GatewayNativeConnectionDispatchAdapter, error) {
		dispatchers, err := service.NewGatewayNativeCloudDispatchers(b.Gateway, b.Transport)
		if err != nil {
			return nil, nil, err
		}
		methods := []service.GatewayNativeConnectionMethodAdapter{}
		for _, p := range family.Profiles {
			generation, err := connectionConfigurationGeneration(*b.Config, p)
			if err != nil {
				return nil, nil, err
			}
			definition, _ := service.LookupGatewayConnectionProfile(p.Profile)
			for _, m := range definition.Methods {
				adapter, err := service.NewGatewayNativeCloudConnectionMethod(p.Profile, m.MethodID, generation, service.GatewayConnectionMount{BaseURL: p.BaseURL, Model: p.Model, QualificationRef: p.QualificationRef})
				if err != nil {
					return nil, nil, err
				}
				methods = append(methods, adapter)
			}
		}
		return methods, dispatchers, nil
	}
	return family
}
