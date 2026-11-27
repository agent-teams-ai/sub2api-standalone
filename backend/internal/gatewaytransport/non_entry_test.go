//go:build unit

package gatewaytransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayNativeNonEntryNegotiationOnPrivateAndPublicHTTP(t *testing.T) {
	for _, mode := range []string{"private", "public"} {
		for _, acknowledgement := range []string{"exact", "absent", "duplicate", "coalesced", "unknown"} {
			t.Run(mode+"/"+acknowledgement, func(t *testing.T) {
				nonEntryHTTP(t, mode, acknowledgement, service.ErrGatewayNativeAdmissionStorageConflict)
			})
		}
		for name, failure := range map[string]error{
			"generic": errors.New("40P01 must not be classified from text"),
			"cancelled": context.Canceled,
			"deadline": context.DeadlineExceeded,
		} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				nonEntryHTTP(t, mode, "exact", failure)
			})
		}
	}
}

func nonEntryHTTP(t *testing.T, mode, acknowledgement string, failure error) {
	t.Helper()
	var original Request
	var entries atomic.Int32
	proofs := make(chan Proof, 1)
	receipts := make(chan Receipt, 1)
	readProof := func() Proof {
		select {
		case proof := <-proofs:
			return proof
		case <-time.After(5 * time.Second):
			t.Fatal("original admission proof was not received")
			return Proof{}
		}
	}
	readReceipt := func() Receipt {
		select {
		case result := <-receipts:
			return result
		case <-time.After(5 * time.Second):
			t.Fatal("public terminal settlement was not received")
			return Receipt{}
		}
	}
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+fixtureCallbackToken, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/private/native/v1/execution-authority":
			var input publicAuthorizationRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			_ = json.NewEncoder(w).Encode(publicAuthorizationReply{"fixture-consumer", input.RequestRef, original.Worker, original.Admission, original.Descriptor})
		case "/private/native/v1/admit", "/private/native/v1/public-admit":
			require.Equal(t, []string{nonEntryCapability}, r.Header.Values(nonEntryCapabilityHeader))
			var input admitRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			switch acknowledgement {
			case "exact":
				w.Header().Set(nonEntryCapabilityHeader, nonEntryCapability)
			case "duplicate":
				w.Header().Add(nonEntryCapabilityHeader, nonEntryCapability)
				w.Header().Add(nonEntryCapabilityHeader, nonEntryCapability)
			case "coalesced":
				w.Header().Set(nonEntryCapabilityHeader, nonEntryCapability+", "+nonEntryCapability)
			case "unknown":
				w.Header().Set(nonEntryCapabilityHeader, "unsupported")
			}
			reply := ownerReply(input)
			proofs <- reply.Dispatch.Closure
			_ = json.NewEncoder(w).Encode(reply)
		case "/private/native/v1/execution-settle":
			var input struct {
				Receipt Receipt `json:"receipt"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			receipts <- input.Receipt
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			t.Errorf("unexpected callback %s", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer authority.Close()
	h, private, _, _ := ownerIngressFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entries.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}), func(cfg *Config, repo *ownerFixtureRepo) {
		original = ownerRequest()
		original.Descriptor.BaseURL = cfg.Profile.BaseURL
		cfg.CallbackOrigin = authority.URL
		repo.nativeLockError = failure
	})
	eligible := acknowledgement == "exact" && errors.Is(failure, service.ErrGatewayNativeAdmissionStorageConflict)
	var observed Receipt
	if mode == "private" {
		// Read-only exact owner readback, after the actual native HTTP call returned.
		result := ownerPOST(private.Client(), private.URL, "", fixtureExecutionToken, original)
		require.NoError(t, result.err)
		if eligible {
			require.Equal(t, http.StatusAccepted, result.code)
			require.Equal(t, service.GatewayNativeAdmissionStorageConflict, result.receipt.NonEntryReason)
		} else {
			require.NotEqual(t, http.StatusAccepted, result.code)
		}
		proof := readProof()
		read := ownerPOST(private.Client(), private.URL, "/read-owner", fixtureExecutionToken, map[string]any{"proof": proof})
		require.NoError(t, read.err)
		require.Equal(t, http.StatusAccepted, read.code)
		observed = read.receipt
		require.NotNil(t, observed.Proof)
		require.Equal(t, proof, *observed.Proof)
	} else {
		public := httptest.NewServer(h.PublicHandler(2))
		defer public.Close()
		request, err := http.NewRequest(http.MethodPost, public.URL+"/v1/responses", bytes.NewReader(original.Payload))
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer synthetic-non-entry-execution")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Gateway-Request-Id", original.RequestRef)
		response, err := public.Client().Do(request)
		require.NoError(t, err)
		if eligible {
			require.Equal(t, http.StatusAccepted, response.StatusCode)
		} else {
			require.NotEqual(t, http.StatusAccepted, response.StatusCode)
		}
		body, readErr := io.ReadAll(response.Body)
		require.NoError(t, readErr)
		require.NoError(t, response.Body.Close())
		if eligible {
			var status map[string]any
			require.NoError(t, json.Unmarshal(body, &status))
			require.Equal(t, map[string]any{
				"requestRef": original.RequestRef, "effect": "dispatch_started", "status": "pending",
			}, status)
			for _, private := range []string{"nonEntryReason", "proof", "token", "lifetime", "fixture-proof"} {
				require.NotContains(t, string(body), private)
			}
		}
		observed = readReceipt()
		require.NotNil(t, observed.Proof)
		require.Equal(t, readProof(), *observed.Proof)
	}
	require.Zero(t, entries.Load())
	require.True(t, closed(observed.Lifetime))
	require.False(t, observed.Lifetime.Entered)
	require.Equal(t, original.Admission.ExecutionRef, observed.ExecutionRef)
	require.Equal(t, original.RequestRef, observed.RequestRef)
	raw, err := json.Marshal(observed)
	require.NoError(t, err)
	if eligible {
		require.Equal(t, service.GatewayNativeAdmissionStorageConflict, observed.NonEntryReason)
		require.Contains(t, string(raw), "\"nonEntryReason\":\"admission_storage_conflict\"")
	} else {
		require.Empty(t, observed.NonEntryReason)
		require.NotContains(t, string(raw), "nonEntryReason")
	}
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NotContains(t, decoded["lifetime"], "nonEntryReason")
}
