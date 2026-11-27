//go:build integration || gatewaycustody

package repository

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Add a second transaction lock only in this fixture to make the existing
// protected FOR SHARE participate in a real PostgreSQL deadlock.
type nonEntryConflictDB struct {
	*sql.DB
	ready chan struct{}
	key   int64
	armed atomic.Bool
}

func (d *nonEntryConflictDB) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	tx, err := d.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if !d.armed.CompareAndSwap(true, false) {
		return tx, nil
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL deadlock_timeout='25ms'"); err == nil {
		_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", d.key)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	close(d.ready)
	return tx, nil
}

type nonEntryConflictRepository struct {
	service.AccountRepository
	locked *accountRepository
}

func (r *nonEntryConflictRepository) LockGatewayNativeAccount(ctx context.Context, id int64) (*service.Account, func(), error) {
	return r.locked.LockGatewayNativeAccount(ctx, id)
}

func TestGatewayNativeNonEntryActualPostgresDeadlockBeforeProvider(t *testing.T) {
	nonEntryActualPostgres(t, false)
}

func TestGatewayNativeNonEntryJoinedPostgresProof(t *testing.T) {
	if os.Getenv("GATEWAY_NATIVE_NON_ENTRY_JOINED_RUNNER") == "" {
		t.Skip("NOT_RUN: explicit joined native/Gateway fixture required")
	}
	nonEntryActualPostgres(t, true)
}

func TestGatewayNativeNonEntryJoinedConsumerRetry(t *testing.T) {
	if os.Getenv("GATEWAY_NATIVE_NON_ENTRY_JOINED_RUNNER") == "" {
		t.Skip("NOT_RUN: explicit joined request-retry consumer fixture required")
	}
	nonEntryPostgresScenario(t, true, true)
}

func nonEntryActualPostgres(t *testing.T, joined bool) {
	t.Helper()
	nonEntryPostgresScenario(t, joined, false)
}

