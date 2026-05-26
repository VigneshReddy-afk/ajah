package attribution

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ajah/core/internal/events"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// ---- helpers ----------------------------------------------------------------

func newTestEngine(t *testing.T, mr *miniredis.Miniredis) (*Engine, *redis.Client) {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return New(rdb, zap.NewNop()), rdb
}

func newObservedEngine(t *testing.T, mr *miniredis.Miniredis, level zapcore.Level) (*Engine, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(level)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return New(rdb, zap.New(core)), logs
}

// baseEvent returns a fully populated event pinned to a fixed UTC timestamp
// so key date suffixes are deterministic across tests.
func baseEvent() events.RequestEvent {
	return events.RequestEvent{
		RequestID:    "req-test",
		UserID:       "user-1",
		FeatureName:  "feature-a",
		AgentStep:    "step-1",
		Model:        "gpt-4o",
		Provider:     "openai",
		InputTokens:  1000,
		OutputTokens: 500,
		Timestamp:    time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
	}
}

const almostEqual = 1e-9 // tolerance for float64 USD comparisons

func withinTolerance(got, want float64) bool {
	return math.Abs(got-want) <= almostEqual
}

// ---- pricing tests ----------------------------------------------------------

func TestEngine_CalculatesCostForAllKnownModels(t *testing.T) {
	cases := []struct {
		model    string
		input    int
		output   int
		wantCost float64
	}{
		{"gpt-4o", 1000, 500, 1000*0.000005 + 500*0.000015},
		{"gpt-4-turbo", 1000, 500, 1000*0.00001 + 500*0.00003},
		{"claude-3-5-sonnet", 1000, 500, 1000*0.000003 + 500*0.000015},
		{"claude-3-opus", 1000, 500, 1000*0.000015 + 500*0.000075},
		{"gemini-1.5-pro", 1000, 500, 1000*0.00000125 + 500*0.000005},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			mr := miniredis.RunT(t)
			engine, _ := newTestEngine(t, mr)

			evt := baseEvent()
			evt.Model = tc.model
			evt.InputTokens = tc.input
			evt.OutputTokens = tc.output

			record, err := engine.Process(context.Background(), evt)
			if err != nil {
				t.Fatalf("Process() error: %v", err)
			}
			if !withinTolerance(record.CostUSD, tc.wantCost) {
				t.Errorf("CostUSD = %.12f, want %.12f", record.CostUSD, tc.wantCost)
			}
		})
	}
}

func TestEngine_UsesDefaultPricingForUnknownModel(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, _ := newTestEngine(t, mr)

	evt := baseEvent()
	evt.Model = "totally-unknown-model-xyz"

	record, err := engine.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	want := float64(evt.InputTokens)*defaultPrice.InputPerToken +
		float64(evt.OutputTokens)*defaultPrice.OutputPerToken

	if !withinTolerance(record.CostUSD, want) {
		t.Errorf("CostUSD = %.12f, want %.12f (default pricing)", record.CostUSD, want)
	}
}

// ---- Redis key pattern tests ------------------------------------------------

func TestEngine_PublishesToAllThreeRedisKeys(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, rdb := newTestEngine(t, mr)

	evt := baseEvent()
	_, err := engine.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	ctx := context.Background()
	day := evt.Timestamp.UTC().Format("2006-01-02")
	wantCost := float64(evt.InputTokens)*0.000005 + float64(evt.OutputTokens)*0.000015

	checks := []struct {
		label string
		key   string
	}{
		{"user", fmt.Sprintf("cost:user:%s:daily:%s", evt.UserID, day)},
		{"feature", fmt.Sprintf("cost:feature:%s:daily:%s", evt.FeatureName, day)},
		{"model", fmt.Sprintf("cost:model:%s:daily:%s", evt.Model, day)},
	}

	for _, c := range checks {
		val, err := rdb.Get(ctx, c.key).Float64()
		if err != nil {
			t.Errorf("[%s] key %q not found in Redis: %v", c.label, c.key, err)
			continue
		}
		if !withinTolerance(val, wantCost) {
			t.Errorf("[%s] value = %.12f, want %.12f", c.label, val, wantCost)
		}
	}
}

func TestEngine_AccumulatesCostAcrossMultipleEvents(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, rdb := newTestEngine(t, mr)

	ctx := context.Background()
	evt := baseEvent()
	costPerCall := float64(evt.InputTokens)*0.000005 + float64(evt.OutputTokens)*0.000015

	const calls = 4
	for i := 0; i < calls; i++ {
		evt.RequestID = fmt.Sprintf("req-%d", i)
		if _, err := engine.Process(ctx, evt); err != nil {
			t.Fatalf("Process() error on call %d: %v", i, err)
		}
	}

	day := evt.Timestamp.UTC().Format("2006-01-02")
	key := fmt.Sprintf("cost:user:%s:daily:%s", evt.UserID, day)

	val, err := rdb.Get(ctx, key).Float64()
	if err != nil {
		t.Fatalf("reading key %q: %v", key, err)
	}
	if !withinTolerance(val, costPerCall*calls) {
		t.Errorf("accumulated value = %.12f, want %.12f", val, costPerCall*calls)
	}
}

