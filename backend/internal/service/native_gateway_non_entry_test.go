//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func nonEntryLifetime(t *testing.T) (context.Context, *GatewayNativeLifetime, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	life, err := NewGatewayNativeLifetime(ctx, cancel, func() {}, 4096)
	require.NoError(t, err)
	require.True(t, life.Admit(time.Now().Add(time.Minute)))
	return WithGatewayNativeLifetime(ctx, life), life, cancel
}

func TestGatewayNativeNonEntryPreventsLaterEntryAndStaysOutOfLegacyJSON(t *testing.T) {
	ctx, life, _ := nonEntryLifetime(t)
	gatewayNativeAdmissionFailed(ctx, ErrGatewayNativeAdmissionStorageConflict)
	require.Equal(t, GatewayNativeAdmissionStorageConflict, life.Snapshot().NonEntryReason)
	var entered atomic.Bool
	require.False(t, gatewayNativeEntered(ctx, &entered))
	require.False(t, entered.Load())
	life.finish()
	snapshot := life.Snapshot()
	require.True(t, snapshot.ContextDone && snapshot.ForwardingReturned && snapshot.BodyKnown && snapshot.BodyClosed)
	require.False(t, snapshot.Entered || snapshot.CloseFailed || snapshot.Completed)
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var legacy map[string]any
	require.NoError(t, json.Unmarshal(raw, &legacy))
	require.Len(t, legacy, 8)
	require.NotContains(t, string(raw), "admission_storage_conflict")
	require.NotContains(t, legacy, "nonEntryReason")
}

func TestGatewayNativeNonEntryExcludesCancellationEntryAndGenericFailures(t *testing.T) {
	for _, name := range []string{"generic", "cancelled", "cancelled_child", "expired_child", "sealed", "entered", "expired_lease", "not_admitted"} {
		t.Run(name, func(t *testing.T) {
			ctx, life, cancel := nonEntryLifetime(t)
			failure := ErrGatewayNativeAdmissionStorageConflict
			switch name {
			case "generic":
				failure = errors.New("40P01 admission_storage_conflict")
			case "cancelled":
				cancel()
			case "cancelled_child":
				child, cancelChild := context.WithCancel(ctx)
				cancelChild()
				ctx = child
				require.NoError(t, life.ctx.Err())
			case "expired_child":
				child, cancelChild := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancelChild()
				ctx = child
				require.ErrorIs(t, child.Err(), context.DeadlineExceeded)
				require.NoError(t, life.ctx.Err())
			case "sealed":
				life.Cancel()
			case "entered":
				var entered atomic.Bool
				require.True(t, gatewayNativeEntered(ctx, &entered))
			case "expired_lease":
				life.mu.Lock()
				life.lease = time.Now().Add(-time.Second)
				life.mu.Unlock()
			case "not_admitted":
				life.mu.Lock()
				life.admitted = false
				life.mu.Unlock()
			}
			gatewayNativeAdmissionFailed(ctx, failure)
			require.Empty(t, life.Snapshot().NonEntryReason)
			life.finish()
		})
	}
}

func TestGatewayNativeNonEntryAndEntryShareOneLatch(t *testing.T) {
	for range 100 {
		ctx, life, cancel := nonEntryLifetime(t)
		start := make(chan struct{})
		var entered atomic.Bool
		var done sync.WaitGroup
		done.Add(2)
		go func() {
			defer done.Done()
			<-start
			gatewayNativeAdmissionFailed(ctx, ErrGatewayNativeAdmissionStorageConflict)
		}()
		go func() {
			defer done.Done()
			<-start
			gatewayNativeEntered(ctx, &entered)
		}()
		close(start)
		done.Wait()
		snapshot := life.Snapshot()
		require.NotEqual(t, snapshot.Entered, snapshot.NonEntryReason != "")
		require.Equal(t, snapshot.Entered, entered.Load())
		life.finish()
		cancel()
	}
}
