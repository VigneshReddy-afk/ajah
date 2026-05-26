package attribution

import (
	"context"
	"fmt"
	"time"

	"github.com/ajah/core/internal/events"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	retryLoopThreshold = 5
	retryWindow        = 60 * time.Second
	costKeyTTL         = 90 * 24 * time.Hour
)

type modelPrice struct {
	InputPerToken  float64
	OutputPerToken float64
}

// pricingTable is read-only after package init — not global mutable state.
var pricingTable = map[string]modelPrice{
	"gpt-4o":            {InputPerToken: 0.000005, OutputPerToken: 0.000015},
	"gpt-4-turbo":       {InputPerToken: 0.00001, OutputPerToken: 0.00003},
	"claude-3-5-sonnet": {InputPerToken: 0.000003, OutputPerToken: 0.000015},
	"claude-3-opus":     {InputPerToken: 0.000015, OutputPerToken: 0.000075},
	"gemini-1.5-pro":    {InputPerToken: 0.00000125, OutputPerToken: 0.000005},
}

var defaultPrice = modelPrice{InputPerToken: 0.00001, OutputPerToken: 0.00003}

// CostRecord is the attribution output for a single request.
type CostRecord struct {
	RequestID    string
	UserID       string
	FeatureName  string
	AgentStep    string
	Model        string
	Provider     string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Timestamp    time.Time
}

// Engine calculates request costs and publishes daily counters to Redis.
type Engine struct {
	rdb    *redis.Client
	logger *zap.Logger
}

// New creates an Engine backed by the given Redis client.
func New(rdb *redis.Client, logger *zap.Logger) *Engine {
	return &Engine{
		rdb:    rdb,
		logger: logger,
	}
}

// Process calculates the USD cost for event, increments the three daily cost
// counters in Redis, checks for retry loops, and returns a CostRecord for
// downstream storage.
func (e *Engine) Process(ctx context.Context, event events.RequestEvent) (CostRecord, error) {
	price := lookupPrice(event.Model)
	costUSD := float64(event.InputTokens)*price.InputPerToken +
		float64(event.OutputTokens)*price.OutputPerToken

	record := CostRecord{
		RequestID:    event.RequestID,
		UserID:       event.UserID,
		FeatureName:  event.FeatureName,
		AgentStep:    event.AgentStep,
		Model:        event.Model,
		Provider:     event.Provider,
		InputTokens:  event.InputTokens,
		OutputTokens: event.OutputTokens,
		CostUSD:      costUSD,
		Timestamp:    event.Timestamp,
	}

	if err := e.publishCost(ctx, event, costUSD); err != nil {
		return record, err
	}

	e.checkRetryLoop(ctx, event)

	return record, nil
}

// publishCost increments the three daily cost keys in Redis and sets their TTL.
func (e *Engine) publishCost(ctx context.Context, event events.RequestEvent, costUSD float64) error {
	day := event.Timestamp.UTC().Format("2006-01-02")

	keys := [3]string{
		fmt.Sprintf("cost:user:%s:daily:%s", event.UserID, day),
		fmt.Sprintf("cost:feature:%s:daily:%s", event.FeatureName, day),
		fmt.Sprintf("cost:model:%s:daily:%s", event.Model, day),
	}

	for _, key := range keys {
		if err := e.rdb.IncrByFloat(ctx, key, costUSD).Err(); err != nil {
			e.logger.Error("failed to increment cost counter",
				zap.String("key", key),
				zap.Error(err),
			)
			return fmt.Errorf("redis INCRBYFLOAT %q: %w", key, err)
		}
		// Non-fatal: a missing TTL just means the key outlives the intended
		// 90-day window; cost data is not lost.
		if err := e.rdb.Expire(ctx, key, costKeyTTL).Err(); err != nil {
			e.logger.Warn("failed to set TTL on cost key",
				zap.String("key", key),
				zap.Error(err),
			)
		}
	}
	return nil
}

// checkRetryLoop detects when the same UserID+FeatureName+AgentStep fires more
// than retryLoopThreshold times within a retryWindow and logs a warning.
func (e *Engine) checkRetryLoop(ctx context.Context, event events.RequestEvent) {
	if event.UserID == "" || event.FeatureName == "" || event.AgentStep == "" {
		return
	}

	key := fmt.Sprintf("retry:%s:%s:%s", event.UserID, event.FeatureName, event.AgentStep)

	count, err := e.rdb.Incr(ctx, key).Result()
	if err != nil {
		e.logger.Warn("failed to increment retry loop counter",
			zap.String("key", key),
			zap.Error(err),
		)
		return
	}

	// Set the expiry only on first increment so the window starts from the
	// first event, not rolling on every call.
	if count == 1 {
		if err := e.rdb.Expire(ctx, key, retryWindow).Err(); err != nil {
			e.logger.Warn("failed to set retry window TTL",
				zap.String("key", key),
				zap.Error(err),
			)
		}
	}

	if count > retryLoopThreshold {
		e.logger.Warn("retry loop detected",
			zap.String("user_id", event.UserID),
			zap.String("feature_name", event.FeatureName),
			zap.String("agent_step", event.AgentStep),
			zap.String("request_id", event.RequestID),
			zap.Int64("count_in_window", count),
		)
	}
}

func lookupPrice(model string) modelPrice {
	if p, ok := pricingTable[model]; ok {
		return p
	}
	return defaultPrice
}