func nonEntryPostgresScenario(t *testing.T, joined, consumer bool) {
	t.Helper()
	dsnName := "GATEWAY_NATIVE_NON_ENTRY_TEST_DSN"
	if joined {
		dsnName = "GATEWAY_NATIVE_NON_ENTRY_JOINED_NATIVE_DSN"
	}
	dsn := custodyPGDSN(t, dsnName, "native_non_entry_")
	var ctx context.Context
	var cancel context.CancelFunc
	if consumer {
		// One original cutoff is shared with RR, Gateway and every handshake.
		deadline := time.Now().Add(2 * time.Minute).Truncate(time.Millisecond)
		ctx, cancel = context.WithDeadline(context.Background(), deadline)
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	}
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	custody, err := service.NewGatewayNativeCredentialCustody("non-entry", map[string][]byte{"non-entry": custodyPGBytes(t, 32)})
	require.NoError(t, err)
	scope := service.GatewayNativeCredentialScope{
		Consumer: "non-entry-consumer", Owner: "non-entry-owner", Account: "b1111111-1111-4111-8111-111111111111",
		Generation: "a1111111-1111-4111-8111-111111111111", Purpose: service.GatewayCredentialPurpose,
	}
	envelope, err := custody.Seal(scope, "synthetic-provider-key-for-non-entry")
	require.NoError(t, err)
	var providerCalls, priorCalls, currentCalls atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		if !consumer {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "{\"status\":\"completed\",\"output\":[]}")
			return
		}
		defer r.Body.Close()
		body, failure := io.ReadAll(io.LimitReader(r.Body, 4097))
		var request struct {
			Model           string `json:"model"`
			Stream          bool   `json:"stream"`
			Input           string `json:"input"`
			MaxOutputTokens int    `json:"max_output_tokens"`
		}
		if failure != nil || len(body) > 4096 || json.Unmarshal(body, &request) != nil ||
			request.Model != "synthetic-model" || !request.Stream ||
			request.MaxOutputTokens < 1 || request.MaxOutputTokens > 100 {
			http.Error(w, "invalid consumer request", http.StatusBadRequest)
			return
		}
		var responseID, text string
		switch request.Input {
		case "prior-turn":
			priorCalls.Add(1)
			responseID, text = "prior-paid-turn", "prior-paid-result"
		case "current-turn":
			currentCalls.Add(1)
			responseID, text = "current-paid-turn", "current-paid-result"
		default:
			http.Error(w, "unexpected consumer input", http.StatusBadRequest)
			return
		}
		event, failure := json.Marshal(map[string]any{
			"type":     "response.completed",
			"response": map[string]any{
				"id": responseID, "model": request.Model, "status": "completed",
				"output": []any{map[string]any{
					"id": responseID + "-message", "type": "message", "role": "assistant", "status": "completed",
					"content": []any{map[string]any{"type": "output_text", "text": text}},
				}},
				"usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5},
			},
		})
		if failure != nil {
			http.Error(w, "invalid consumer response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: "+string(event)+"\n\n")
	}))
	defer upstream.Close()
	account := custodyPGRow(scope, envelope, upstream.URL)
	require.NoError(t, accounts.Create(ctx, account))
	// The reused custody constructor intentionally creates an inactive row.
	// Activate only this fresh synthetic account so admission reaches its lock.
	_, err = db.ExecContext(ctx, "UPDATE accounts SET status='active' WHERE id=$1", account.ID)
	require.NoError(t, err)
	stored, err := accounts.GetByID(ctx, account.ID)
	require.NoError(t, err)
	route, err := service.GatewayNativeDescriptor(stored)
	require.NoError(t, err)

	conflict := &nonEntryConflictDB{DB: db, ready: make(chan struct{}), key: 10281127}
	var competing *sql.Tx
	defer func() {
		if competing != nil {
			_ = competing.Rollback()
		}
	}()
	armConflict := func() *sql.Tx {
		require.Nil(t, competing, "only the current logical request may be armed")
		var failure error
		competing, failure = db.BeginTx(ctx, nil)
		require.NoError(t, failure)
		_, failure = competing.ExecContext(ctx, "SET LOCAL deadlock_timeout='5s'")
		require.NoError(t, failure)
		var locked int64
		require.NoError(t, competing.QueryRowContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", account.ID).Scan(&locked))
		require.True(t, conflict.armed.CompareAndSwap(false, true))
		return competing
	}
	if !consumer {
		armConflict()
	}
	repo := &nonEntryConflictRepository{AccountRepository: accounts, locked: &accountRepository{sql: conflict}}
	transport, err := service.NewGatewayNativeLifetimeUpstream(&custodyPGHTTP{client: upstream.Client()})
	require.NoError(t, err)
	gateway := custodyPGGateway(repo, transport)
	if joined {
		nonEntryJoined(t, ctx, gateway, custody, scope, route, conflict, competing, &providerCalls, consumer, armConflict)
		if consumer {
			require.EqualValues(t, 1, priorCalls.Load(), "the prior paid turn is not replayed")
			require.EqualValues(t, 1, currentCalls.Load(), "only the successor enters the current provider turn")
		}
		return
	}
	callCtx, stopCall := context.WithTimeout(ctx, 15*time.Second)
	defer stopCall()
	life, err := service.NewGatewayNativeLifetime(callCtx, stopCall, func() {}, 4096)
	require.NoError(t, err)
	life.BindAccount(scope.Consumer, scope.Account, scope.Generation)
	require.True(t, life.Admit(time.Now().Add(15*time.Second)))
	principal, err := service.WithGatewayNativeConsumer(callCtx, scope.Consumer)
	require.NoError(t, err)
	control := service.WithGatewayNativeLifetime(service.WithGatewayNativeCustody(principal, custody), life)
	type outcome struct {
		entered bool
		err     error
	}
	result := make(chan outcome, 1)
	go func() {
		body := []byte("{\"model\":\"synthetic-model\",\"input\":\"fixture\",\"store\":false,\"service_tier\":\"default\"}")
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/private/native/v1/responses", bytes.NewReader(body))
		_, entered, failure := gateway.ForwardGatewayRoute(control, c, route, body)
		result <- outcome{entered, failure}
	}()
	select {
	case <-conflict.ready:
	case completed := <-result:
		t.Fatalf("forward ended before the protected SQL lock: %v", completed.err)
	case <-ctx.Done():
		t.Fatal("protected SQL lock did not start")
	}
	// The native transaction now owns the advisory lock and waits for the account;
	// this transaction owns the account and waits for that advisory lock.
	_, err = competing.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", conflict.key)
	require.NoError(t, err)
	require.NoError(t, competing.Rollback())
	completed := <-result
	require.False(t, completed.entered)
	require.ErrorIs(t, completed.err, service.ErrGatewayNativeIdentity)
	snapshot := life.Snapshot()
	require.Equal(t, service.GatewayNativeAdmissionStorageConflict, snapshot.NonEntryReason)
	require.True(t, snapshot.ContextDone && snapshot.ForwardingReturned && snapshot.BodyKnown && snapshot.BodyClosed)
	require.False(t, snapshot.Entered || snapshot.CloseFailed || snapshot.Completed)
	require.Zero(t, providerCalls.Load())
}

