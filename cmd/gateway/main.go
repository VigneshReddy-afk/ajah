package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ajah/core/internal/attribution"
	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/db"
	"github.com/ajah/core/internal/events"
	"github.com/ajah/core/internal/flagging"
	"github.com/ajah/core/internal/masking"
	"github.com/ajah/core/internal/proxy"
	"github.com/ajah/core/internal/sessions"
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
	if err := writer.CreateSessionTable(startCtx); err != nil {
		return fmt.Errorf("create session table: %w", err)
	}
	if err := writer.MigrateTable(startCtx); err != nil {
		return fmt.Errorf("migrate traces table: %w", err)
	}

	// 5. PostgreSQL settings store ---------------------------------------------
	dbStore, err := db.New(cfg.DatabaseURL, logger)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer func() { _ = dbStore.Close() }()

	if err := dbStore.CreateTables(startCtx); err != nil {
		return fmt.Errorf("create postgres tables: %w", err)
	}
	if err := dbStore.MigrateTables(startCtx); err != nil {
		return fmt.Errorf("migrate postgres tables: %w", err)
	}
	dbStore.RefreshCache(startCtx)

	// 6. PII masker ------------------------------------------------------------
	masker := masking.New(logger)

	// 7. Attribution engine ----------------------------------------------------
	engine := attribution.New(rdb, logger)

	// 8. Scorer HTTP client (best-effort; never fails the main pipeline) --------
	scorerClient := &http.Client{Timeout: 10 * time.Second}

	// 8b. Risk flagger (async; never blocks the response path) -----------------
	flagger := flagging.New(cfg.ScorerURL, logger)
	webhookClient := &http.Client{Timeout: time.Duration(cfg.WebhookTimeoutSeconds) * time.Second}

	// 9. Session accumulator ---------------------------------------------------
	acc := sessions.New(rdb, writer, logger)
	acc.SetSessionTimeout(time.Duration(cfg.SessionTimeout) * time.Second)

	// 10. Event emitter --------------------------------------------------------
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

		// Step 3: quality scoring (best-effort; only for successful responses)
		qualityScore := 0.0
		var scorerOut scorerOutcome
		if event.StatusCode == http.StatusOK {
			scoreCtx, scoreCancel := context.WithTimeout(ctx, 10*time.Second)
			scorerOut = callScorer(scoreCtx, scorerClient, cfg.ScorerURL, event, logger)
			qualityScore = scorerOut.OverallQualityScore
			scoreCancel()
		}

		// Step 3b: risk flagging (async; never blocks; only for successful responses)
		var riskFlag flagging.RiskFlag
		if event.StatusCode == http.StatusOK {
			flagCtx, flagCancel := context.WithTimeout(ctx, 10*time.Second)
			riskFlag = flagger.Evaluate(flagCtx, event.RequestID, event.SessionID, event.Prompt, event.Response, qualityScore, scorerOut.RAGVerdict)
			flagCancel()

			if riskFlag.ShouldWarn {
				day := event.Timestamp.UTC().Format("2006-01-02")
				warnCountKey := fmt.Sprintf("warnings:daily:%s", day)
				if incErr := rdb.Incr(ctx, warnCountKey).Err(); incErr == nil {
					rdb.Expire(ctx, warnCountKey, dailyKeyTTL)
				}
			}
			if riskFlag.RiskLevel == "high" {
				wi := warningItem{
					RequestID:         riskFlag.RequestID,
					SessionID:         riskFlag.SessionID,
					FeatureName:       event.FeatureName,
					RiskLevel:         riskFlag.RiskLevel,
					HallucinationRisk: riskFlag.HallucinationRisk,
					GroundingScore:    riskFlag.GroundingScore,
					Reasons:           riskFlag.Reasons,
					Timestamp:         event.Timestamp,
				}
				if wiJSON, marshalErr := json.Marshal(wi); marshalErr == nil {
					rdb.LPush(ctx, "warnings:high:list", string(wiJSON))
					rdb.LTrim(ctx, "warnings:high:list", 0, 99)
				}
			}

			// Store per-request flag so apps can poll GET /warnings/{requestID}.
			fr := flagResult{
				RequestID: riskFlag.RequestID,
				Flagged:   riskFlag.ShouldWarn,
				RiskLevel: riskFlag.RiskLevel,
				Reasons:   riskFlag.Reasons,
			}
			if frJSON, marshalErr := json.Marshal(fr); marshalErr == nil {
				rdb.Set(ctx, "flag:"+event.RequestID, frJSON, time.Hour)
			}

			// Webhook delivery (opt-in per feature; fire-and-forget).
			if riskFlag.ShouldWarn && event.FeatureName != "" {
				if webhookURL := dbStore.WebhookURLFor(event.FeatureName); webhookURL != "" {
					go fireWebhook(webhookURL, riskFlag, webhookClient, logger)
				}
			}
		}

		// Step 4: write trace to ClickHouse
		record := storage.TraceRecord{
			TraceID:               event.RequestID,
			RequestID:             event.RequestID,
			UserID:                event.UserID,
			SessionID:             event.SessionID,
			FeatureName:           event.FeatureName,
			AgentStep:             event.AgentStep,
			ParentStepID:          event.ParentStepID,
			StepName:              event.StepName,
			ToolName:              event.ToolName,
			Provider:              event.Provider,
			Model:                 event.Model,
			InputTokens:           event.InputTokens,
			OutputTokens:          event.OutputTokens,
			CostUSD:               costRecord.CostUSD,
			LatencyMs:             event.LatencyMs,
			StatusCode:            event.StatusCode,
			MaskedPrompt:          maskResult.Masked,
			WasPIIMasked:          maskResult.WasMasked,
			QualityScore:          qualityScore,
			Timestamp:             event.Timestamp,
			HallucinationRisk:     riskFlag.HallucinationRisk,
			GroundingScore:        riskFlag.GroundingScore,
			RiskLevel:             riskFlag.RiskLevel,
			ShouldWarn:            riskFlag.ShouldWarn,
			RAGVerdict:            scorerOut.RAGVerdict,
			RAGGroundingScore:     scorerOut.RAGGroundingScore,
			RAGContradictionScore: scorerOut.RAGContradictionScore,
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

			// Check cost spike alert for this feature
			if event.FeatureName != "" {
				featureCostKey := fmt.Sprintf("cost:feature:%s:daily:%s", event.FeatureName, day)
				featureCost, costErr := rdb.Get(ctx, featureCostKey).Float64()
				if costErr == nil {
					threshold := dbStore.ThresholdFor(event.FeatureName)
					if featureCost > threshold {
						firedKey := fmt.Sprintf("alerts:fired:%s:%s", event.FeatureName, day)
						set, setErr := rdb.SetNX(ctx, firedKey, "1", 25*time.Hour).Result()
						if setErr == nil && set {
							alert := alertResponse{
								Timestamp: time.Now().UTC(),
								Type:      "cost_spike",
								Feature:   event.FeatureName,
								Threshold: threshold,
								Actual:    featureCost,
							}
							if alertJSON, marshalErr := json.Marshal(alert); marshalErr == nil {
								rdb.LPush(ctx, "alerts:list", string(alertJSON))
								rdb.LTrim(ctx, "alerts:list", 0, 99)
								logger.Warn("cost spike alert fired",
									zap.String("feature", event.FeatureName),
									zap.Float64("threshold", threshold),
									zap.Float64("actual", featureCost),
								)
							}
						}
					}
				}
			}
		}

		// Step 5: session accumulation (best-effort; never breaks the pipeline)
		if event.SessionID != "" {
			if addErr := acc.AddTrace(ctx, event.SessionID, record); addErr != nil {
				logger.Warn("session trace accumulation failed",
					zap.String("session_id", event.SessionID),
					zap.Error(addErr),
				)
			}
		}

		return firstErr
	}

	emitter := events.New(bufferSize, cfg.AsyncWorkerCount, logger, processFn)

	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	defer cancelEmitter()
	emitter.Start(emitterCtx)
	acc.StartReaper(emitterCtx)

	// Background: refresh threshold cache every 60 s so settings changes
	// take effect without a restart.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				dbStore.RefreshCache(context.Background())
			case <-emitterCtx.Done():
				return
			}
		}
	}()

	// 10. Proxy handler --------------------------------------------------------
	proxyHandler := proxy.New(cfg, emitter, logger)

	// 11. Router ---------------------------------------------------------------
	r := chi.NewRouter()
	r.Use(corsMiddleware)

	r.Post("/v1/chat/completions", proxyHandler.ServeHTTP)
	r.Get("/health", healthHandler)
	r.Get("/metrics/cost", costMetricsHandler(rdb, logger))
	r.Get("/metrics/traces", tracesHandler(writer, logger))
	r.Get("/metrics/alerts", alertsHandler(rdb, logger))
	r.Get("/settings", getSettingsHandler(dbStore, logger))
	r.Post("/settings", postSettingsHandler(dbStore, logger))
	r.Get("/sessions", sessionsHandler(writer, logger))
	r.Get("/sessions/{sessionID}", sessionDetailHandler(writer, logger))
	r.Get("/warnings", warningsHandler(rdb, logger))
	r.Get("/warnings/{requestID}", warningByRequestIDHandler(rdb, logger))

	// 12. HTTP server ----------------------------------------------------------
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

	// 13. Graceful shutdown ----------------------------------------------------
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

		byModel, err := scanCostKeys(ctx, rdb, logger,
			fmt.Sprintf("cost:model:*:daily:%s", today), "cost:model:", ":daily:")
		if err != nil {
			http.Error(w, "failed to read model costs", http.StatusInternalServerError)
			return
		}

		totalTraces := readCounter(ctx, rdb, logger, fmt.Sprintf("traces:daily:%s", today))
		piiMaskedCount := readCounter(ctx, rdb, logger, fmt.Sprintf("pii:masked:daily:%s", today))

		payload := map[string]interface{}{
			"date":             today,
			"by_user":          byUser,
			"by_feature":       byFeature,
			"by_model":         byModel,
			"total_traces":     totalTraces,
			"pii_masked_count": piiMaskedCount,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			logger.Warn("failed to encode metrics response", zap.Error(err))
		}
	}
}

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

