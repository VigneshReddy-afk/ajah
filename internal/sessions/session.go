package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ajah/core/internal/storage"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// SessionMeta holds aggregate metadata for a single session.
type SessionMeta struct {
	SessionID   string
	StartTime   time.Time
	LastSeen    time.Time
	TotalCost   float64
	TotalTokens int64
	StepCount   int32
	AvgQuality  float64
	FeatureName string
	UserID      string
	Status      string
}

// SessionSummary is the full session result returned by CloseSession.
type SessionSummary struct {
	SessionMeta
	Traces []storage.TraceRecord
}

// CircuitBreakerLimits holds per-feature agent limits.
// Zero values mean the limit is disabled.
type CircuitBreakerLimits struct {
	MaxStepsPerSession int
	MaxCostPerSession  float64
}

// CircuitBreakerTrippedError is returned by AddTrace when a session limit is exceeded.
type CircuitBreakerTrippedError struct {
	Reason    string
	SessionID string
	Steps     int32
	Cost      float64
}

func (e *CircuitBreakerTrippedError) Error() string {
	return fmt.Sprintf("circuit breaker tripped for session %s: %s (steps=%d cost=%.6f)",
		e.SessionID, e.Reason, e.Steps, e.Cost)
}

// IsCircuitBreakerTripped returns true if the error is a CircuitBreakerTrippedError.
func IsCircuitBreakerTripped(err error) bool {
	_, ok := err.(*CircuitBreakerTrippedError)
	return ok
}

// Accumulator buffers per-session traces in Redis and flushes completed
// sessions to ClickHouse.
type Accumulator struct {
	rdb            *redis.Client
	ch             *storage.Writer
	log            *zap.Logger
	sessionTimeout time.Duration
	reaperInterval time.Duration
	// limitsFunc returns circuit breaker limits for a feature name.
	// If nil, circuit breaking is disabled.
	limitsFunc func(feature string) CircuitBreakerLimits
}

// New constructs an Accumulator with a 5-minute session idle timeout and a
// 60-second reaper interval.
func New(rdb *redis.Client, w *storage.Writer, logger *zap.Logger) *Accumulator {
	return &Accumulator{
		rdb:            rdb,
		ch:             w,
		log:            logger,
		sessionTimeout: 5 * time.Minute,
		reaperInterval: 60 * time.Second,
	}
}

// SetSessionTimeout overrides the default 5-minute idle timeout.
func (a *Accumulator) SetSessionTimeout(d time.Duration) {
	a.sessionTimeout = d
}

// SetReaperInterval overrides the default 60-second reaper tick interval.
func (a *Accumulator) SetReaperInterval(d time.Duration) {
	a.reaperInterval = d
}

// SetLimitsFunc registers a function that returns circuit breaker limits for a feature.
// Called on every AddTrace — must be fast (should read from an in-memory cache).
func (a *Accumulator) SetLimitsFunc(fn func(feature string) CircuitBreakerLimits) {
	a.limitsFunc = fn
}

func metaKey(id string) string    { return "session:" + id + ":meta" }
func tracesKey(id string) string  { return "session:" + id + ":traces" }
func circuitKey(id string) string { return "ajah:circuit:tripped:" + id }

const sessionTTL = 24 * time.Hour

// DeadStepResult holds the result of dead step analysis.
type DeadStepResult struct {
	IsDeadStep      bool
	SimilarityScore float64
	PrevStepName    string
}

