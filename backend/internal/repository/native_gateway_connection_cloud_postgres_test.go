//go:build integration || gatewaycustody

package repository

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nativeCloudWireFixture struct {
	t                             *testing.T
	method, variant, profile      string
	base, secret, access, session string
	key                           *rsa.PrivateKey
	phase, inference, tokens      atomic.Int32
}

func nativeCloudEventFrame(t *testing.T, data []byte) []byte {
	t.Helper()
	var headers bytes.Buffer
	for _, pair := range [][2]string{{":event-type", "chunk"}, {":message-type", "event"}, {":content-type", "application/json"}} {
		require.NoError(t, headers.WriteByte(byte(len(pair[0]))))
		_, _ = headers.WriteString(pair[0])
		require.NoError(t, headers.WriteByte(7))
		require.NoError(t, binary.Write(&headers, binary.BigEndian, uint16(len(pair[1]))))
		_, _ = headers.WriteString(pair[1])
	}
	payload, err := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString(data)})
	require.NoError(t, err)
	var wire bytes.Buffer
	require.NoError(t, binary.Write(&wire, binary.BigEndian, uint32(16+headers.Len()+len(payload))))
	require.NoError(t, binary.Write(&wire, binary.BigEndian, uint32(headers.Len())))
	require.NoError(t, binary.Write(&wire, binary.BigEndian, crc32.ChecksumIEEE(wire.Bytes())))
	_, _ = wire.Write(headers.Bytes())
	_, _ = wire.Write(payload)
	require.NoError(t, binary.Write(&wire, binary.BigEndian, crc32.ChecksumIEEE(wire.Bytes())))
	return wire.Bytes()
}

