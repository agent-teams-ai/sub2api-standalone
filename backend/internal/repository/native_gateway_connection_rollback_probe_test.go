//go:build integration || gatewaycustody

package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This is the minimum compatible reader, not a claim that pre-parity releases
// understand the new journal. CI compiles this exact probe against the pinned
// production tree and records the separately built reader image and binary.
const nativeConnectionRollbackFloor = "a59da2e24adec39b8b21a512bc6721bf7fa2a4c1"

type nativeConnectionRollbackRow struct {
	Scope        service.GatewayNativeConnectionScope `json:"scope"`
	State        string                               `json:"state"`
	IntentSHA256 string                               `json:"intentSHA256"`
}

type nativeConnectionRollbackInput struct {
	Rows []nativeConnectionRollbackRow `json:"rows"`
}

type nativeConnectionRollbackReceipt struct {
	Contract                string                        `json:"contract"`
	ReaderSource            string                        `json:"readerSource"`
	ReaderSHA256            string                        `json:"readerSHA256"`
	States                  []string                      `json:"states"`
	Rows                    []nativeConnectionRollbackRow `json:"rows"`
	ReadOnly                bool                          `json:"readOnly"`
	NoEntry                 bool                          `json:"noEntry"`
	RowsUnchanged           bool                          `json:"rowsUnchanged"`
	MaterialPolicyPreserved bool                          `json:"materialPolicyPreserved"`
}