func TestEngine_SetsRedisKeyTTLTo90Days(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, _ := newTestEngine(t, mr)

	evt := baseEvent()
	if _, err := engine.Process(context.Background(), evt); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	day := evt.Timestamp.UTC().Format("2006-01-02")
	keys := [3]string{
		fmt.Sprintf("cost:user:%s:daily:%s", evt.UserID, day),
		fmt.Sprintf("cost:feature:%s:daily:%s", evt.FeatureName, day),
		fmt.Sprintf("cost:model:%s:daily:%s", evt.Model, day),
	}

	const delta = 5 * time.Second
	for _, key := range keys {
		ttl := mr.TTL(key)
		if ttl < costKeyTTL-delta || ttl > costKeyTTL+delta {
			t.Errorf("key %q TTL = %v, want ~%v", key, ttl, costKeyTTL)
		}
	}
}

// ---- retry loop detection tests ---------------------------------------------

func TestEngine_DetectsRetryLoopAfterThreshold(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, logs := newObservedEngine(t, mr, zapcore.WarnLevel)

	evt := baseEvent()

	// retryLoopThreshold+1 calls must trigger at least one warning.
	for i := 0; i <= retryLoopThreshold; i++ {
		evt.RequestID = fmt.Sprintf("req-%d", i)
		if _, err := engine.Process(context.Background(), evt); err != nil {
			t.Fatalf("Process() error on call %d: %v", i, err)
		}
	}

	if n := logs.FilterMessage("retry loop detected").Len(); n == 0 {
		t.Error("expected at least one 'retry loop detected' warning, got none")
	}
}

func TestEngine_NoRetryLoopWarningBelowThreshold(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, logs := newObservedEngine(t, mr, zapcore.WarnLevel)

	evt := baseEvent()

	// Exactly retryLoopThreshold calls — must NOT trigger a warning.
	for i := 0; i < retryLoopThreshold; i++ {
		evt.RequestID = fmt.Sprintf("req-%d", i)
		if _, err := engine.Process(context.Background(), evt); err != nil {
			t.Fatalf("Process() error on call %d: %v", i, err)
		}
	}

	if n := logs.FilterMessage("retry loop detected").Len(); n > 0 {
		t.Errorf("expected no retry loop warnings, got %d", n)
	}
}

func TestEngine_RetryLoopSkippedWhenFieldsEmpty(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, logs := newObservedEngine(t, mr, zapcore.WarnLevel)

	evt := baseEvent()
	evt.AgentStep = "" // missing step → retry detection must be skipped

	for i := 0; i <= retryLoopThreshold+10; i++ {
		evt.RequestID = fmt.Sprintf("req-%d", i)
		if _, err := engine.Process(context.Background(), evt); err != nil {
			t.Fatalf("Process() error on call %d: %v", i, err)
		}
	}

	if n := logs.FilterMessage("retry loop detected").Len(); n > 0 {
		t.Errorf("expected no retry loop warning when AgentStep is empty, got %d", n)
	}
}

// ---- CostRecord field tests -------------------------------------------------

func TestEngine_ReturnsCostRecordWithCorrectFields(t *testing.T) {
	mr := miniredis.RunT(t)
	engine, _ := newTestEngine(t, mr)

	evt := events.RequestEvent{
		RequestID:    "req-xyz",
		UserID:       "user-abc",
		FeatureName:  "feat-1",
		AgentStep:    "step-2",
		Model:        "claude-3-opus",
		Provider:     "anthropic",
		InputTokens:  200,
		OutputTokens: 100,
		Timestamp:    time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	record, err := engine.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	wantCost := 200*0.000015 + 100*0.000075

	if record.RequestID != evt.RequestID {
		t.Errorf("RequestID = %q, want %q", record.RequestID, evt.RequestID)
	}
	if record.UserID != evt.UserID {
		t.Errorf("UserID = %q, want %q", record.UserID, evt.UserID)
	}
	if record.FeatureName != evt.FeatureName {
		t.Errorf("FeatureName = %q, want %q", record.FeatureName, evt.FeatureName)
	}
	if record.AgentStep != evt.AgentStep {
		t.Errorf("AgentStep = %q, want %q", record.AgentStep, evt.AgentStep)
	}
	if record.Model != evt.Model {
		t.Errorf("Model = %q, want %q", record.Model, evt.Model)
	}
	if record.Provider != evt.Provider {
		t.Errorf("Provider = %q, want %q", record.Provider, evt.Provider)
	}
	if record.InputTokens != evt.InputTokens {
		t.Errorf("InputTokens = %d, want %d", record.InputTokens, evt.InputTokens)
	}
	if record.OutputTokens != evt.OutputTokens {
		t.Errorf("OutputTokens = %d, want %d", record.OutputTokens, evt.OutputTokens)
	}
	if !withinTolerance(record.CostUSD, wantCost) {
		t.Errorf("CostUSD = %.12f, want %.12f", record.CostUSD, wantCost)
	}
	if !record.Timestamp.Equal(evt.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", record.Timestamp, evt.Timestamp)
	}
}
