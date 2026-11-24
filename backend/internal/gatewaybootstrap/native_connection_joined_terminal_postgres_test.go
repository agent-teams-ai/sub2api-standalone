//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const nativeConnectionJoinedTerminalDSN = "GATEWAY_NATIVE_CONNECTION_TERMINAL_TEST_DSN"
const nativeConnectionJoinedTerminalContract = "native-connections-gateway-sdk-rr-joined/v1"

func nativeConnectionJoinedTerminalDatabase(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	dsn := os.Getenv(nativeConnectionJoinedTerminalDSN)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, u.Scheme)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.True(t, strings.HasPrefix(u.Path, "/native_connection_terminal_"))
	require.Equal(t, "sslmode=disable", u.RawQuery)
	require.Empty(t, u.Fragment)
	require.NotNil(t, u.User)
	_, password := u.User.Password()
	require.False(t, password)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var version string
	require.NoError(t, db.QueryRowContext(ctx, "SHOW server_version").Scan(&version))
	t.Logf("terminal postcheck fixture PostgreSQL %s", version)
	return db
}

// The negative parent processes must remain failing while recording completed
// source assertions. Real migrated PG rows distinguish credential erasure from
// the input-envelope check; the unchanged provider oracle observes a tool and
// signed reasoning continuation, rather than receiving fabricated counters.
func TestNativeConnectionJoinedTerminalPostgres(t *testing.T) {
	if os.Getenv(nativeConnectionJoinedTerminalDSN) == "" {
		t.Skip("NOT_RUN: explicit disposable terminal-check PostgreSQL fixture not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db := nativeConnectionJoinedTerminalDatabase(t, ctx)
	var tables int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'").Scan(&tables))
	require.Zero(t, tables, "never reset a populated terminal-check database")
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	var accountID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts
 (name,platform,type,status,schedulable,credentials,deleted_at)
 VALUES ('terminal-check-fixture','openai','apikey','disabled',false,'{}',clock_timestamp()) RETURNING id`).Scan(&accountID))
	executable, err := os.Executable()
	require.NoError(t, err)
	cases := []struct {
		name      string
		verified  bool
		childExit int
		phase     string
		failure   string
	}{
		{"blocked_nonzero", true, 1, "blocked", "joined runner failed"},
		{"blocked_zero", true, 0, "blocked", "completed"},
		{"wrong_contract", true, 0, "completed", nativeConnectionJoinedTerminalContract},
		{"missing_terminal", true, 1, "", "joined runner failed"},
		{"nonfatal_source", false, 1, "blocked", "nonfatal source postcondition fixture"},
		{"unfinished_protocol", false, 1, "blocked", "terminal-source-fixture"},
		{"retained_credential", false, 1, "blocked", "joined final erasure invariant failed"},
		{"retained_input_envelope", false, 1, "blocked", "terminal enrollment and cleanup erased every saved input envelope"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			credentials := `{}`
			if test.name == "retained_credential" {
				credentials = `{"api_key":"terminal-fixture-secret"}`
			}
			_, err := db.ExecContext(ctx, "UPDATE accounts SET credentials=$1::jsonb WHERE id=$2", credentials, accountID)
			require.NoError(t, err)
			if test.name == "retained_input_envelope" {
				_, err := db.ExecContext(ctx, `INSERT INTO gateway_oauth_connect_intents
 (consumer,owner_ref,account_ref,generation,purpose,operation_ref,enrollment_operation,
 client_id,redirect_uri,deadline,state_hash,material_envelope)
 VALUES ('terminal-fixture','terminal-owner','terminal-account',$1,'provider-oauth-bundle-v1',
 'terminal-operation',$2,'app_EMoamEEZ73f0CkXaXp7hrann','http://localhost:1455/auth/callback',
 clock_timestamp()+interval '5 minutes',repeat('f',64),$3)`, uuid.NewString(), "connect-"+uuid.NewString(), "gcc1."+strings.Repeat("X", 60))
				require.NoError(t, err)
			}
			artifacts := t.TempDir()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestNativeConnectionJoinedTerminalProcess$", "-test.v", "--", "fixture-native-terminal", test.name, artifacts)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, "the parent must retain its failing qualification outcome")
			require.Equal(t, 1, exit.ExitCode())
			require.Contains(t, string(output), test.failure)
			require.NotContains(t, string(output), "terminal-fixture-secret")
			path := filepath.Join(artifacts, "native-receipt.json")
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "terminal-fixture-secret")
			var receipt struct {
				ChildExitCode        int                                         `json:"childExitCode"`
				ChildTerminalPhase   string                                      `json:"childTerminalPhase"`
				PeerVerified         bool                                        `json:"peerVerified"`
				SourcePostconditions nativeConnectionJoinedPostconditionEvidence `json:"sourcePostconditions"`
			}
			require.NoError(t, json.Unmarshal(raw, &receipt))
			require.Equal(t, test.childExit, receipt.ChildExitCode)
			require.Equal(t, test.phase, receipt.ChildTerminalPhase)
			require.True(t, receipt.PeerVerified, "the actual source verifier must execute before child outcome assertions")
			require.Equal(t, "native-connections-source-postconditions/v1", receipt.SourcePostconditions.Contract)
			require.Equal(t, test.verified, receipt.SourcePostconditions.Verified)
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
		})
	}
}

func TestNativeConnectionJoinedTerminalProcess(t *testing.T) {
	if len(os.Args) >= 3 && os.Args[len(os.Args)-2] == "fixture-native-terminal-runner" {
		variant := os.Args[len(os.Args)-1]
		phase, contract, code := "blocked", nativeConnectionJoinedTerminalContract, 1
		if variant == "blocked_zero" {
			code = 0
		}
		if variant == "wrong_contract" {
			phase, contract, code = "completed", "wrong-terminal-contract", 0
		}
		if variant != "missing_terminal" {
			require.NoError(t, json.NewEncoder(os.Stdout).Encode(map[string]string{"phase": phase, "contract": contract}))
		}
		os.Exit(code)
	}
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "fixture-native-terminal" {
		return
	}
	variant, artifacts := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := nativeConnectionJoinedTerminalDatabase(t, ctx)
	peer, protocol := nativeConnectionJoinedTerminalProtocol(t, variant != "unfinished_protocol")
	peerVerified := false
	peer.Verify = func() {
		require.Positive(t, protocol.counts()["schemaRequests"].(int64), "the source observed an actual schema request")
		if variant == "nonfatal_source" && protocol.counts()["dispatches"].(int64) != 3 {
			t.Error("nonfatal source postcondition fixture: required third request was not observed")
		}
		peerVerified = true
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.CommandContext(ctx, executable, "-test.run=^TestNativeConnectionJoinedTerminalProcess$", "--", "fixture-native-terminal-runner", variant)
	raw, childErr := child.Output()
	var terminal struct{ Phase, Contract string }
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &terminal))
	}
	finishNativeConnectionJoined(t, childErr, terminal.Phase, terminal.Contract, nativeConnectionJoinedTerminalContract, func() {
		requireNativeConnectionJoinedSourcePostconditions(t, ctx, db, 0, []nativeConnectionJoinedRow{peer.Row},
			map[string]*nativeConnectionJoinedPeer{peer.Row.ID: peer}, map[string]*nativeConnectionJoinedProtocol{peer.Row.ID: protocol})
	}, func(verified bool) {
		receipt, err := json.Marshal(map[string]any{
			"childExitCode": child.ProcessState.ExitCode(), "childTerminalPhase": terminal.Phase, "peerVerified": peerVerified,
			"sourcePostconditions": nativeConnectionJoinedPostconditionEvidence{Contract: "native-connections-source-postconditions/v1", Verified: verified},
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(artifacts, "native-receipt.json"), receipt, 0600))
	})
}

func nativeConnectionJoinedTerminalProtocol(t *testing.T, complete bool) (*nativeConnectionJoinedPeer, *nativeConnectionJoinedProtocol) {
	t.Helper()
	schema := json.RawMessage(`{"type":"object","properties":{"findings":{"type":"array"},"revalidations":{"type":"array"}}}`)
	protocol := &nativeConnectionJoinedProtocol{expectedSchema: nativeConnectionJoinedSchemaDigest(schema)}
	peer := &nativeConnectionJoinedPeer{Protocol: "responses", Row: nativeConnectionJoinedRow{
		ID: "terminal-source-fixture", Model: "openai/gpt-5.4-mini", Execute: true,
		ReviewProof: &nativeConnectionJoinedReviewProof{ReasoningEffort: "medium", MaxOutputTokens: 8192},
	}, InferenceAuth: func(r *http.Request) bool { return r.Method == http.MethodPost }}
	body := map[string]any{
		"model": peer.Row.Model, "stream": true, "input": "review", "reasoning": map[string]string{"effort": "medium"},
		"max_output_tokens": 8192, "include": []string{"reasoning.encrypted_content"},
		"text": map[string]any{"format": map[string]any{"type": "json_schema", "schema": schema}},
		"tools": []any{map[string]any{"type": "function", "name": "exec_command", "parameters": map[string]any{
			"type": "object", "properties": map[string]any{"cmd": map[string]string{"type": "string"}}, "required": []string{"cmd"},
		}}},
	}
	serve := func() *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		response := httptest.NewRecorder()
		require.NoError(t, protocol.serve(response, httptest.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/responses", bytes.NewReader(raw)), peer))
		return response
	}
	first := serve()
	if complete {
		var output []map[string]any
		for _, line := range strings.Split(first.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event struct {
				Type     string
				Response struct{ Output []map[string]any }
			}
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
			if event.Type == "response.completed" {
				output = event.Response.Output
			}
		}
		require.Len(t, output, 2)
		require.Equal(t, "reasoning", output[0]["type"])
		require.Equal(t, "function_call", output[1]["type"])
		body["input"] = []map[string]any{output[0], output[1], {"type": "function_call_output", "call_id": output[1]["call_id"], "output": "owned-checkout"}}
		serve()
	}
	return peer, protocol
}