// AddTrace appends trace to the Redis-backed session buffer and updates the
// session metadata hash. Both Redis keys are refreshed to a 24h TTL on every call.
//
// Returns CircuitBreakerTrippedError if the session has exceeded configured
// step or cost limits. The caller (gateway handler) should return 429 in this case.
func (a *Accumulator) AddTrace(ctx context.Context, sessionID string, trace storage.TraceRecord) error {
	// ── Circuit breaker pre-check ────────────────────────────────────────────
	// If this session was already tripped, reject immediately without writing.
	ck := circuitKey(sessionID)
	if tripped, _ := a.rdb.Exists(ctx, ck).Result(); tripped > 0 {
		reason, _ := a.rdb.Get(ctx, ck).Result()
		return &CircuitBreakerTrippedError{
			Reason:    reason,
			SessionID: sessionID,
		}
	}

	data, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}

	mk := metaKey(sessionID)
	tk := tracesKey(sessionID)

	pipe := a.rdb.Pipeline()
	pipe.RPush(ctx, tk, string(data))
	pipe.HSetNX(ctx, mk, "start_time", trace.Timestamp.UnixNano())
	pipe.HSet(ctx, mk,
		"last_seen", time.Now().UnixNano(),
		"feature_name", trace.FeatureName,
		"user_id", trace.UserID,
	)
	pipe.HIncrByFloat(ctx, mk, "total_cost", trace.CostUSD)
	pipe.HIncrBy(ctx, mk, "total_tokens", int64(trace.InputTokens+trace.OutputTokens))
	pipe.HIncrBy(ctx, mk, "step_count", 1)
	pipe.Expire(ctx, tk, sessionTTL)
	pipe.Expire(ctx, mk, sessionTTL)

	results, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline: %w", err)
	}

	// Read updated counters from pipeline results (indices 3=cost incr, 5=step incr)
	var totalCost float64
	var stepCount int32
	if len(results) > 3 {
		if cmd, ok := results[3].(*redis.FloatCmd); ok {
			totalCost, _ = cmd.Result()
		}
	}
	if len(results) > 5 {
		if cmd, ok := results[5].(*redis.IntCmd); ok {
			n, _ := cmd.Result()
			stepCount = int32(n)
		}
	}

	// ── Circuit breaker limit check ──────────────────────────────────────────
	if a.limitsFunc != nil && trace.FeatureName != "" {
		limits := a.limitsFunc(trace.FeatureName)
		var tripReason string
		if limits.MaxStepsPerSession > 0 && int(stepCount) >= limits.MaxStepsPerSession {
			tripReason = fmt.Sprintf("step limit exceeded (%d/%d steps)", stepCount, limits.MaxStepsPerSession)
		}
		if limits.MaxCostPerSession > 0 && totalCost >= limits.MaxCostPerSession {
			tripReason = fmt.Sprintf("cost limit exceeded ($%.6f/$%.6f)", totalCost, limits.MaxCostPerSession)
		}
		if tripReason != "" {
			// Mark the session as tripped in Redis — TTL matches session idle timeout
			a.rdb.Set(ctx, ck, tripReason, sessionTTL)
			a.log.Warn("agent circuit breaker tripped",
				zap.String("session_id", sessionID),
				zap.String("feature", trace.FeatureName),
				zap.String("reason", tripReason),
				zap.Int32("steps", stepCount),
				zap.Float64("cost", totalCost),
			)
			return &CircuitBreakerTrippedError{
				Reason:    tripReason,
				SessionID: sessionID,
				Steps:     stepCount,
				Cost:      totalCost,
			}
		}
	}

	return nil
}

// CheckDeadStep compares the current trace response against the previous step
// in this session to detect redundant/looping agent steps.
// Returns a DeadStepResult — call this after AddTrace succeeds.
// A step is considered "dead" if it has identical or near-identical output to
// a prior step (simple string overlap > 85%).
func (a *Accumulator) CheckDeadStep(ctx context.Context, sessionID string, currentResponse string) DeadStepResult {
	if currentResponse == "" || len(currentResponse) < 20 {
		return DeadStepResult{}
	}

	tk := tracesKey(sessionID)
	// Get the last 5 traces to compare against
	rawTraces, err := a.rdb.LRange(ctx, tk, -6, -2).Result()
	if err != nil || len(rawTraces) == 0 {
		return DeadStepResult{}
	}

	// Compare against the most recent prior trace
	last := rawTraces[len(rawTraces)-1]
	var prevTrace storage.TraceRecord
	if err := json.Unmarshal([]byte(last), &prevTrace); err != nil {
		return DeadStepResult{}
	}

	if prevTrace.ResponseText == "" {
		return DeadStepResult{}
	}

	similarity := stringSimilarity(currentResponse, prevTrace.ResponseText)
	isDead := similarity > 0.85

	if isDead {
		a.log.Warn("dead step detected",
			zap.String("session_id", sessionID),
			zap.String("prev_step", prevTrace.StepName),
			zap.Float64("similarity", similarity),
		)
	}

	return DeadStepResult{
		IsDeadStep:      isDead,
		SimilarityScore: similarity,
		PrevStepName:    prevTrace.StepName,
	}
}

// stringSimilarity returns a simple overlap coefficient between two strings (0.0–1.0).
// Uses character trigram overlap — fast, no external deps, good enough for dead step detection.
func stringSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if len(a) < 3 || len(b) < 3 {
		return 0.0
	}
	// Truncate to first 500 chars for performance
	if len(a) > 500 {
		a = a[:500]
	}
	if len(b) > 500 {
		b = b[:500]
	}

	trigramsA := buildTrigrams(a)
	trigramsB := buildTrigrams(b)

	if len(trigramsA) == 0 || len(trigramsB) == 0 {
		return 0.0
	}

	intersection := 0
	for k := range trigramsA {
		if trigramsB[k] {
			intersection++
		}
	}

	smaller := len(trigramsA)
	if len(trigramsB) < smaller {
		smaller = len(trigramsB)
	}
	return float64(intersection) / float64(smaller)
}

func buildTrigrams(s string) map[string]bool {
	runes := []rune(s)
	out := make(map[string]bool, len(runes))
	for i := 0; i+2 < len(runes); i++ {
		out[string(runes[i:i+3])] = true
	}
	return out
}