type scorerPayload struct {
	RequestID     string `json:"request_id"`
	Prompt        string `json:"prompt"`
	Response      string `json:"response"`
	Model         string `json:"model"`
	FeatureName   string `json:"feature_name"`
	SourceContext string `json:"source_context,omitempty"`
}

type scorerOutcome struct {
	OverallQualityScore   float64  `json:"overall_quality_score"`
	RAGVerdict            string   `json:"rag_verdict"`
	RAGGroundingScore     float64  `json:"rag_grounding_score"`
	RAGContradictionScore float64  `json:"rag_contradiction_score"`
	RAGSupportedClaims    []string `json:"rag_supported_claims"`
	RAGUnsupportedClaims  []string `json:"rag_unsupported_claims"`
	RAGContradictedClaims []string `json:"rag_contradicted_claims"`
}

// callScorer posts to the quality scorer service and returns the full scorerOutcome.
// Any failure (network, decode, etc.) logs a warning and returns a zero-value
// outcome so the main pipeline is never blocked by scorer availability.
func callScorer(ctx context.Context, client *http.Client, baseURL string, event events.RequestEvent, logger *zap.Logger) scorerOutcome {
	scorerURL := baseURL + "/score"
	logger.Debug("scorer: calling", zap.String("url", scorerURL), zap.String("request_id", event.RequestID))

	body, err := json.Marshal(scorerPayload{
		RequestID:     event.RequestID,
		Prompt:        event.Prompt,
		Response:      event.Response,
		Model:         event.Model,
		FeatureName:   event.FeatureName,
		SourceContext: event.SourceContext,
	})
	if err != nil {
		logger.Warn("scorer: marshal failed", zap.Error(err))
		return scorerOutcome{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, scorerURL, bytes.NewReader(body))
	if err != nil {
		logger.Warn("scorer: build request failed", zap.Error(err))
		return scorerOutcome{}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("scorer: unreachable", zap.String("url", scorerURL), zap.Error(err))
		return scorerOutcome{}
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Warn("scorer: read response failed", zap.Error(err))
		return scorerOutcome{}
	}
	if resp.StatusCode != http.StatusOK {
		logger.Warn("scorer: non-200 status",
			zap.String("request_id", event.RequestID),
			zap.Int("status_code", resp.StatusCode),
			zap.String("body", string(rawBody)),
		)
		return scorerOutcome{}
	}
	logger.Debug("scorer: raw response",
		zap.String("request_id", event.RequestID),
		zap.Int("status_code", resp.StatusCode),
		zap.String("body", string(rawBody)),
	)

	var outcome scorerOutcome
	if err := json.Unmarshal(rawBody, &outcome); err != nil {
		logger.Warn("scorer: decode response failed", zap.String("body", string(rawBody)), zap.Error(err))
		return scorerOutcome{}
	}
	logger.Info("scorer: quality score extracted",
		zap.String("request_id", event.RequestID),
		zap.Float64("quality_score", outcome.OverallQualityScore),
		zap.String("rag_verdict", outcome.RAGVerdict),
	)
	return outcome
}

func extractSegment(key, prefix, suffix string) string {
	s := strings.TrimPrefix(key, prefix)
	if idx := strings.LastIndex(s, suffix); idx >= 0 {
		return s[:idx]
	}
	return s
}

// fireWebhook POSTs the RiskFlag JSON to webhookURL. It retries once on any
// non-2xx response or network error. Always fire-and-forget — never blocks.
func fireWebhook(webhookURL string, flag flagging.RiskFlag, client *http.Client, logger *zap.Logger) {
	body, err := json.Marshal(flag)
	if err != nil {
		logger.Warn("webhook: marshal failed", zap.Error(err))
		return
	}
	for attempt := range 2 {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, webhookURL, bytes.NewReader(body))
		if err != nil {
			logger.Warn("webhook: build request failed", zap.Error(err))
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if attempt == 0 {
				logger.Warn("webhook: attempt failed, retrying", zap.String("url", webhookURL), zap.Error(err))
				continue
			}
			logger.Warn("webhook: failed after retry", zap.String("url", webhookURL), zap.Error(err))
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
		if attempt == 0 {
			logger.Warn("webhook: non-2xx, retrying", zap.String("url", webhookURL), zap.Int("status", resp.StatusCode))
			continue
		}
		logger.Warn("webhook: non-2xx after retry", zap.String("url", webhookURL), zap.Int("status", resp.StatusCode))
	}
}
