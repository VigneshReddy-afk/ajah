package events

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestEmitter_ProcessesAllConcurrentEvents emits 1000 events from 10 goroutines
// and verifies that every event is processed exactly once.
func TestEmitter_ProcessesAllConcurrentEvents(t *testing.T) {
	const (
		numGoroutines = 10
		eventsEach    = 100
		total         = numGoroutines * eventsEach
	)

	var count atomic.Int64
	allDone := make(chan struct{})

	process := func(_ context.Context, _ RequestEvent) error {
		// close is called exactly once: atomic.Add returns the new value, and
		// only one goroutine will observe it equal to total.
		if count.Add(1) == int64(total) {
			close(allDone)
		}
		return nil
	}

	// Buffer is large enough to accept all events without drops.
	em := New(total*2, 4, zap.NewNop(), process)

	ctx, cancel := context.WithCancel(context.Background())
	em.Start(ctx)

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < eventsEach; j++ {
				em.Emit(RequestEvent{RequestID: fmt.Sprintf("req-%d-%d", n, j)})
			}
		}(i)
	}
	wg.Wait() // all emits submitted

	select {
	case <-allDone:
		// success: all 1000 events processed
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out: only %d of %d events processed", count.Load(), total)
	}

	cancel()
	em.Stop()

	// Final sanity check after Stop guarantees no worker is still running.
	if got := count.Load(); got != int64(total) {
		t.Errorf("final count = %d, want %d", got, total)
	}
}

// TestEmitter_NeverBlocksWhenChannelFull verifies that Emit returns immediately
// even when the internal channel is completely full.
//
// Strategy: do not start any workers so the channel cannot be drained.
// The first bufferSize emits fill the channel; every subsequent emit must
// take the non-blocking `default` path and return immediately.
func TestEmitter_NeverBlocksWhenChannelFull(t *testing.T) {
	const bufferSize = 5

	process := func(_ context.Context, _ RequestEvent) error { return nil }

	em := New(bufferSize, 4, zap.NewNop(), process)
	// Intentionally do not call Start — no workers drain the channel.

	const totalEmits = bufferSize * 3 // first 5 fill the channel, next 10 are dropped

	start := time.Now()
	for i := 0; i < totalEmits; i++ {
		em.Emit(RequestEvent{RequestID: fmt.Sprintf("req-%d", i)})
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Emit blocked: %d emits took %v, want < 100ms", totalEmits, elapsed)
	}
}

// TestEmitter_DrainOnStop verifies that events buffered at shutdown time are
// processed before Stop returns, not silently discarded.
func TestEmitter_DrainOnStop(t *testing.T) {
	const total = 50

	var processed atomic.Int64
	process := func(_ context.Context, _ RequestEvent) error {
		processed.Add(1)
		return nil
	}

	// Buffer larger than total so no events are dropped during emit.
	em := New(total*2, 2, zap.NewNop(), process)
	ctx, cancel := context.WithCancel(context.Background())
	em.Start(ctx)

	for i := 0; i < total; i++ {
		em.Emit(RequestEvent{RequestID: fmt.Sprintf("req-%d", i)})
	}

	// Cancel before all events are necessarily processed; workers must drain.
	cancel()
	em.Stop()

	if got := processed.Load(); got != int64(total) {
		t.Errorf("after Stop: processed %d events, want %d", got, total)
	}
}

// TestEmitter_StopCompletesWithinTimeout verifies Stop() returns well before
// the 5-second hard limit when the process function is instant.
func TestEmitter_StopCompletesWithinTimeout(t *testing.T) {
	process := func(_ context.Context, _ RequestEvent) error { return nil }

	em := New(200, 4, zap.NewNop(), process)
	ctx, cancel := context.WithCancel(context.Background())
	em.Start(ctx)

	for i := 0; i < 100; i++ {
		em.Emit(RequestEvent{RequestID: fmt.Sprintf("req-%d", i)})
	}

	cancel()

	start := time.Now()
	em.Stop()

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Stop took %v, expected well under 2s", elapsed)
	}
}

// TestEmitter_ProcessFnErrorIsLogged verifies that a failing ProcessFn does
// not crash the worker or prevent subsequent events from being processed.
func TestEmitter_ProcessFnErrorIsLogged(t *testing.T) {
	const total = 20

	var count atomic.Int64
	process := func(_ context.Context, event RequestEvent) error {
		count.Add(1)
		return fmt.Errorf("simulated processing error for %s", event.RequestID)
	}

	em := New(total*2, 2, zap.NewNop(), process)
	ctx, cancel := context.WithCancel(context.Background())
	em.Start(ctx)

	for i := 0; i < total; i++ {
		em.Emit(RequestEvent{RequestID: fmt.Sprintf("req-%d", i)})
	}

	// Wait for all events to be attempted.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && count.Load() < int64(total) {
		time.Sleep(time.Millisecond)
	}

	cancel()
	em.Stop()

	if got := count.Load(); got != int64(total) {
		t.Errorf("process called %d times, want %d", got, total)
	}
}
