package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ajah/core/internal/attribution"
	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/events"
	"github.com/ajah/core/internal/masking"
	"github.com/ajah/core/internal/proxy"
	"github.com/ajah/core/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Config ----------------------------------------------------------------
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// 2. Logger ----------------------------------------------------------------
	logger, err := buildLogger(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("starting ajah gateway",
		zap.String("version", version),
		zap.String("port", cfg.Port),
		zap.String("log_level", cfg.LogLevel),
	)

	// 3. Redis -----------------------------------------------------------------
	rdb, err := connectRedis(cfg, logger)
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() { _ = rdb.Close() }()

	// 4. ClickHouse writer -----------------------------------------------------
	writer, err := storage.New(cfg.ClickHouseURL, logger)
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}
	defer func() { _ = writer.Close() }()

	startCtx, startCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startCancel()
	if err := writer.CreateTable(startCtx); err != nil {
		return fmt.Errorf("create clickhouse table: %w", err)
	}

	// 5. PII masker ------------------------------------------------------------
	masker := masking.New(logger)

	// 6. Attribution engine ----------------------------------------------------
	engine := attribution.New(rdb, logger)

	// 7. Event emitter ---------------------------------------------------------
	const dailyKeyTTL = 90 * 24 * time.Hour

	bufferSize := cfg.AsyncWorkerCount * 10
	processFn := func(ctx context.Context, event events.RequestEvent) error {
		var firstErr error

		// Step 1: mask prompt
		maskResult := masker.Mask(event.Prompt)

		if maskResult.WasMasked {
			day := event.Timestamp.UTC().Format("2006-01-02")
			piiKey := fmt.Sprintf("pii:masked:daily:%s", day)
			if incErr := rdb.Incr(ctx, piiKey).Err(); incErr == nil {
				rdb.Expire(ctx, piiKey, dailyKeyTTL)
			} else {
				logger.Warn("failed to increment pii masked counter", zap.Error(incErr))
			}
		}

		// Step 2: attribution (always returns a CostRecord even on error)
		costRecord, attrErr := engine.Process(ctx, event)
		if attrErr != nil {
			firstErr = attrErr
		}

		// Step 3: write trace to ClickHouse
		record := storage.TraceRecord{
			TraceID:      event.RequestID,
			RequestID:    event.RequestID,
			UserID:       event.UserID,
			SessionID:    event.SessionID,
			FeatureName:  event.FeatureName,
			AgentStep:    event.AgentStep,
			Provider:     event.Provider,
			Model:        event.Model,
			InputTokens:  event.InputTokens,
			OutputTokens: event.OutputTokens,
			CostUSD:      costRecord.CostUSD,
			LatencyMs:    event.LatencyMs,
			StatusCode:   event.StatusCode,
			MaskedPrompt: maskResult.Masked,
			WasPIIMasked: maskResult.WasMasked,
			QualityScore: 0,
			Timestamp:    event.Timestamp,
		}
		writeErr := writer.Write(ctx, record)
		if writeErr != nil {
			if firstErr == nil {
				firstErr = writeErr
			}
		} else {
			day := event.Timestamp.UTC().Format("2006-01-02")
			tracesKey := fmt.Sprintf("traces:daily:%s", day)
			if incErr := rdb.Incr(ctx, tracesKey).Err(); incErr == nil {
				rdb.Expire(ctx, tracesKey, dailyKeyTTL)
			} else {
				logger.Warn("failed to increment traces counter", zap.Error(incErr))
			}
		}

		return firstErr
	}

	emitter := events.New(bufferSize, cfg.AsyncWorkerCount, logger, processFn)

	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	defer cancelEmitter()
	emitter.Start(emitterCtx)

	// 8. Proxy handler ---------------------------------------------------------
	proxyHandler := proxy.New(cfg, emitter, logger)

	// 9. Router ----------------------------------------------------------------
	r := chi.NewRouter()
	r.Post("/v1/chat/completions", proxyHandler.ServeHTTP)
	r.Get("/health", healthHandler)
	r.Get("/metrics/cost", costMetricsHandler(rdb, logger))

	// 10. HTTP server ----------------------------------------------------------
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("gateway listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}

	// 11. Graceful shutdown ----------------------------------------------------
	logger.Info("shutting down: draining in-flight requests")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", zap.Error(err))
	}

	cancelEmitter()
	emitter.Stop()

	logger.Info("shutdown complete")
	return nil
}

