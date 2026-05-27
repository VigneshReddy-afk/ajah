package sessions

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ajah/core/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newTestAccumulator(t *testing.T) (*Accumulator, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	return New(rdb, nil, zap.NewNop()), mr
}

func sampleTrace() storage.TraceRecord {
	return storage.TraceRecord{
		TraceID:      "trace-1",
		UserID:       "user-1",
		SessionID:    "sess-1",
		FeatureName:  "chat",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
		QualityScore: 0.8,
		Timestamp:    time.Now().UTC(),
	}
}

func TestAddTrace_CreatesMeta(t *testing.T) {
	a, _ := newTestAccumulator(t)
	ctx := context.Background()

	if err := a.AddTrace(ctx, "sess-1", sampleTrace()); err != nil {
		t.Fatalf("AddTrace: %v", err)
	}

	meta, err := a.rdb.HGetAll(ctx, metaKey("sess-1")).Result()
	if err != nil {
		t.Fatalf("HGetAll: %v", err)
	}
	if meta["user_id"] != "user-1" {
		t.Errorf("user_id = %q, want %q", meta["user_id"], "user-1")
	}
	if meta["feature_name"] != "chat" {
		t.Errorf("feature_name = %q, want %q", meta["feature_name"], "chat")
	}
	if meta["step_count"] != "1" {
		t.Errorf("step_count = %q, want %q", meta["step_count"], "1")
	}
	if _, ok := meta["start_time"]; !ok {
		t.Error("start_time not set in meta")
	}
}

func TestAddTrace_AccumulatesCost(t *testing.T) {
	a, _ := newTestAccumulator(t)
	ctx := context.Background()

	tr := sampleTrace()
	tr.CostUSD = 0.01
	for i := 0; i < 2; i++ {
		if err := a.AddTrace(ctx, "sess-1", tr); err != nil {
			t.Fatalf("AddTrace %d: %v", i, err)
		}
	}

	costStr, err := a.rdb.HGet(ctx, metaKey("sess-1"), "total_cost").Result()
	if err != nil {
		t.Fatalf("HGet total_cost: %v", err)
	}
	cost, _ := strconv.ParseFloat(costStr, 64)
	if cost < 0.0199 || cost > 0.0201 {
		t.Errorf("total_cost = %f, want ~0.02", cost)
	}
}

func TestAddTrace_UpdatesLastSeen(t *testing.T) {
	a, _ := newTestAccumulator(t)
	ctx := context.Background()

	before := time.Now().Add(-time.Millisecond)
	if err := a.AddTrace(ctx, "sess-1", sampleTrace()); err != nil {
		t.Fatalf("AddTrace: %v", err)
	}

	lastSeenStr, err := a.rdb.HGet(ctx, metaKey("sess-1"), "last_seen").Result()
	if err != nil {
		t.Fatalf("HGet last_seen: %v", err)
	}
	ns, _ := strconv.ParseInt(lastSeenStr, 10, 64)
	lastSeen := time.Unix(0, ns)
	if !lastSeen.After(before) {
		t.Errorf("last_seen %v not after %v", lastSeen, before)
	}
}

func TestCloseSession_ComputesAvgQuality(t *testing.T) {
	a, _ := newTestAccumulator(t)
	ctx := context.Background()

	for _, q := range []float64{0.8, 0.6, 0.0} {
		tr := storage.TraceRecord{
			FeatureName:  "chat",
			QualityScore: q,
			CostUSD:      0.01,
			Timestamp:    time.Now().UTC(),
		}
		if err := a.AddTrace(ctx, "sess-1", tr); err != nil {
			t.Fatalf("AddTrace: %v", err)
		}
	}

	summary, err := a.CloseSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	const wantAvg = 0.7
	const tolerance = 0.001
	if summary.AvgQuality < wantAvg-tolerance || summary.AvgQuality > wantAvg+tolerance {
		t.Errorf("AvgQuality = %f, want ~%f", summary.AvgQuality, wantAvg)
	}
}

func TestCloseSession_DeletesRedisKeys(t *testing.T) {
	a, _ := newTestAccumulator(t)
	ctx := context.Background()

	if err := a.AddTrace(ctx, "sess-1", sampleTrace()); err != nil {
		t.Fatalf("AddTrace: %v", err)
	}
	if _, err := a.CloseSession(ctx, "sess-1"); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	metaExists, _ := a.rdb.Exists(ctx, metaKey("sess-1")).Result()
	tracesExist, _ := a.rdb.Exists(ctx, tracesKey("sess-1")).Result()
	if metaExists != 0 {
		t.Error("meta key should be deleted after CloseSession")
	}
	if tracesExist != 0 {
		t.Error("traces key should be deleted after CloseSession")
	}
}

func TestStartReaper_ClosesExpiredSessions(t *testing.T) {
	a, _ := newTestAccumulator(t)
	a.sessionTimeout = time.Millisecond
	a.reaperInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := a.AddTrace(ctx, "sess-expired", sampleTrace()); err != nil {
		t.Fatalf("AddTrace: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // ensure session is past timeout

	a.StartReaper(ctx)

	time.Sleep(200 * time.Millisecond) // let reaper run multiple times

	metaExists, _ := a.rdb.Exists(context.Background(), metaKey("sess-expired")).Result()
	if metaExists != 0 {
		t.Error("expected session to be reaped but meta key still exists")
	}
}