func nonEntryJoined(
	t *testing.T, ctx context.Context, gateway *service.OpenAIGatewayService,
	custody *service.GatewayNativeCredentialCustody, scope service.GatewayNativeCredentialScope,
	route service.GatewayNativeRoute, conflict *nonEntryConflictDB,
	competing *sql.Tx, providerCalls *atomic.Int32,
	consumer bool, armConflict func() *sql.Tx,
) {
	t.Helper()
	require.Equal(t, "disposable_pg17_10", os.Getenv("AG_NATIVE_NON_ENTRY_JOINED_ALLOW"))
	mode := os.Getenv("GATEWAY_NATIVE_NON_ENTRY_JOINED_MODE")
	require.Contains(t, []string{"private", "public"}, mode)
	runner, node := os.Getenv("GATEWAY_NATIVE_NON_ENTRY_JOINED_RUNNER"), os.Getenv("GATEWAY_NATIVE_NON_ENTRY_JOINED_NODE")
	require.True(t, filepath.IsAbs(runner) && filepath.IsAbs(node))
	runnerBytes, err := os.ReadFile(runner)
	require.NoError(t, err)
	digest := sha256.Sum256(runnerBytes)
	require.Equal(t, os.Getenv("GATEWAY_NATIVE_NON_ENTRY_JOINED_RUNNER_SHA256"), hex.EncodeToString(digest[:]))
	var mounted atomic.Pointer[gatewaytransport.Handler]
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler := mounted.Load()
		if handler == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer native.Close()
	var public atomic.Value
	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler := public.Load()
		if handler == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		handler.(http.Handler).ServeHTTP(w, r)
	}))
	defer publicServer.Close()
	fixture := map[string]any{
		"mode": mode, "consumerId": scope.Consumer, "accountRef": scope.Account,
		"ownerRef": scope.Owner, "operationId": "joined-non-entry-enrollment",
		"qualificationRef": "non-entry-joined", "nativeOrigin": native.URL, "nativePublicOrigin": publicServer.URL,
		"descriptor": map[string]any{
			"generation": route.Generation, "nativePhysicalIdentity": route.Generation,
			"nativeId": route.AccountID, "createdAt": route.CreatedAt.Format(time.RFC3339Nano),
			"profile": route.Profile, "baseURL": route.BaseURL, "model": route.Model,
		},
	}
	if consumer {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		fixture["scenario"] = "request-retry-consumer"
		fixture["deadline"] = deadline.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	raw, err := json.Marshal(fixture)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "native-non-entry-fixture.json")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	cmd := exec.CommandContext(ctx, node, runner)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "AG_NATIVE_NON_ENTRY_JOINED_FIXTURE="+path)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 16384)
	type joinedLine struct {
		bytes []byte
		ok    bool
		err   error
	}
	// Exactly one read is outstanding. The original authority cutoff bounds
	// every phase; EOF also rejects a child that exits before its next phase.
	readLine := func() <-chan joinedLine {
		result := make(chan joinedLine, 1)
		go func() {
			ok := scanner.Scan()
			result <- joinedLine{append([]byte(nil), scanner.Bytes()...), ok, scanner.Err()}
		}()
		return result
	}
	awaitLine := func(pending <-chan joinedLine) joinedLine {
		select {
		case line := <-pending:
			require.NoError(t, line.err)
			return line
		case <-ctx.Done():
			t.Fatal("joined native authority deadline elapsed while awaiting child")
			return joinedLine{}
		}
	}
	first := awaitLine(readLine())
	require.True(t, first.ok, "joined Gateway ready response missing")
	var ready struct {
		Phase             string `json:"phase"`
		CallbackOrigin    string `json:"callbackOrigin"`
		EngineIncarnation string `json:"engineIncarnation"`
	}
	require.NoError(t, json.Unmarshal(first.bytes, &ready))
	require.Equal(t, "ready", ready.Phase)
	require.Regexp(t, "^http://127[.]0[.]0[.]1:[0-9]+$", ready.CallbackOrigin)
	require.Regexp(t, "^[0-9a-f-]{36}$", ready.EngineIncarnation)
	const executionToken = "joined-test-non-entry-execution"
	const cleanupToken = "joined-test-non-entry-cleanup"
	const callbackToken = "joined-test-non-entry-callback"
	callback := func(callCtx context.Context, name string, body any) error {
		raw, failure := json.Marshal(body)
		if failure != nil {
			return service.ErrGatewayNativeIdentity
		}
		request, failure := http.NewRequestWithContext(callCtx, http.MethodPost, ready.CallbackOrigin+"/private/native/v1/"+name, bytes.NewReader(raw))
		if failure != nil {
			return service.ErrGatewayNativeIdentity
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+callbackToken)
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, failure := client.Do(request)
		if failure != nil {
			return service.ErrGatewayNativeIdentity
		}
		defer response.Body.Close()
		var result struct {
			OK bool `json:"ok"`
		}
		if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil || !result.OK {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	bound := func(consumer string, proof gatewaytransport.Proof) map[string]any {
		return map[string]any{"consumerId": consumer, "proof": proof}
	}
	cfg := gatewaytransport.Config{
		Gateway: gateway, Custody: custody,
		Enrollment: gatewaytransport.Enrollment{OriginRef: "non-entry-joined-origin", EngineIncarnation: ready.EngineIncarnation, QualificationRef: "non-entry-joined"},
		Profile: gatewaytransport.QualifiedProfile{Profile: route.Profile, BaseURL: route.BaseURL, Model: route.Model, QualificationRef: "non-entry-joined", RequestBytes: 4096, OutputBytes: 4096, Tokens: 100, ProviderTokenUpperBound: 200},
		MaxEntries: 4, EnvelopeBytes: 8192, CallbackBytes: 65536,
		CallbackOrigin: ready.CallbackOrigin, CallbackCredential: callbackToken,
		CallbackTimeout: 10 * time.Second, IOTimeout: 15 * time.Second, CleanupTimeout: 10 * time.Second,
		Authorize: func(r *http.Request) (gatewaytransport.Peer, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer " + executionToken:
				return gatewaytransport.Peer{ConsumerID: scope.Consumer, Role: "execution"}, nil
			case "Bearer " + cleanupToken:
				return gatewaytransport.Peer{ConsumerID: scope.Consumer, Role: "cleanup"}, nil
			}
			return gatewaytransport.Peer{}, service.ErrGatewayNativeIdentity
		},
		VerifyEnrollment: func(callCtx context.Context, e gatewaytransport.Enrollment) error {
			return callback(callCtx, "enrollment", map[string]any{"originRef": e.OriginRef, "engineIncarnation": e.EngineIncarnation, "qualificationRef": e.QualificationRef})
		},
		VerifyDispatch: func(callCtx context.Context, consumer string, p gatewaytransport.Proof, lease time.Time) error {
			body := bound(consumer, p)
			body["leaseExpiresAt"] = lease.Format(time.RFC3339Nano)
			return callback(callCtx, "dispatch-proof", body)
		},
		AuthorizeOwnerClosure: func(callCtx context.Context, consumer string, p gatewaytransport.Proof) error {
			return callback(callCtx, "owner-closure-authority", bound(consumer, p))
		},
		AcknowledgeOwnerClosure: func(callCtx context.Context, consumer string, p gatewaytransport.Proof, receipt gatewaytransport.Receipt) error {
			body := bound(consumer, p)
			body["receipt"] = receipt
			return callback(callCtx, "closure-ack", body)
		},
		AuthorizeCleanup: func(context.Context, string, gatewaytransport.Proof, gatewaytransport.CleanupLease) error {
			return service.ErrGatewayNativeIdentity
		},
		AcknowledgeClosure: func(context.Context, string, gatewaytransport.Proof, gatewaytransport.CleanupLease, gatewaytransport.Receipt) error {
			return service.ErrGatewayNativeIdentity
		},
	}
	handler, err := gatewaytransport.New(ctx, cfg)
	require.NoError(t, err)
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = handler.Stop(stopCtx)
	}()
	mounted.Store(handler)
	public.Store(handler.PublicHandler(2))
	_, err = io.WriteString(stdin, "{\"phase\":\"continue\"}\n")
	require.NoError(t, err)
	if consumer {
		prior := awaitLine(readLine())
		require.True(t, prior.ok, "joined consumer exited before its prior paid turn")
		var phase struct {
			Phase string `json:"phase"`
		}
		require.NoError(t, json.Unmarshal(prior.bytes, &phase))
		require.Equal(t, "prior-paid-completed", phase.Phase)
		require.EqualValues(t, 1, providerCalls.Load())
		competing = armConflict()
		_, err = io.WriteString(stdin, "{\"phase\":\"continue-conflict\"}\n")
		require.NoError(t, err)
	}
	pending := readLine()
	select {
	case <-conflict.ready:
	case line := <-pending:
		require.NoError(t, line.err)
		t.Fatal("joined consumer exited or reported completion before the protected SQL lock")
	case <-ctx.Done():
		t.Fatal("joined native protected admission did not reach SQL")
	}
	if consumer {
		require.EqualValues(t, 1, providerCalls.Load(), "the current root is still blocked at protected admission")
	}
	_, err = competing.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", conflict.key)
	require.NoError(t, err)
	require.NoError(t, competing.Rollback())
	var terminal struct {
		Phase              string `json:"phase"`
		Contract           string `json:"contract"`
		Mode               string `json:"mode"`
		ProofAfterACK      bool   `json:"proofAfterAck"`
		OriginalSpent      bool   `json:"originalSpent"`
		ExpiredDenied      bool   `json:"expiredDenied"`
		PriorPaidPreserved bool   `json:"priorPaidPreserved"`
		OneSuccessor       bool   `json:"oneSuccessor"`
	}
	var transcript bytes.Buffer
	for {
		line := awaitLine(pending)
		if !line.ok {
			break
		}
		require.LessOrEqual(t, transcript.Len()+len(line.bytes)+1, 65536)
		transcript.Write(line.bytes)
		transcript.WriteByte('\n')
		require.NoError(t, json.Unmarshal(line.bytes, &terminal))
		pending = readLine()
	}
	err = cmd.Wait()
	waited = true
	require.NoError(t, err, "joined Gateway failed: %s", transcript.String())
	require.Equal(t, "completed", terminal.Phase)
	require.Equal(t, mode, terminal.Mode)
	if consumer {
		require.Equal(t, "native-non-entry-consumer/v1", terminal.Contract)
		require.True(t, terminal.ProofAfterACK && terminal.OriginalSpent && terminal.PriorPaidPreserved && terminal.OneSuccessor)
		require.EqualValues(t, 2, providerCalls.Load(), "one prior turn and one successor are the only provider calls")
	} else {
		require.Equal(t, "native-non-entry-joined/v1", terminal.Contract)
		require.True(t, terminal.ProofAfterACK && terminal.OriginalSpent && terminal.ExpiredDenied)
		require.Zero(t, providerCalls.Load(), "original, duplicate, cancelled and expired requests never enter the provider")
	}
	require.NotContains(t, transcript.String()+stderr.String(), "synthetic-provider-key-for-non-entry")
}
