//go:build linux && (integration || gatewaycustody)

package gatewaybootstrap

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type nativeConnectionJoinedPostconditionEvidence struct {
	Contract string `json:"contract"`
	Verified bool   `json:"verified"`
}

// A failed consumer batch must still reach Native's independent source and
// erasure assertions. A partial check failure retains a negative receipt;
// successful postchecks never qualify a failed child or an invalid terminal.
func finishNativeConnectionJoined(t *testing.T, childErr error, phase, contract, expectedContract string, verifySource func(), writeReceipt func(bool)) {
	t.Helper()
	saved := false
	defer func() {
		if !saved {
			writeReceipt(false)
		}
	}()
	verifySource()
	// Peer callbacks can report nonfatal errors. Capture their outcome before
	// the intentionally separate child/terminal success assertions fail the test.
	writeReceipt(!t.Failed())
	saved = true
	require.NoError(t, childErr, "joined runner failed; sanitized row/source receipt saved")
	require.Equal(t, "completed", phase)
	require.Equal(t, expectedContract, contract)
}

func requireNativeConnectionJoinedSourcePostconditions(t *testing.T, ctx context.Context, db *sql.DB, unexpected int64, rows []nativeConnectionJoinedRow, peersByID map[string]*nativeConnectionJoinedPeer, protocols map[string]*nativeConnectionJoinedProtocol) {
	t.Helper()
	var count int
	require.Zero(t, unexpected, "no unqualified source endpoint, credential or protocol was accepted")
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
