//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/native_connection_g0_rows.json
var nativeConnectionJoinedG0 []byte

//go:embed testdata/native_connection_extension_rows.json
var nativeConnectionJoinedExtensionOracle []byte

// The proxy connects only to the real Gateway authority after discovery has
// completed. No test owner grants replace the callback authorization protocol.
type nativeConnectionJoinedOwner struct{ current atomic.Pointer[authority] }

func (o *nativeConnectionJoinedOwner) AuthorizeNativeOAuthOwner(ctx context.Context, consumer, operation, account, generation string) (string, error) {
	if a := o.current.Load(); a != nil {
		return a.AuthorizeNativeOAuthOwner(ctx, consumer, operation, account, generation)
	}
	return "", ErrDenied
}
func (o *nativeConnectionJoinedOwner) AuthorizeNativeOAuthRefreshOwner(ctx context.Context, consumer, operation, account, generation string) (string, error) {
	if a := o.current.Load(); a != nil {
		return a.AuthorizeNativeOAuthRefreshOwner(ctx, consumer, operation, account, generation)
	}
	return "", ErrDenied
}
func (o *nativeConnectionJoinedOwner) AuthorizeNativeOAuthCustodyOwner(ctx context.Context, consumer, operation, account, generation string) (string, error) {
	if a := o.current.Load(); a != nil {
		return a.AuthorizeNativeOAuthCustodyOwner(ctx, consumer, operation, account, generation)
	}
	return "", ErrDenied
}
func (o *nativeConnectionJoinedOwner) AuthorizeNativeOAuthCleanup(ctx context.Context, in service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
	if a := o.current.Load(); a != nil {
		return a.AuthorizeNativeOAuthCleanup(ctx, in)
	}
	return service.GatewayNativeOAuthCleanupAuthority{}, ErrDenied
}
func (o *nativeConnectionJoinedOwner) AuthorizeNativeOAuthCleanupOwner(ctx context.Context, in service.GatewayNativeOAuthCleanupRequest) (string, error) {
	if a := o.current.Load(); a != nil {
		return a.AuthorizeNativeOAuthCleanupOwner(ctx, in)
	}
	return "", ErrDenied
}
func (o *nativeConnectionJoinedOwner) AuthorizeNativeAccountLimits(ctx context.Context, consumer string, in service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
	if a := o.current.Load(); a != nil {
		return a.AuthorizeNativeAccountLimits(ctx, consumer, in)
	}
	return service.GatewayNativeRoute{}, ErrDenied
}

func (o *nativeConnectionJoinedOwner) AuthorizeNativeAccountInfo(ctx context.Context, consumer string, in service.GatewayAccountLimitsInput) (service.GatewayNativeRoute, error) {
	if a := o.current.Load(); a != nil {
		return a.AuthorizeNativeAccountInfo(ctx, consumer, in)
	}
	return service.GatewayNativeRoute{}, ErrDenied
}

type nativeConnectionJoinedUpstream struct{ client *http.Client }

func (u *nativeConnectionJoinedUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(request)
}
func (u *nativeConnectionJoinedUpstream) DoWithTLS(request *http.Request, proxy string, account int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, account, concurrency)
}