func (f *nativeCloudWireFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.Host == "oauth2.googleapis.com" {
		f.tokens.Add(1)
		assert.Equal(f.t, "/token", r.URL.Path)
		assert.Equal(f.t, http.MethodPost, r.Method)
		require.NoError(f.t, r.ParseForm())
		assert.Equal(f.t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.PostForm.Get("grant_type"))
		token, err := jwt.Parse(r.PostForm.Get("assertion"), func(*jwt.Token) (any, error) { return &f.key.PublicKey, nil },
			jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience("https://oauth2.googleapis.com/token"), jwt.WithIssuer("fixture@source-project.iam.gserviceaccount.com"), jwt.WithExpirationRequired())
		require.NoError(f.t, err)
		require.True(f.t, token.Valid)
		claims, ok := token.Claims.(jwt.MapClaims)
		require.True(f.t, ok)
		assert.Equal(f.t, "https://www.googleapis.com/auth/cloud-platform", claims["scope"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.access, "token_type": "Bearer", "expires_in": 3600})
		return
	}
	f.inference.Add(1)
	assert.Equal(f.t, http.MethodPost, r.Method)
	assert.Empty(f.t, r.Header.Get("Cookie"))
	assert.Empty(f.t, r.Header.Get("X-Codex-Turn-State"))
	assert.Empty(f.t, r.Header.Get("x-api-key"))
	body, err := io.ReadAll(r.Body)
	require.NoError(f.t, err)
	assert.NotContains(f.t, string(body), f.secret)
	assert.NotContains(f.t, string(body), "foreign-consumer-token")
	if f.method == "vertex_service_account" {
		assert.Equal(f.t, "us-east5-aiplatform.googleapis.com", r.Host)
		assert.Equal(f.t, "/v1/projects/selected-project/locations/us-east5/publishers/anthropic/models/claude-sonnet-4-6:streamRawPredict", r.URL.Path)
		assert.Equal(f.t, "Bearer "+f.access, r.Header.Get("Authorization"))
		assert.Contains(f.t, string(body), `"anthropic_version":"vertex-2023-10-16"`)
	} else {
		assert.Equal(f.t, "bedrock-runtime.eu-west-1.amazonaws.com", r.Host)
		account := &service.Account{Type: service.AccountTypeBedrock, Credentials: map[string]any{"aws_region": "eu-west-1"}}
		model, ok := service.ResolveBedrockModelID(account, "claude-sonnet-4-6")
		require.True(f.t, ok)
		assert.Equal(f.t, service.BuildBedrockURL("eu-west-1", model, true), "https://"+r.Host+r.URL.RequestURI())
		assert.Contains(f.t, string(body), `"anthropic_version":"bedrock-2023-05-31"`)
		if f.method == "bedrock_api_key" {
			assert.Equal(f.t, "Bearer "+f.secret, r.Header.Get("Authorization"))
		} else {
			assert.Equal(f.t, f.session, r.Header.Get("X-Amz-Security-Token"))
			signed, err := http.NewRequest(http.MethodPost, "https://"+r.Host+r.URL.RequestURI(), bytes.NewReader(body))
			require.NoError(f.t, err)
			signed.Header = r.Header.Clone()
			signed.Header.Del("Authorization")
			_, signedPart, present := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
			require.True(f.t, present)
			signedNames, _, present := strings.Cut(signedPart, ",")
			require.True(f.t, present)
			allowedHeaders := make(map[string]bool)
			for _, name := range strings.Split(signedNames, ";") {
				allowedHeaders[name] = true
			}
			require.True(f.t, allowedHeaders["host"] && allowedHeaders["content-type"] && allowedHeaders["x-amz-date"])
			for name := range signed.Header {
				// The HTTP client adds hop headers after the native signature.
				if !allowedHeaders[strings.ToLower(name)] {
					signed.Header.Del(name)
				}
			}
			instant, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
			require.NoError(f.t, err)
			digest := sha256.Sum256(body)
			require.NoError(f.t, v4.NewSigner().SignHTTP(r.Context(), aws.Credentials{AccessKeyID: f.access, SecretAccessKey: f.secret, SessionToken: f.session}, signed, hex.EncodeToString(digest[:]), "bedrock", "eu-west-1", instant))
			assert.Equal(f.t, signed.Header.Get("Authorization"), r.Header.Get("Authorization"), "actual signature binds request body, host, region, session and signing time")
		}
	}
	if f.phase.Load() == 2 {
		assert.Contains(f.t, string(body), "synthetic file contents")
	}
	if f.phase.Load() == 3 && f.method == "vertex_service_account" {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "private-cloud-error")
		return
	}
	if f.method == "vertex_service_account" {
		nativeConnectionWriteProtocol(f.t, w, &nativeConnectionWireCase{model: "claude-sonnet-4-6", protocol: service.APIProtocolAnthropic}, f.phase.Load() == 2)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	sse := httptest.NewRecorder()
	nativeConnectionWriteProtocol(f.t, sse, &nativeConnectionWireCase{model: "claude-sonnet-4-6", protocol: service.APIProtocolAnthropic}, f.phase.Load() == 2)
	scanner := bufio.NewScanner(strings.NewReader(sse.Body.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		frame := nativeCloudEventFrame(f.t, []byte(strings.TrimPrefix(line, "data: ")))
		if f.phase.Load() == 3 {
			frame[len(frame)-1] ^= 0xff
		}
		_, _ = w.Write(frame)
	}
	require.NoError(f.t, scanner.Err())
}

func TestGatewayNativeConnectionPostgresCloudSigningAndStreams(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_CLOUD_CONNECTION_TEST_DSN", "native_cloud_connection_test_")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody("cloud-wire", map[string][]byte{"cloud-wire": bytes.Repeat([]byte{0x66}, 32)})
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var current atomic.Pointer[nativeCloudWireFixture]
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().serve(w, r) }))
	defer server.Close()
	baseTransport, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	transport := baseTransport.Clone()
	transport.DisableKeepAlives = true
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "bedrock-runtime.eu-west-1.amazonaws.com:443" && address != "us-east5-aiplatform.googleapis.com:443" && address != "oauth2.googleapis.com:443" {
			return nil, service.ErrGatewayNativeIdentity
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionTLSUpstream{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	dispatchers, err := service.NewGatewayNativeCloudDispatchers(gateway, transport)
	require.NoError(t, err)
	ownerCtx, err := service.WithGatewayNativeConsumer(ctx, "cloud-consumer")
	require.NoError(t, err)
	ownerCtx, err = service.WithGatewayNativeOAuthOwner(ownerCtx, "cloud-owner")
	require.NoError(t, err)
	owner := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if ctx.Err() != nil || !service.GatewayConnectionScopeAuthorized(ctx, scope) || scope.Owner != "cloud-owner" {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	for _, variant := range []string{"aws_long_lived", "aws_session", "bedrock_key", "vertex_service_account"} {
		t.Run(variant, func(t *testing.T) {
			f := &nativeCloudWireFixture{t: t, variant: variant, key: key, method: "aws_sigv4", profile: "anthropic-bedrock-messages-v1", base: "https://bedrock-runtime.eu-west-1.amazonaws.com", secret: "cloud-secret-" + uuid.NewString(), access: "AKIA" + strings.ReplaceAll(uuid.NewString(), "-", "")}
			input := map[string]string{"access_key_id": f.access, "secret_access_key": f.secret, "region": "eu-west-1"}
			if variant == "aws_session" {
				f.session = "cloud-session-" + uuid.NewString()
				input["session_token"] = f.session
			} else if variant == "bedrock_key" {
				f.method = "bedrock_api_key"
				input = map[string]string{"api_key": f.secret, "region": "eu-west-1"}
			} else if variant == "vertex_service_account" {
				f.method, f.profile, f.base, f.access = variant, "anthropic-vertex-messages-v1", "https://us-east5-aiplatform.googleapis.com", "google-access-"+uuid.NewString()
				raw, err := json.Marshal(map[string]string{"type": "service_account", "project_id": "source-project", "private_key_id": "fixture-key", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "client_email": "fixture@source-project.iam.gserviceaccount.com", "token_uri": "https://foreign.invalid/must-not-receive-signed-assertion"})
				require.NoError(t, err)
				input = map[string]string{"service_account_json": string(raw), "project_id": "selected-project", "location": "us-east5"}
			}
			current.Store(f)
			method, err := service.NewGatewayNativeCloudConnectionMethod(f.profile, f.method, "cloud-"+uuid.NewString(), service.GatewayConnectionMount{BaseURL: f.base, Model: "claude-sonnet-4-6", QualificationRef: "cloud-fixture"})
			require.NoError(t, err)
			var route *service.GatewayNativeRoute
			cleanup := func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
				return service.GatewayNativeOAuthCleanupAuthority{Owner: "cloud-owner", LeaseExpiresAt: time.Now().Add(time.Minute), Native: route}, nil
			}
			native, err := service.NewGatewayNativeConnections(service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: bytes.Repeat([]byte{0x67}, 32), Methods: []service.GatewayNativeConnectionMethodAdapter{method}, Connect: owner, Refresh: owner, Cleanup: cleanup, Dispatchers: dispatchers, Gateway: gateway})
			require.NoError(t, err)
			in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "cloud-consumer", Owner: "cloud-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "cloud-" + uuid.NewString(), Connection: method.Selection()}
			created, err := native.Begin(ownerCtx, in, input)
			require.NoError(t, err)
			require.Equal(t, "completed", created.State)
			require.Equal(t, "cloud_identity", created.Credential.CredentialPolicy)
			require.Equal(t, "none", created.Credential.RefreshPolicy)
			require.Zero(t, f.tokens.Load(), "syntax/custody setup never probes cloud credentials")
			published, err := native.Describe(ownerCtx, in)
			require.NoError(t, err)
			require.NotNil(t, published.Native)
			route = &published.Native.GatewayNativeRoute
			request := map[string]any{"model": "claude-sonnet-4-6", "input": "Review file.go", "store": false, "stream": true, "max_output_tokens": 512, "tools": []any{map[string]any{"type": "function", "name": "read_source", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}}}}}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			f.phase.Store(1)
			first, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.NoError(t, err)
			require.True(t, entered)
			output := nativeConnectionCompletedOutput(t, first.Body.String())
			request["input"] = append(output, json.RawMessage(`{"type":"function_call_output","call_id":"call_review","output":"synthetic file contents"}`))
			body, err = json.Marshal(request)
			require.NoError(t, err)
			f.phase.Store(2)
			second, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.NoError(t, err)
			require.True(t, entered)
			require.NotEmpty(t, nativeConnectionCompletedOutput(t, second.Body.String()))
			f.phase.Store(3)
			failed, entered, err := nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.ErrorIs(t, err, service.ErrGatewayNativeEffectUnknown)
			require.True(t, entered)
			require.NotContains(t, failed.Body.String(), "private-cloud-error")
			require.EqualValues(t, 3, f.inference.Load(), "CRC/provider failures cannot retry or change cloud protocol")
			maintained, err := native.Maintain(ownerCtx, in)
			require.NoError(t, err)
			require.Equal(t, "idle", maintained.State)
			_, err = native.Cleanup(ownerCtx, in, service.GatewayNativeOAuthCleanupRequest{Scope: in.Scope, Operation: in.Operation, Action: "erase", CleanupRef: "cloud-cleanup", CleanupToken: "cloud-cleanup-token"})
			require.NoError(t, err)
			_, entered, err = nativeConnectionForwardFixture(t, ownerCtx, custody, native, gateway, in, *route, body)
			require.Error(t, err)
			require.False(t, entered)
			require.EqualValues(t, 3, f.inference.Load())
			var durable string
			require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_build_object('account',(SELECT to_jsonb(a) FROM accounts a WHERE id=$1),'intent',(SELECT to_jsonb(i) FROM gateway_oauth_connect_intents i WHERE operation_ref=$2))::text`, route.AccountID, in.Operation).Scan(&durable))
			require.NotContains(t, durable, f.secret)
			require.NotContains(t, durable, f.access)
			require.NotContains(t, durable, "BEGIN RSA PRIVATE KEY")
			if f.session != "" {
				require.NotContains(t, durable, f.session)
			}
		})
	}
}
