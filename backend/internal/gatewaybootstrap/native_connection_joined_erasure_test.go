//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
)

type nativeConnectionJoinedErasureEvidence struct {
	Deleted          bool
	Native           bool
	Bound            bool
	Credentials      json.RawMessage
	SavedPublication json.RawMessage
	SavedRevision    int64
}

// Generic erasure deliberately retains immutable, non-secret provenance. The
// expected publication comes from the completed enrollment journal; the version
// comes only from committed refresh results for the same physical birth/scope.
// Legacy credentials must still be an empty object. Neither a null bundle nor an
// unknown key is evidence that credential material was erased.
func nativeConnectionJoinedErasureValid(e nativeConnectionJoinedErasureEvidence) bool {
	var credentials map[string]any
	if !e.Deleted || json.Unmarshal(e.Credentials, &credentials) != nil || credentials == nil {
		return false
	}
	if !e.Native {
		return len(credentials) == 0 && len(e.SavedPublication) == 0
	}
	if !e.Bound || len(credentials) != 2 || e.SavedRevision < 1 {
		return false
	}
	revision, ok := credentials["connection_revision"].(string)
	if !ok || revision != strconv.FormatInt(e.SavedRevision, 10) {
		return false
	}
	publication, ok := credentials["connection_publication"].(map[string]any)
	if !ok || len(publication) == 0 {
		return false
	}
	var saved map[string]any
	return json.Unmarshal(e.SavedPublication, &saved) == nil && len(saved) > 0 && reflect.DeepEqual(publication, saved)
}