// One HTTP/PG/SDK/RR harness covers the independent G0 inventory. The Express
// batch changes only Vertex's trusted mount; one profile cannot advertise two
// contradictory immutable endpoint configurations in the same catalog.
func TestNativeConnectionsJoinedPrivateHTTPPostgres(t *testing.T) {
	dsn, runner := os.Getenv("GATEWAY_NATIVE_CONNECTION_JOINED_DSN"), os.Getenv("GATEWAY_NATIVE_CONNECTION_JOINED_RUNNER")
	if dsn == "" || runner == "" {
		t.Skip("NOT_RUN: explicit disposable joined native/Gateway/SDK/RR fixture not configured")
	}
	require.Equal(t, "disposable_pg17_10", os.Getenv("AG_NATIVE_CONNECTION_JOINED_ALLOW"))
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, u.Scheme)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.Equal(t, "postgres", u.User.Username())
	_, password := u.User.Password()
	require.False(t, password)
	require.True(t, strings.HasPrefix(u.Path, "/native_connection_joined_"))
	require.Equal(t, "sslmode=disable", u.RawQuery)
	require.Empty(t, u.Fragment)
	require.True(t, filepath.IsAbs(runner))
	runnerBytes, err := os.ReadFile(runner)
	require.NoError(t, err)
	runnerDigest := sha256.Sum256(runnerBytes)
	require.Equal(t, os.Getenv("GATEWAY_NATIVE_CONNECTION_JOINED_RUNNER_SHA256"), hex.EncodeToString(runnerDigest[:]))
	node, loader, artifacts := os.Getenv("GATEWAY_NATIVE_CONNECTION_JOINED_NODE"), os.Getenv("GATEWAY_NATIVE_CONNECTION_JOINED_LOADER"), os.Getenv("GATEWAY_NATIVE_CONNECTION_JOINED_ARTIFACTS")
	for _, path := range []string{node, loader, artifacts} {
		require.True(t, filepath.IsAbs(path))
	}
	require.NoError(t, os.MkdirAll(artifacts, 0700))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var version string
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_setting('server_version')").Scan(&version))
	require.Regexp(t, `^17\.10(?:$| )`, version)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'").Scan(&count))
	require.Zero(t, count, "never reset a populated native database")
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer func() { _ = client.Close() }()
	repo := repository.NewAccountRepository(client, db, nil)
	physical, ok := repo.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	adminRepo, ok := repo.(service.AdminAccountRepository)
	require.True(t, ok)
	adminSvc := service.NewAdminService(nil, nil, nil, adminRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	journal, err := repository.NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	cfg := fixtureConfig()
	consumer, control, execution, cleanupToken, callback := "joined-"+uuid.NewString(), "control-"+uuid.NewString(), "execution-"+uuid.NewString(), "cleanup-"+uuid.NewString(), "callback-"+uuid.NewString()
	cfg.Peers = []ControlPeer{{ConsumerID: consumer, Role: "management", Credential: control}, {ConsumerID: consumer, Role: "execution", Credential: execution}, {ConsumerID: consumer, Role: "cleanup", Credential: cleanupToken}}
	cfg.Profile.RequestBytes, cfg.Profile.OutputBytes, cfg.Profile.Tokens, cfg.Profile.ProviderTokenUpperBound = 1<<20, 1<<20, 16384, 16384
	batch := os.Getenv("GATEWAY_NATIVE_CONNECTION_JOINED_BATCH")
	if batch == "" {
		batch = "standard"
	}
	require.Contains(t, []string{"standard", "express", "extensibility"}, batch)
	families := []nativeConnectionJoinedFamily{}
	if batch == "standard" {
		families = append(families, newNativeConnectionJoinedAPIKeyFamily(t, &cfg), newNativeConnectionJoinedCloudFamily(t, &cfg),
			newNativeConnectionJoinedCodexFamily(t, &cfg), newNativeConnectionJoinedSubscriptionFamily(t, &cfg))
	}
	if batch == "extensibility" {
		families = append(families, newNativeConnectionJoinedExtensionFamily(t, &cfg))
	} else {
		families = append(families, newNativeConnectionJoinedGeminiFamily(t, &cfg, batch == "express"))
	}
	profiles, profileIDs, rows := []gatewaytransport.QualifiedProfile{}, []string{}, []nativeConnectionJoinedRow{}
	peersByID, protocols, hosts, private := map[string]*nativeConnectionJoinedPeer{}, map[string]*nativeConnectionJoinedProtocol{}, map[string]bool{}, map[string]string{}
	for _, family := range families {
		for _, p := range family.Profiles {
			for _, old := range profiles {
				require.NotEqual(t, old.Profile, p.Profile, "one frozen mount per profile")
			}
			profiles, profileIDs = append(profiles, p), append(profileIDs, p.Profile)
			if p.Profile == service.GatewayMiMoResponsesProfile {
				cfg.Profile = ProfileConfig{BaseURL: p.BaseURL, Model: p.Model, QualificationRef: p.QualificationRef, RequestBytes: p.RequestBytes, OutputBytes: p.OutputBytes, Tokens: p.Tokens, ProviderTokenUpperBound: p.ProviderTokenUpperBound}
			}
		}
		for _, peer := range family.Peers {
			require.Nil(t, peersByID[peer.Row.ID], "duplicate G0 row")
			require.Equal(t, nativeConnectionJoinedRowID(peer.Row.ProfileID, peer.Row.MethodID, peer.Row.Variant), peer.Row.ID)
			peersByID[peer.Row.ID], protocols[peer.Row.ID] = peer, &nativeConnectionJoinedProtocol{}
			rows = append(rows, peer.Row)
		}
		for _, host := range family.Hosts {
			hosts[host] = true
		}
		for name, value := range family.Private {
			require.Empty(t, private[name])
			private[name] = value
		}
	}
	var inventory struct {
		Rows []struct {
			ProfileID string `json:"profileId"`
			MethodID  string `json:"methodId"`
			Variant   string `json:"credentialVariant"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(nativeConnectionJoinedG0, &inventory))
	require.Len(t, inventory.Rows, 53)
	expected := map[string]bool{}
	if batch == "extensibility" {
		var extension struct {
			Contract string                                                           `json:"contract"`
			Rows     []struct{ ProfileID, MethodID, Variant, Provider, Model string } `json:"rows"`
		}
		require.NoError(t, json.Unmarshal(nativeConnectionJoinedExtensionOracle, &extension))
		require.Equal(t, "native-provider-extension-oracle/v1", extension.Contract)
		require.Len(t, extension.Rows, 1)
		require.Len(t, rows, 1)
		for _, row := range extension.Rows {
			expected[nativeConnectionJoinedRowID(row.ProfileID, row.MethodID, row.Variant)] = true
			require.Equal(t, row.Provider, rows[0].Provider)
			require.Equal(t, row.Model, rows[0].Model)
		}
		for _, row := range inventory.Rows {
			require.False(t, expected[nativeConnectionJoinedRowID(row.ProfileID, row.MethodID, row.Variant)], "synthetic extension must not replace a G0 variant")
		}
	} else {
		for _, row := range inventory.Rows {
			if (row.Variant == "express_projectless") != (batch == "express") {
				continue
			}
			expected[nativeConnectionJoinedRowID(row.ProfileID, row.MethodID, row.Variant)] = true
		}
	}
	require.Len(t, rows, len(expected), "all G0 variants must be represented, including typed blockers")
	for _, row := range rows {
		require.True(t, expected[row.ID], "unexpected G0 row %s", row.ID)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Blocker == "" && rows[j].Blocker != "" })
	key, err := base64.StdEncoding.DecodeString(cfg.Custody.Keys[0].Key)
	require.NoError(t, err)
	custody, err := service.NewGatewayNativeCredentialCustody(cfg.Custody.ActiveKeyID, map[string][]byte{cfg.Custody.ActiveKeyID: key})
	require.NoError(t, err)
	clear(key)
	var active atomic.Pointer[nativeConnectionJoinedPeer]
	var unexpected, providerCalls atomic.Int64
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		peer := active.Load()
		if peer == nil || r.TLS == nil {
			unexpected.Add(1)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if peer.Handle != nil && peer.Handle(w, r) {
			return
		}
		if protocols[peer.Row.ID].serve(w, r, peer) != nil {
			unexpected.Add(1)
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer provider.Close()
	tlsTransport := provider.Client().Transport.(*http.Transport).Clone()
	tlsTransport.Proxy, tlsTransport.DisableKeepAlives = nil, true
	tlsTransport.TLSClientConfig.ServerName = "example.com"
	tlsTransport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if !hosts[address] {
			unexpected.Add(1)
			return nil, ErrDenied
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, provider.Listener.Addr().String())
	}
	defer tlsTransport.CloseIdleConnections()
	upstream, err := service.NewGatewayNativeLifetimeUpstream(&nativeConnectionJoinedUpstream{&http.Client{Transport: nativeConnectionJoinedExtensionTransport(batch, tlsTransport), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	require.NoError(t, err)
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, gateway.SetGatewayOpenRouterModels(service.NewGatewayOpenRouterModels(tlsTransport)))
	require.NoError(t, gateway.SetGatewayLiveModels(service.NewGatewayLiveModels(tlsTransport)))
	owners := &nativeConnectionJoinedOwner{}
	oauth := &privateOAuth{owners: owners, cleanup: owners.AuthorizeNativeOAuthCleanup}
	materialKey, err := connectionMaterialKey(cfg)
	require.NoError(t, err)
	defer clear(materialKey)
	build := nativeConnectionJoinedBuild{Context: ctx, DB: db, Repository: repo, Journal: journal, Custody: custody, Gateway: gateway, Config: &cfg, Owners: owners, Transport: tlsTransport, OAuth: oauth, MaterialKey: materialKey}
	methods, dispatchers := []service.GatewayNativeConnectionMethodAdapter{}, map[string]service.GatewayNativeConnectionDispatchAdapter{}
	for _, family := range families {
		composed, dispatch, err := family.Compose(build)
		require.NoError(t, err)
		methods = append(methods, composed...)
		for name, adapter := range dispatch {
			_, duplicate := dispatchers[name]
			require.False(t, duplicate, "duplicate dispatch family %s", name)
			dispatchers[name] = adapter
		}
	}
	authorizeEntry := func(refresh bool) service.GatewayNativeOAuthEntryAuthorizer {
		return func(ctx context.Context, scope service.GatewayNativeCredentialScope, operation string) error {
			read := owners.AuthorizeNativeOAuthOwner
			if refresh {
				read = owners.AuthorizeNativeOAuthRefreshOwner
			}
			owner, err := read(ctx, scope.Consumer, operation, scope.Account, scope.Generation)
			if err != nil || owner != scope.Owner {
				return ErrDenied
			}
			return nil
		}
	}
	nativeDependencies := service.GatewayNativeConnectionsDependencies{Journal: journal, Repository: physical, Custody: custody, MaterialKey: materialKey, Methods: methods,
		Connect: authorizeEntry(false), Refresh: authorizeEntry(true), Cleanup: owners.AuthorizeNativeOAuthCleanup, Dispatchers: dispatchers, Gateway: gateway, Codex: oauth.dispatch}
	native, err := service.NewGatewayNativeConnections(nativeDependencies)
	require.NoError(t, err)
	oauth.nativeConnections = native
	require.NoError(t, composePrivateModels(oauth, owners, gateway, db, custody))
	require.NoError(t, composePrivateLimitsV2(oauth, db, owners))
	require.NoError(t, composePrivateAccountInfo(oauth, db, owners))
	peers := authorize(cfg.Peers)
	enrollment := gatewaytransport.Enrollment{OriginRef: "joined-connections-origin", EngineIncarnation: uuid.NewString(), QualificationRef: cfg.Profile.QualificationRef}
	adapters := privateNativeConnectionRecoveryAdapters(native, dispatchers)
	oauth.connections, err = composePrivateConnectionBindings(cfg, profiles, enrollment, oauth, custody, peers, adapters...)
	require.NoError(t, err)
	catalog, err := privateCatalogOnly(privateAccountCapabilitiesForProfiles(profiles, oauth), oauth.connections.capabilities, peers)
	require.NoError(t, err)
	handler := &privateStartupHandler{}
	handler.install(catalog)
	server := httptest.NewServer(handler)
	defer server.Close()
	exchangePath := filepath.Join(artifacts, "private-exchange.json")
	fixture := map[string]any{"nativeOrigin": server.URL, "consumerId": consumer, "managementCredential": control, "executionCredential": execution, "cleanupCredential": cleanupToken,
		"callbackCredential": callback, "originRef": enrollment.OriginRef, "engineIncarnation": enrollment.EngineIncarnation, "qualificationRef": enrollment.QualificationRef,
		"profiles": profileIDs, "rows": rows, "exchangePath": exchangePath, "private": private, "batch": batch, "expectedInventoryRows": 53}
	if batch == "extensibility" {
		fixture["expectedExtensionRows"] = 1
	}
	raw, err := json.Marshal(fixture)
	require.NoError(t, err)
	fixturePath := filepath.Join(artifacts, "private-fixture.json")
	require.NoError(t, os.WriteFile(fixturePath, raw, 0600))
	defer func() { _ = os.Remove(fixturePath); _ = os.Remove(exchangePath) }()
	cmd := exec.CommandContext(ctx, node, "--import", loader, runner)
	cmd.Env = append(os.Environ(), "AG_NATIVE_CONNECTION_JOINED_FIXTURE="+fixturePath)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	diagnostics, err := os.OpenFile(filepath.Join(artifacts, "gateway-diagnostics.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer func() { _ = diagnostics.Close() }()
	cmd.Stderr = diagnostics
	require.NoError(t, cmd.Start())
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	transcript, err := os.OpenFile(filepath.Join(artifacts, "joined-transcript.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer func() { _ = transcript.Close() }()
	scanner := bufio.NewScanner(io.TeeReader(stdout, transcript))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	require.True(t, scanner.Scan(), "Gateway startup frame missing")
	var ready struct {
		Phase, CallbackOrigin, ReviewSchemaSHA256 string
		ReviewSchema                              json.RawMessage
	}
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &ready))
	require.Equal(t, "ready", ready.Phase)
	schemaDigest, err := hex.DecodeString(ready.ReviewSchemaSHA256)
	require.NoError(t, err)
	require.Len(t, schemaDigest, sha256.Size, "pinned Action review schema proof is required")
	require.NotEmpty(t, ready.ReviewSchema)
	require.LessOrEqual(t, len(ready.ReviewSchema), 64<<10)
	require.Equal(t, ready.ReviewSchemaSHA256, nativeConnectionJoinedSchemaDigest(ready.ReviewSchema), "source oracle must use the exact pinned Action schema")
	anthropicSchema := nativeConnectionJoinedAnthropicSchemaDigest(ready.ReviewSchema)
	require.NotEmpty(t, anthropicSchema, "independent Anthropic schema oracle must preserve the original constraints")
	for _, protocol := range protocols {
		protocol.expectedSchema = ready.ReviewSchemaSHA256
		protocol.expectedAnthropicSchema = anthropicSchema
	}
	a, err := newAuthority(AuthorityConfig{Origin: ready.CallbackOrigin, Credential: callback}, nil)
	require.NoError(t, err)
	defer a.client.CloseIdleConnections()
	owners.current.Store(a)
	tcfg := gatewaytransport.Config{Gateway: gateway, Custody: custody, Connections: native, OAuth: oauth.dispatch, ClaudeOAuth: oauth.claude, GrokOAuth: oauth.grok,
		Authorize: peers, Enrollment: enrollment, Profile: cfg.qualifiedProfile(), MaxEntries: 16, CallbackTimeout: 5 * time.Second, IOTimeout: 5 * time.Second, CleanupTimeout: 5 * time.Second, EnvelopeBytes: 2 << 20, CallbackBytes: 65536}
	for _, p := range profiles {
		switch p.Profile {
		case service.GatewayMiMoResponsesProfile:
		case service.GatewayCodexOAuthResponsesProfile:
			tcfg.Codex = &p
		case service.GatewayClaudeOAuthMessagesProfile:
			tcfg.Claude = &p
		case service.GatewayGrokOAuthResponsesProfile:
			tcfg.Grok = &p
		default:
			tcfg.NativeProfiles = append(tcfg.NativeProfiles, p)
		}
	}
	a.compose(&tcfg)
	transport, err := gatewaytransport.New(ctx, tcfg)
	require.NoError(t, err)
	defer func() { _ = transport.Stop(context.Background()) }()
	handler.install(privateHandler(cfg.Profile, adminSvc, gateway, custody, transport, peers, oauth))
	require.NoError(t, json.NewEncoder(stdin).Encode(map[string]string{"phase": "continue"}))
	var terminal struct{ Phase, Contract, RowID, AccountRef, Generation string }
	reconnectCustody, reconnectErasure := map[string]bool{}, map[string]bool{}
	var nativeServiceRecompositions int
	var withdrawnCapabilities *service.GatewayAccountCapabilitiesV2
	var nativeSecretScans int
	checkNativeSentinels := func() {
		if batch != "extensibility" {
			return
		}
		for _, table := range []string{"accounts", "gateway_oauth_connect_intents", "gateway_oauth_refresh_attempts"} {
			var encoded string
			require.NoError(t, db.QueryRowContext(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(r)),'[]'::jsonb)::text FROM "+table+" r").Scan(&encoded))
			for _, name := range []string{"extensionSentinelFirst", "extensionSentinelReplacement"} {
				require.NotEmpty(t, private[name])
				require.False(t, strings.Contains(encoded, private[name]), "provider credential must remain absent from native durable plaintext")
			}
		}
		nativeSecretScans++
	}
	for scanner.Scan() {
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &terminal))
		switch terminal.Phase {
		case "case":
			peer := peersByID[terminal.RowID]
			require.NotNil(t, peer, "unknown inventory row")
			active.Store(peer)
			if peer.Select != nil {
				peer.Select()
			}
			require.NoError(t, json.NewEncoder(stdin).Encode(map[string]string{"phase": "case-ready", "rowId": terminal.RowID}))
		case "reconnect-custody":
			peer := active.Load()
			require.NotNil(t, peer)
			require.Equal(t, peer.Row.ID, terminal.RowID)
			require.True(t, peer.Row.ReconnectCustody)
			require.False(t, reconnectCustody[terminal.RowID])
			requireNativeConnectionJoinedReconnect(t, ctx, build, peer.Row, consumer, terminal.AccountRef, terminal.Generation)
			reconnectCustody[terminal.RowID] = true
			require.NoError(t, json.NewEncoder(stdin).Encode(map[string]string{"phase": "reconnect-custody-verified", "rowId": terminal.RowID}))
		case "reconnect-erasure":
			require.Equal(t, active.Load().Row.ID, terminal.RowID)
			require.True(t, reconnectCustody[terminal.RowID])
			require.False(t, reconnectErasure[terminal.RowID])
			requireNativeConnectionJoinedErasure(t, ctx, db)
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM gateway_oauth_connect_intents WHERE material_envelope <> ''`).Scan(&count))
			require.Zero(t, count, "retirement erased every saved input envelope")
			reconnectErasure[terminal.RowID] = true
			require.NoError(t, json.NewEncoder(stdin).Encode(map[string]string{"phase": "reconnect-erasure-verified", "rowId": terminal.RowID}))
		case "authorize":
			peer := active.Load()
			require.NotNil(t, peer)
			require.Equal(t, peer.Row.ID, terminal.RowID)
			require.NotNil(t, peer.ObserveAuthorizeURL)
			info, err := os.Lstat(exchangePath)
			require.NoError(t, err)
			require.True(t, info.Mode().IsRegular() && info.Mode().Perm() == 0600 && info.Size() <= 16384)
			raw, err := os.ReadFile(exchangePath)
			require.NoError(t, err)
			var observed struct {
				RowID        string `json:"rowId"`
				AuthorizeURL string `json:"authorizationURL"`
			}
			require.NoError(t, json.Unmarshal(raw, &observed))
			require.Equal(t, terminal.RowID, observed.RowID)
			fields, err := peer.ObserveAuthorizeURL(observed.AuthorizeURL)
			require.NoError(t, err, "native authorize URL failed source peer validation")
			completion, err := json.Marshal(map[string]any{"rowId": terminal.RowID, "fields": fields})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(exchangePath, completion, 0600))
			require.NoError(t, json.NewEncoder(stdin).Encode(map[string]string{"phase": "authorization-ready", "rowId": terminal.RowID}))
		case "pending-recovery":
			require.Equal(t, "extensibility", batch)
			require.Zero(t, nativeServiceRecompositions)
			require.NotNil(t, active.Load())
			require.Equal(t, active.Load().Row.ID, terminal.RowID)
			// Reconstruct the native service, private routes and protected
			// transport from the same real PG journal and custody key. The
			// pending receipt remains authoritative after creation discovery
			// is withdrawn; no adapter state or transient PKCE is copied.
			nextOAuth := *oauth
			replacementDependencies := nativeDependencies
			replacementDependencies.Methods = nil
			replacementDependencies.Dispatchers = map[string]service.GatewayNativeConnectionDispatchAdapter{}
			replacementBuild := build
			replacementBuild.OAuth = &nextOAuth
			for _, family := range families {
				freshMethods, freshDispatchers, composeErr := family.Compose(replacementBuild)
				require.NoError(t, composeErr)
				replacementDependencies.Methods = append(replacementDependencies.Methods, freshMethods...)
				for familyID, adapter := range freshDispatchers {
					replacementDependencies.Dispatchers[familyID] = adapter
				}
			}
			native, err = service.NewGatewayNativeConnections(replacementDependencies)
			require.NoError(t, err)
			nextOAuth.nativeConnections = native
			require.NoError(t, composePrivateModels(&nextOAuth, owners, gateway, db, custody))
			require.NoError(t, composePrivateLimitsV2(&nextOAuth, db, owners))
			require.NoError(t, composePrivateAccountInfo(&nextOAuth, db, owners))
			nextOAuth.connections, err = composePrivateConnectionBindings(cfg, profiles, enrollment, &nextOAuth, custody, peers, privateNativeConnectionRecoveryAdapters(native, dispatchers)...)
			require.NoError(t, err)
			originalCapabilities := nextOAuth.connections.capabilities
			withdrawnCapabilities = &originalCapabilities
			nextOAuth.connections.capabilities.Profiles = []service.GatewayConnectionCapability{}
			require.NoError(t, transport.Stop(ctx))
			tcfg.Connections = native
			transport, err = gatewaytransport.New(ctx, tcfg)
			require.NoError(t, err)
			oauth = &nextOAuth
			handler.install(privateHandler(cfg.Profile, adminSvc, gateway, custody, transport, peers, oauth))
			nativeServiceRecompositions++
			require.NoError(t, json.NewEncoder(stdin).Encode(map[string]string{"phase": "pending-recovery-ready", "rowId": terminal.RowID}))
		case "pending-recovery-complete":
			require.Equal(t, "extensibility", batch)
			require.Equal(t, 1, nativeServiceRecompositions)
			require.NotNil(t, withdrawnCapabilities)
			require.Empty(t, oauth.connections.capabilities.Profiles)
			require.Equal(t, active.Load().Row.ID, terminal.RowID)
			checkNativeSentinels()
			oauth.connections.capabilities = *withdrawnCapabilities
			withdrawnCapabilities = nil
			require.NoError(t, json.NewEncoder(stdin).Encode(map[string]string{"phase": "pending-recovery-restored", "rowId": terminal.RowID}))
		}
	}
	require.NoError(t, scanner.Err())
	err = cmd.Wait()
	waited = true
	receipts := map[string]any{}
	for _, row := range rows {
		peer := peersByID[row.ID]
		counts := protocols[row.ID].counts()
		if peer.Counters != nil {
			counts["source"] = peer.Counters()
		}
		if row.ReconnectCustody {
			counts["reconnectCustodyVerified"], counts["reconnectErasureVerified"] = reconnectCustody[row.ID], reconnectErasure[row.ID]
		}
		receipts[row.ID] = counts
	}
	sourceContract, terminalContract := "native-connections-joined-source/v1", "native-connections-gateway-sdk-rr-joined/v1"
	if batch == "extensibility" {
		sourceContract, terminalContract = "native-provider-extension-source/v1", "native-provider-extension-gateway-sdk-rr/v1"
	}
	receiptFields := map[string]any{"contract": sourceContract, "batch": batch, "expectedInventoryRows": 53, "batchRows": len(rows),
		"childExitCode": cmd.ProcessState.ExitCode(), "nativeVersion": version, "runnerSHA256": hex.EncodeToString(runnerDigest[:]), "providerRequests": providerCalls.Load(), "unexpectedRequests": unexpected.Load(), "rows": receipts}
	if batch == "extensibility" {
		receiptFields["expectedExtensionRows"] = 1
		receiptFields["nativeServiceRecompositions"] = nativeServiceRecompositions
		receiptFields["creationCatalogRestored"] = nativeServiceRecompositions == 1 && withdrawnCapabilities == nil
		checkNativeSentinels()
		exportedFiles := 0
		for _, name := range []string{"gateway-diagnostics.log", "joined-transcript.jsonl", "gateway-rr-receipt.json", "native-joined.log"} {
			path := filepath.Join(artifacts, name)
			info, statErr := os.Stat(path)
			if os.IsNotExist(statErr) && name == "native-joined.log" {
				continue
			}
			require.NoError(t, statErr)
			require.LessOrEqual(t, info.Size(), int64(32<<20))
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			for _, key := range []string{"extensionSentinelFirst", "extensionSentinelReplacement"} {
				require.False(t, bytes.Contains(data, []byte(private[key])), "provider credential must not appear in exported fixture output")
			}
			exportedFiles++
		}
		receiptFields["credentialSecrecy"] = map[string]any{"nativeDurableChecks": nativeSecretScans, "exportedFiles": exportedFiles + 1, "sentinels": 2, "passed": true}
	}
	receipt, marshalErr := json.Marshal(receiptFields)
	require.NoError(t, marshalErr)
	if batch == "extensibility" {
		for _, name := range []string{"extensionSentinelFirst", "extensionSentinelReplacement"} {
			require.False(t, bytes.Contains(receipt, []byte(private[name])), "source receipt must not contain credentials")
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(artifacts, "native-receipt.json"), receipt, 0600))
	require.NoError(t, err, "joined runner failed; sanitized row/source receipt saved")
	require.Equal(t, "completed", terminal.Phase)
	require.Equal(t, terminalContract, terminal.Contract)
	if batch == "extensibility" {
		require.Equal(t, 1, nativeServiceRecompositions)
		require.Nil(t, withdrawnCapabilities)
		require.Equal(t, 2, nativeSecretScans)
	}
	require.Zero(t, unexpected.Load(), "no unqualified source endpoint, credential or protocol was accepted")
	for _, row := range rows {
		peer := peersByID[row.ID]
		if peer.Verify != nil {
			peer.Verify()
		}
		if !row.Execute {
			continue
		}
		counts := protocols[row.ID].counts()
		require.GreaterOrEqual(t, counts["dispatches"].(int64), int64(2), row.ID)
		require.Positive(t, counts["functionCalls"].(int64), row.ID)
		require.Positive(t, counts["functionOutputs"].(int64), row.ID)
		require.Positive(t, counts["schemaRequests"].(int64), row.ID)
		if counts["customDeclarations"].(int64) > 0 {
			require.Positive(t, counts["customCalls"].(int64), row.ID)
			require.Positive(t, counts["customOutputs"].(int64), row.ID)
		}
		if peer.Protocol == "gemini" || row.ProfileID == service.GatewayGeminiAPIKeyProfile || row.ReviewProof != nil {
			require.Positive(t, counts["signatureChecks"].(int64), row.ID)
		}
		if row.ReviewProof != nil {
			require.Positive(t, counts["thinkingRequests"].(int64), row.ID)
		}
	}
	requireNativeConnectionJoinedErasure(t, ctx, db)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM gateway_oauth_connect_intents WHERE material_envelope <> ''`).Scan(&count))
	require.Zero(t, count, "terminal enrollment and cleanup erased every saved input envelope")
}
