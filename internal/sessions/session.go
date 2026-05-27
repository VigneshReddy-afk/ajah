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

// Accumulator buffers per-session traces in Redis and flushes completed
// sessions to ClickHouse.
type Accumulator struct {
	rdb            *redis.Client
	ch             *storage.Writer
	log            *zap.Logger
	sessionTimeout time.Duration
	reaperInterval time.Duration
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

func metaKey(id string) string   { return "session:" + id + ":meta" }
func tracesKey(id string) string { return "session:" + id + ":traces" }

const sessionTTL = 24 * time.Hour

// AddTrace appends trace to the Redis-backed session buffer and updates the
// session metadata hash. Both Redis keys are refreshed to a 24 h TTL on every
// call.
func (a *Accumulator) AddTrace(ctx context.Context, sessionID string, trace storage.TraceRecord) error {
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

	if _, err = pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline: %w", err)
	}
	return nil
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