// CloseSession reads the buffered session from Redis, writes it to ClickHouse,
// deletes the Redis keys, and returns the full SessionSummary.
func (a *Accumulator) CloseSession(ctx context.Context, sessionID string) (*SessionSummary, error) {
	mk := metaKey(sessionID)
	tk := tracesKey(sessionID)

	meta, err := a.rdb.HGetAll(ctx, mk).Result()
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", err)
	}

	rawTraces, err := a.rdb.LRange(ctx, tk, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("read traces: %w", err)
	}

	traces := make([]storage.TraceRecord, 0, len(rawTraces))
	for _, raw := range rawTraces {
		var tr storage.TraceRecord
		if err := json.Unmarshal([]byte(raw), &tr); err != nil {
			return nil, fmt.Errorf("unmarshal trace: %w", err)
		}
		traces = append(traces, tr)
	}

	sm := SessionMeta{SessionID: sessionID, Status: "completed"}

	if v, ok := meta["start_time"]; ok {
		if ns, err := strconv.ParseInt(v, 10, 64); err == nil {
			sm.StartTime = time.Unix(0, ns).UTC()
		}
	}
	if v, ok := meta["last_seen"]; ok {
		if ns, err := strconv.ParseInt(v, 10, 64); err == nil {
			sm.LastSeen = time.Unix(0, ns).UTC()
		}
	}
	if v, ok := meta["total_cost"]; ok {
		sm.TotalCost, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := meta["total_tokens"]; ok {
		n, _ := strconv.ParseInt(v, 10, 64)
		sm.TotalTokens = n
	}
	if v, ok := meta["step_count"]; ok {
		n, _ := strconv.ParseInt(v, 10, 64)
		sm.StepCount = int32(n)
	}
	sm.FeatureName = meta["feature_name"]
	sm.UserID = meta["user_id"]

	var qualSum float64
	var qualCount int
	for _, tr := range traces {
		if tr.QualityScore > 0 {
			qualSum += tr.QualityScore
			qualCount++
		}
	}
	if qualCount > 0 {
		sm.AvgQuality = qualSum / float64(qualCount)
	}

	summary := &SessionSummary{SessionMeta: sm, Traces: traces}

	if a.ch != nil {
		if err := a.ch.CreateSessionTable(ctx); err != nil {
			return nil, fmt.Errorf("create session table: %w", err)
		}
		if err := a.ch.WriteSession(ctx, storage.SessionRecord{
			SessionID:   sm.SessionID,
			StartTime:   sm.StartTime,
			LastSeen:    sm.LastSeen,
			TotalCost:   sm.TotalCost,
			TotalTokens: sm.TotalTokens,
			StepCount:   sm.StepCount,
			AvgQuality:  sm.AvgQuality,
			FeatureName: sm.FeatureName,
			UserID:      sm.UserID,
			Status:      sm.Status,
		}); err != nil {
			return nil, fmt.Errorf("write session: %w", err)
		}
	}

	if err := a.rdb.Del(ctx, mk, tk).Err(); err != nil {
		return nil, fmt.Errorf("delete redis keys: %w", err)
	}

	return summary, nil
}

// GetTurns returns all TraceRecords buffered in Redis for a session,
// in append order. Returns an empty slice if the session has no turns.
func (a *Accumulator) GetTurns(ctx context.Context, sessionID string) ([]storage.TraceRecord, error) {
	key := "session:" + sessionID + ":traces"
	data, err := a.rdb.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	var turns []storage.TraceRecord
	for _, d := range data {
		var tr storage.TraceRecord
		if err := json.Unmarshal([]byte(d), &tr); err != nil {
			continue
		}
		turns = append(turns, tr)
	}
	return turns, nil
}

// StartReaper launches a background goroutine that scans Redis every
// reaperInterval and closes sessions whose last_seen exceeds sessionTimeout.
// It stops cleanly when ctx is cancelled.
func (a *Accumulator) StartReaper(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(a.reaperInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.reapExpired(ctx)
			}
		}
	}()
}

func (a *Accumulator) reapExpired(ctx context.Context) {
	var cursor uint64
	var reaped int
	for {
		var keys []string
		var err error
		keys, cursor, err = a.rdb.Scan(ctx, cursor, "session:*:meta", 100).Result()
		if err != nil {
			a.log.Error("scan sessions", zap.Error(err))
			return
		}

		for _, key := range keys {
			id := strings.TrimPrefix(key, "session:")
			id = strings.TrimSuffix(id, ":meta")

			lastSeenStr, err := a.rdb.HGet(ctx, key, "last_seen").Result()
			if err != nil {
				continue
			}
			ns, err := strconv.ParseInt(lastSeenStr, 10, 64)
			if err != nil {
				continue
			}
			if time.Since(time.Unix(0, ns)) > a.sessionTimeout {
				if _, err := a.CloseSession(context.Background(), id); err != nil {
					a.log.Error("close expired session",
						zap.String("session_id", id), zap.Error(err))
					continue
				}
				reaped++
			}
		}
		if cursor == 0 {
			break
		}
	}
	if reaped > 0 {
		a.log.Info("reaped expired sessions", zap.Int("count", reaped))
	}
}