func requireNativeConnectionJoinedErasure(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT a.id,a.deleted_at IS NOT NULL,
 COALESCE(a.extra->'gateway_native_connection_v1'='true'::jsonb,false),a.credentials::text,
 c.native_publication::text,COALESCE(f.result_version,1),
 COALESCE(c.state='completed' AND NOT c.recovery_denied
  AND c.outcome_generation=c.generation
  AND a.extra->>'gateway_connection_operation_v1'=c.operation_ref
  AND a.extra->>'gateway_generation_v1'=c.generation
  AND a.extra->>'gateway_profile_v1'=c.connection_binding->>'profileId'
  AND a.extra->>'gateway_model_v1'=c.native_mount->>'model'
  AND a.extra->'gateway_credential_scope_v1'=jsonb_build_object(
   'consumer',c.consumer,'owner',c.owner_ref,'account',c.account_ref,'generation',c.generation,'purpose',c.purpose)
  AND a.platform=c.native_platform AND a.type=c.native_account_type,false)
 FROM accounts a
 LEFT JOIN gateway_oauth_connect_intents c ON c.outcome_account_id=a.id AND c.client_id='native-connection-v1'
 LEFT JOIN LATERAL (SELECT max(r.result_version) AS result_version FROM gateway_oauth_refresh_attempts r
  WHERE r.account_id=a.id AND r.native_created_at=a.created_at
   AND r.consumer=c.consumer AND r.owner_ref=c.owner_ref AND r.account_ref=c.account_ref AND r.generation=c.generation
   AND r.state='completed' AND r.publication_xid IS NOT NULL) f ON true
 ORDER BY a.id`)
	if err != nil {
		t.Fatalf("joined final erasure query failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := map[int64]bool{}
	for rows.Next() {
		var id int64
		var credentials []byte
		var publication sql.NullString
		var e nativeConnectionJoinedErasureEvidence
		if err := rows.Scan(&id, &e.Deleted, &e.Native, &credentials, &publication, &e.SavedRevision, &e.Bound); err != nil {
			t.Fatalf("joined final erasure row could not be read: %v", err)
		}
		e.Credentials = credentials
		if publication.Valid {
			e.SavedPublication = json.RawMessage(publication.String)
		}
		if seen[id] || !nativeConnectionJoinedErasureValid(e) {
			// Do not include credentials or publication values in exported logs.
			t.Fatalf("joined final erasure invariant failed for physical account %d", id)
		}
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("joined final erasure rows incomplete: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("joined final erasure requires at least one physical account")
	}
}

func TestNativeConnectionJoinedErasureEvidence(t *testing.T) {
	publication := map[string]any{
		"connection":     map[string]any{"profileId": "fixture-native-profile", "methodId": "api_key", "methodRevision": "1", "configurationGeneration": "fixture-generation"},
		"credential":     map[string]any{"kind": "api_key", "policy": "provider_key", "refreshPolicy": "none"},
		"dispatchFamily": "openai_responses", "authFamily": "api_key",
	}
	saved, err := json.Marshal(publication)
	if err != nil {
		t.Fatal(err)
	}
	credential := func(revision any, extras map[string]any) json.RawMessage {
		value := map[string]any{"connection_revision": revision, "connection_publication": publication}
		for key, extra := range extras {
			value[key] = extra
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	valid := nativeConnectionJoinedErasureEvidence{Deleted: true, Native: true, Bound: true, Credentials: credential("1", nil), SavedPublication: saved, SavedRevision: 1}
	tests := []struct {
		name   string
		change func(*nativeConnectionJoinedErasureEvidence)
		want   bool
	}{
		{name: "retained enrollment metadata", want: true},
		{name: "committed refresh metadata", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Credentials, e.SavedRevision = credential("4", nil), 4
		}, want: true},
		{name: "legacy empty object", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Native, e.Bound, e.Credentials, e.SavedPublication = false, false, json.RawMessage(`{}`), nil
		}, want: true},
		{name: "secret bundle", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Credentials = credential("1", map[string]any{"connection_bundle": "fixture-secret"})
		}},
		{name: "null bundle", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Credentials = credential("1", map[string]any{"connection_bundle": nil})
		}},
		{name: "unknown null key", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Credentials = credential("1", map[string]any{"unexpected": nil})
		}},
		{name: "wrong publication", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Credentials = credential("1", map[string]any{"connection_publication": map[string]any{"dispatchFamily": "wrong_family"}})
		}},
		{name: "missing saved publication", change: func(e *nativeConnectionJoinedErasureEvidence) { e.SavedPublication = nil }},
		{name: "null publication", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Credentials = credential("1", map[string]any{"connection_publication": nil})
		}},
		{name: "future revision", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Credentials = credential("2", nil) }},
		{name: "stale revision", change: func(e *nativeConnectionJoinedErasureEvidence) { e.SavedRevision = 2 }},
		{name: "numeric revision", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Credentials = credential(1, nil) }},
		{name: "noncanonical revision", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Credentials = credential("01", nil) }},
		{name: "no canonical revision", change: func(e *nativeConnectionJoinedErasureEvidence) { e.SavedRevision = 0 }},
		{name: "lost provenance", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Credentials = json.RawMessage(`{}`) }},
		{name: "live native birth", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Deleted = false }},
		{name: "unbound native birth", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Bound = false }},
		{name: "legacy with retained metadata", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Native, e.SavedPublication = false, nil }},
		{name: "legacy with null key", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Native, e.SavedPublication, e.Credentials = false, nil, json.RawMessage(`{"api_key":null}`)
		}},
		{name: "legacy with generic journal", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Native, e.Credentials = false, json.RawMessage(`{}`) }},
		{name: "live legacy birth", change: func(e *nativeConnectionJoinedErasureEvidence) {
			e.Deleted, e.Native, e.SavedPublication, e.Credentials = false, false, nil, json.RawMessage(`{}`)
		}},
		{name: "null credentials", change: func(e *nativeConnectionJoinedErasureEvidence) { e.Credentials = json.RawMessage(`null`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := valid
			if test.change != nil {
				test.change(&e)
			}
			if actual := nativeConnectionJoinedErasureValid(e); actual != test.want {
				t.Errorf("erasure evidence accepted = %t, want %t", actual, test.want)
			}
		})
	}
}