func nativeConnectionRollbackHash(t *testing.T, in service.GatewayNativeOAuthConnectIntent) string {
	t.Helper()
	// Production deliberately suppresses every intent field from JSON. Hash an
	// explicit private projection instead; never persist or log its raw bytes.
	fields := map[string]any{
		"Connection": in.Connection, "NativeMount": in.NativeMount,
		"NativeDispatchFamily": in.NativeDispatchFamily, "NativePlatform": in.NativePlatform,
		"NativeAccountType": in.NativeAccountType, "NativePublication": in.NativePublication,
		"Scope": in.Scope, "Operation": in.Operation, "ClientID": in.ClientID,
		"Redirect": in.Redirect, "Deadline": in.Deadline.UTC(), "StateHash": in.StateHash,
		"Envelope": in.Envelope, "State": in.State, "Outcome": in.Outcome,
		"EnrollmentOperation": in.EnrollmentOperation, "EnrollmentMAC": in.EnrollmentMAC,
		"RecoveryDenied": in.RecoveryDenied, "Method": in.Method,
		"DevicePhase": in.DevicePhase, "DeviceRevision": in.DeviceRevision,
		"DevicePollAfter": in.DevicePollAfter.UTC(), "DeviceDeadline": in.DeviceDeadline.UTC(),
		"ImportInputMAC": in.ImportInputMAC, "ImportClaimMAC": in.ImportClaimMAC,
		"ImportKeyCommitment": in.ImportKeyCommitment,
	}
	shape := reflect.TypeOf(in)
	require.Len(t, fields, shape.NumField(), "new stored fields require explicit rollback coverage")
	for i := range shape.NumField() {
		_, present := fields[shape.Field(i).Name]
		require.True(t, present, "every stored intent field must enter the private digest")
	}
	raw, err := json.Marshal(fields)
	require.NoError(t, err)
	defer clear(raw)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func nativeConnectionRollbackRows(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var rows string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(i) ORDER BY operation_ref)::text FROM gateway_oauth_connect_intents i`).Scan(&rows))
	return rows
}

func nativeConnectionRollbackOwner(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	ctx, err := service.WithGatewayNativeConsumer(ctx, "rollback-consumer")
	require.NoError(t, err)
	ctx, err = service.WithGatewayNativeOAuthOwner(ctx, "rollback-owner")
	require.NoError(t, err)
	return ctx
}

func nativeConnectionRollbackDependencies(t *testing.T, db *sql.DB, accounts service.AccountRepository, enter service.GatewayNativeOAuthEntryAuthorizer) service.GatewayNativeConnectionsDependencies {
	t.Helper()
	journal, err := NewGatewayNativeOAuthConnectRepository(db)
	require.NoError(t, err)
	physical, ok := accounts.(service.GatewayNativeConnectionRepository)
	require.True(t, ok)
	custody, err := service.NewGatewayNativeCredentialCustody("rollback-fixture", map[string][]byte{"rollback-fixture": bytes.Repeat([]byte{0x42}, 32)})
	require.NoError(t, err)
	return service.GatewayNativeConnectionsDependencies{
		Journal: journal, Repository: physical, Custody: custody, MaterialKey: bytes.Repeat([]byte{0x61}, 32), Connect: enter, Refresh: enter,
		Cleanup: func(context.Context, service.GatewayNativeOAuthCleanupRequest) (service.GatewayNativeOAuthCleanupAuthority, error) {
			return service.GatewayNativeOAuthCleanupAuthority{}, service.ErrGatewayNativeIdentity
		},
	}
}

// The writer uses the production encrypted journal and effect transitions. The
// method port only holds an exchange open or loses its acknowledgement; this is
// a persistence compatibility probe, not provider or review qualification.
func TestGatewayNativeConnectionRollbackWriter(t *testing.T) {
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_ROLLBACK_DSN", "native_connection_rollback_")
	reader := os.Getenv("GATEWAY_NATIVE_ROLLBACK_READER")
	require.True(t, filepath.IsAbs(reader))
	require.Equal(t, nativeConnectionRollbackFloor, os.Getenv("GATEWAY_NATIVE_ROLLBACK_READER_SOURCE"))
	binary, err := os.ReadFile(reader)
	require.NoError(t, err)
	binaryHash := sha256.Sum256(binary)
	require.Equal(t, os.Getenv("GATEWAY_NATIVE_ROLLBACK_READER_SHA256"), hex.EncodeToString(binaryHash[:]))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, accounts := custodyPGOpen(t, ctx, dsn)
	ownerCtx := nativeConnectionRollbackOwner(t, ctx)
	enter := func(ctx context.Context, scope service.GatewayNativeCredentialScope, _ string) error {
		if ctx.Err() != nil || scope.Owner != "rollback-owner" || !service.GatewayConnectionScopeAuthorized(ctx, scope) {
			return service.ErrGatewayNativeIdentity
		}
		return nil
	}
	dependencies := nativeConnectionRollbackDependencies(t, db, accounts, enter)
	entered, release := make(chan struct{}), make(chan struct{})
	var exchanges atomic.Int32
	secret := "rollback-transient-sentinel-" + uuid.NewString()
	method := service.GatewayNativeConnectionMethodAdapter{
		ProfileID: "gemini-google-one-responses-oauth-v1", MethodID: "browser", MethodRevision: 1,
		ConfigurationGeneration: "rollback-generation-v1", DispatchFamily: "gemini_oauth", AccountType: service.AccountTypeOAuth,
		Mount: service.GatewayConnectionMount{BaseURL: "https://cloudcode-pa.googleapis.com", Model: "gemini-3.1-pro-preview", QualificationRef: "rollback-fixture-v1"},
		Prepare: func(map[string]string) (service.GatewayNativeConnectionPrepared, error) {
			material, marshalErr := json.Marshal(map[string]string{"pkce_verifier": secret})
			return service.GatewayNativeConnectionPrepared{Material: material, AuthFamily: "gemini_google_one_oauth", AuthorizeURL: "https://identity.example.invalid/authorize", Credential: service.GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native"}}, marshalErr
		},
		Complete: func(ctx context.Context, in service.GatewayNativeConnectionScope, _ json.RawMessage, _ map[string]string) (service.GatewayNativeConnectionCredential, error) {
			exchanges.Add(1)
			if in.Operation == "rollback-entered" {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return service.GatewayNativeConnectionCredential{}, service.ErrGatewayNativeIdentity
		},
	}
	dependencies.Methods = []service.GatewayNativeConnectionMethodAdapter{method}
	dependencies.Dispatchers = map[string]service.GatewayNativeConnectionDispatchAdapter{method.DispatchFamily: {
		Forward: func(context.Context, *gin.Context, *service.Account, service.GatewayNativeRoute, []byte) (*service.OpenAIForwardResult, error) {
			return nil, service.ErrGatewayNativeIdentity
		},
		ValidateRequest: func(*http.Request, *service.Account, service.GatewayNativePublishedRoute, string) bool { return false },
	}}
	native, err := service.NewGatewayNativeConnections(dependencies)
	require.NoError(t, err)
	input := nativeConnectionRollbackInput{}
	for _, state := range []string{"prepared", "entered", "unknown"} {
		in := service.GatewayNativeConnectionScope{Scope: service.GatewayNativeCredentialScope{Consumer: "rollback-consumer", Owner: "rollback-owner", Account: uuid.NewString(), Generation: uuid.NewString(), Purpose: service.GatewayOAuthBundlePurpose}, Operation: "rollback-" + state, Connection: method.Selection()}
		created, beginErr := native.Begin(ownerCtx, in, nil)
		require.NoError(t, beginErr)
		require.Equal(t, "prepared", created.State)
		input.Rows = append(input.Rows, nativeConnectionRollbackRow{Scope: in, State: state})
	}
	submission := map[string]string{"completion": "synthetic-callback-only"}
	unknown, err := native.Submit(ownerCtx, input.Rows[2].Scope, submission)
	require.NoError(t, err)
	require.Equal(t, "unknown", unknown.State)
	finished := make(chan error, 1)
	go func() {
		_, submitErr := native.Submit(ownerCtx, input.Rows[1].Scope, submission)
		finished <- submitErr
	}()
	defer func() {
		close(release)
		require.NoError(t, <-finished)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("writer did not enter the held exchange")
	}
	intentHashes := map[string]bool{}
	for i := range input.Rows {
		row := &input.Rows[i]
		saved, readErr := dependencies.Journal.ReadConnectIntent(ownerCtx, row.Scope.Scope, row.Scope.Operation)
		require.NoError(t, readErr)
		require.Equal(t, row.State, saved.State)
		if row.State == "unknown" {
			require.Empty(t, saved.Envelope, "an ambiguous exchange must not retain replayable material")
		} else {
			require.True(t, strings.HasPrefix(saved.Envelope, "gcm1."))
		}
		row.IntentSHA256 = nativeConnectionRollbackHash(t, saved)
		intentHashes[row.IntentSHA256] = true
	}
	require.Len(t, intentHashes, 3, "different durable states must not collapse to an empty serialized intent")
	before := nativeConnectionRollbackRows(t, ctx, db)
	require.False(t, strings.Contains(before, secret), "transient material must remain encrypted")
	beforeExchanges := exchanges.Load()
	dir := t.TempDir()
	inputPath, outputPath := filepath.Join(dir, "input.json"), filepath.Join(dir, "reader.json")
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(inputPath, raw, 0600))
	cmd := exec.CommandContext(ctx, reader, "-test.run", "^TestGatewayNativeConnectionRollbackReader$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GATEWAY_NATIVE_ROLLBACK_INPUT="+inputPath, "GATEWAY_NATIVE_ROLLBACK_OUTPUT="+outputPath)
	output, err := cmd.CombinedOutput()
	require.False(t, bytes.Contains(output, []byte(secret)))
	require.NoError(t, err, "pinned reader failed: %s", output)
	var receipt nativeConnectionRollbackReceipt
	raw, err = os.ReadFile(outputPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, nativeConnectionRollbackFloor, receipt.ReaderSource)
	require.Equal(t, hex.EncodeToString(binaryHash[:]), receipt.ReaderSHA256)
	require.Equal(t, input.Rows, receipt.Rows)
	require.Equal(t, []string{"prepared", "entered", "unknown"}, receipt.States)
	require.True(t, receipt.ReadOnly)
	require.True(t, receipt.NoEntry)
	require.Equal(t, beforeExchanges, exchanges.Load())
	require.Equal(t, before, nativeConnectionRollbackRows(t, ctx, db), "old reader must not rewrite or rearm any row")
	receipt.RowsUnchanged, receipt.MaterialPolicyPreserved = true, true
	if destination := os.Getenv("GATEWAY_NATIVE_ROLLBACK_RECEIPT"); destination != "" {
		require.True(t, filepath.IsAbs(destination))
		raw, err = json.MarshalIndent(receipt, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(destination, append(raw, '\n'), 0600))
	}
}

// Executed only by the current writer against a binary compiled from the
// declared older production tree. There are no method adapters, dispatchers or
// mutation grants, and PostgreSQL itself enforces read-only transactions.
func TestGatewayNativeConnectionRollbackReader(t *testing.T) {
	inputPath := os.Getenv("GATEWAY_NATIVE_ROLLBACK_INPUT")
	if inputPath == "" {
		t.Skip("NOT_RUN: invoked only by the isolated rollback writer")
	}
	dsn := custodyPGDSN(t, "GATEWAY_NATIVE_ROLLBACK_DSN", "native_connection_rollback_")
	endpoint, err := url.Parse(dsn)
	require.NoError(t, err)
	query := endpoint.Query()
	query.Set("options", "-c default_transaction_read_only=on")
	endpoint.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", endpoint.String())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var readOnly string
	require.NoError(t, db.QueryRowContext(ctx, "SHOW default_transaction_read_only").Scan(&readOnly))
	require.Equal(t, "on", readOnly)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer func() { _ = client.Close() }()
	var entries atomic.Int32
	deny := func(context.Context, service.GatewayNativeCredentialScope, string) error {
		entries.Add(1)
		return service.ErrGatewayNativeIdentity
	}
	dependencies := nativeConnectionRollbackDependencies(t, db, NewAccountRepository(client, db, nil), deny)
	native, err := service.NewGatewayNativeConnections(dependencies)
	require.NoError(t, err)
	ownerCtx := nativeConnectionRollbackOwner(t, ctx)
	raw, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	require.LessOrEqual(t, len(raw), 16*1024)
	var input nativeConnectionRollbackInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&input))
	require.Len(t, input.Rows, 3)
	receipt := nativeConnectionRollbackReceipt{Contract: "native-connection-rollback-reader/v1", ReaderSource: nativeConnectionRollbackFloor, ReaderSHA256: os.Getenv("GATEWAY_NATIVE_ROLLBACK_READER_SHA256"), ReadOnly: true}
	for _, row := range input.Rows {
		read, readErr := native.Read(ownerCtx, row.Scope)
		require.NoError(t, readErr)
		require.Equal(t, row.State, read.State)
		require.Equal(t, &row.Scope.Connection, read.Connection)
		require.Empty(t, read.AuthorizeURL, "readback must not recreate a handoff")
		saved, readErr := dependencies.Journal.ReadConnectIntent(ownerCtx, row.Scope.Scope, row.Scope.Operation)
		require.NoError(t, readErr)
		require.Equal(t, row.IntentSHA256, nativeConnectionRollbackHash(t, saved))
		foreign := row.Scope
		foreign.Connection.ConfigurationGeneration = "foreign-generation"
		_, readErr = native.Read(ownerCtx, foreign)
		require.Error(t, readErr)
		foreign = row.Scope
		foreign.Scope.Owner = "foreign-owner"
		_, readErr = native.Read(ownerCtx, foreign)
		require.Error(t, readErr)
		receipt.States = append(receipt.States, read.State)
		receipt.Rows = append(receipt.Rows, row)
	}
	require.Zero(t, entries.Load())
	receipt.NoEntry = true
	raw, err = json.Marshal(receipt)
	require.NoError(t, err)
	path := os.Getenv("GATEWAY_NATIVE_ROLLBACK_OUTPUT")
	require.True(t, filepath.IsAbs(path))
	require.NoError(t, os.WriteFile(path, raw, 0600))
}