func buildLogger(level string) (*zap.Logger, error) {
	if level == "debug" {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}

func connectRedis(cfg *config.Config, logger *zap.Logger) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL %q: %w", cfg.RedisURL, err)
	}

	rdb := redis.NewClient(opts)

	const (
		maxAttempts = 5
		retryDelay  = 2 * time.Second
	)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr := rdb.Ping(pingCtx).Err()
		cancel()

		if pingErr == nil {
			logger.Info("connected to Redis",
				zap.String("addr", opts.Addr),
				zap.Int("attempt", attempt),
			)
			return rdb, nil
		}

		logger.Warn("redis ping failed",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", maxAttempts),
			zap.Error(pingErr),
		)

		if attempt < maxAttempts {
			time.Sleep(retryDelay)
		}
	}

	return nil, fmt.Errorf("redis unreachable after %d attempts", maxAttempts)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok","version":"` + version + `"}`))
}

func costMetricsHandler(rdb *redis.Client, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		today := time.Now().UTC().Format("2006-01-02")

		byUser, err := scanCostKeys(ctx, rdb, logger,
			fmt.Sprintf("cost:user:*:daily:%s", today), "cost:user:", ":daily:")
		if err != nil {
			http.Error(w, "failed to read user costs", http.StatusInternalServerError)
			return
		}

		byFeature, err := scanCostKeys(ctx, rdb, logger,
			fmt.Sprintf("cost:feature:*:daily:%s", today), "cost:feature:", ":daily:")
		if err != nil {
			http.Error(w, "failed to read feature costs", http.StatusInternalServerError)
			return
		}

		totalTraces := readCounter(ctx, rdb, logger, fmt.Sprintf("traces:daily:%s", today))
		piiMaskedCount := readCounter(ctx, rdb, logger, fmt.Sprintf("pii:masked:daily:%s", today))

		payload := map[string]interface{}{
			"date":            today,
			"by_user":         byUser,
			"by_feature":      byFeature,
			"total_traces":    totalTraces,
			"pii_masked_count": piiMaskedCount,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			logger.Warn("failed to encode metrics response", zap.Error(err))
		}
	}
}

// readCounter returns the integer value of a Redis key, or 0 if absent or on error.
func readCounter(ctx context.Context, rdb *redis.Client, logger *zap.Logger, key string) int64 {
	val, err := rdb.Get(ctx, key).Int64()
	if err != nil && err != redis.Nil {
		logger.Warn("failed to read counter", zap.String("key", key), zap.Error(err))
	}
	return val
}

func scanCostKeys(
	ctx context.Context,
	rdb *redis.Client,
	logger *zap.Logger,
	pattern, prefix, suffix string,
) (map[string]float64, error) {
	result := make(map[string]float64)

	iter := rdb.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		val, err := rdb.Get(ctx, key).Float64()
		if err != nil {
			logger.Warn("failed to read cost key", zap.String("key", key), zap.Error(err))
			continue
		}
		result[extractSegment(key, prefix, suffix)] = val
	}
	if err := iter.Err(); err != nil {
		logger.Error("redis SCAN failed", zap.String("pattern", pattern), zap.Error(err))
		return nil, err
	}
	return result, nil
}

func extractSegment(key, prefix, suffix string) string {
	s := strings.TrimPrefix(key, prefix)
	if idx := strings.LastIndex(s, suffix); idx >= 0 {
		return s[:idx]
	}
	return s
}
