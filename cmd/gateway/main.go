package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ajah/core/internal/anomaly"
	"github.com/ajah/core/internal/attribution"
	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/crossmodel"
	"github.com/ajah/core/internal/db"
	"github.com/ajah/core/internal/events"
	"github.com/ajah/core/internal/flagging"
	"github.com/ajah/core/internal/masking"
	"github.com/ajah/core/internal/metrics"
	"github.com/ajah/core/internal/proxy"
	"github.com/ajah/core/internal/security"
	"github.com/ajah/core/internal/sessions"
	"github.com/ajah/core/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const version = "0.1.0"

var tracer = otel.Tracer("ajah-gateway")

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
	scorerClient := &http.Client{
		Timeout: time.Duration(cfg.ScorerTimeoutSeconds) * time.Second,
	}

	// 8b. Risk flagger (async; never blocks the response path) -----------------
	flagger := flagging.New(logger)
	webhookClient := &http.Client{Timeout: time.Duration(cfg.WebhookTimeoutSeconds) * time.Second}

	// 8c. Cross-model verifier (opt-in per feature) ----------------------------
	verifier := crossmodel.New(cfg.ScorerURL, logger)

	// 9. Session accumulator ---------------------------------------------------
	acc := sessions.New(rdb, writer, logger)
	acc.SetSessionTimeout(time.Duration(cfg.SessionTimeout) * time.Second)

	// 9b. Anomaly detector -----------------------------------------------------
	anomalyDetector := anomaly.New(writer, logger)
	// Best-effort initial load — no traces yet on a fresh deployment is fine.
	_ = anomalyDetector.RefreshBaselines(context.Background())

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
			scoreCtx, scoreCancel := context.WithTimeout(ctx, time.Duration(cfg.ScorerTimeoutSeconds)*time.Second)
			scorerStart := time.Now()
			scorerOut = callScorer(scoreCtx, scorerClient, cfg.ScorerURL, event, acc, logger)
			metrics.ScorerLatencyMs.Observe(float64(time.Since(scorerStart).Milliseconds()))
			qualityScore = scorerOut.OverallQualityScore
			scoreCancel()
		}

		// Step 3b: risk flagging (async; never blocks; only for successful responses)
		var riskFlag flagging.RiskFlag
		var crossModelVerdict string
		var crossModelAgreement float64
		if event.StatusCode == http.StatusOK {
			flagCtx, flagCancel := context.WithTimeout(ctx, 10*time.Second)
			riskFlag = flagger.Evaluate(
					flagCtx,
					event.RequestID,
					event.SessionID,
					scorerOut.HallucinationScore,
					scorerOut.FactualConsistencyScore,
					scorerOut.ClaimDensityRisk,
					scorerOut.HedgeRisk,
					scorerOut.DriftRisk,
					scorerOut.Flags,
					scorerOut.RAGVerdict,
				)
			flagCancel()

			// Step 3c: cross-model verification (opt-in per feature; best-effort)
			if event.FeatureName != "" {
				if fs := dbStore.FeatureSettingFor(event.FeatureName); fs != nil && fs.CrossModelEnabled &&
					fs.CrossModelProviderURL != "" && fs.CrossModelAPIKey != "" && fs.CrossModelModel != "" {
					verifyCtx, verifyCancel := context.WithTimeout(ctx, 30*time.Second)
					result, verifyErr := verifier.Verify(
						verifyCtx,
						event.Prompt, event.Response, event.Model,
						fs.CrossModelProviderURL, fs.CrossModelAPIKey, fs.CrossModelModel,
					)
					verifyCancel()
					if verifyErr != nil {
						logger.Warn("cross-model: verification failed",
							zap.String("feature", event.FeatureName), zap.Error(verifyErr))
					} else if result != nil {
						crossModelVerdict = result.Verdict
						crossModelAgreement = result.AgreementScore
						if result.Verdict != "agree" {
							reason := fmt.Sprintf("Cross-model disagreement detected (agreement: %.2f)", result.AgreementScore)
							riskFlag.Reasons = append(riskFlag.Reasons, reason)
							if riskFlag.RiskLevel == "low" {
								riskFlag.RiskLevel = "medium"
								riskFlag.ShouldWarn = true
							}
						}
					}
				}
			}

			if riskFlag.ShouldWarn {
				day := event.Timestamp.UTC().Format("2006-01-02")
				warnCountKey := fmt.Sprintf("warnings:daily:%s", day)
				if incErr := rdb.Incr(ctx, warnCountKey).Err(); incErr == nil {
					rdb.Expire(ctx, warnCountKey, dailyKeyTTL)
				}
			}
			if riskFlag.ShouldWarn {
				wi := warningItem{
					RequestID:         riskFlag.RequestID,
					SessionID:         riskFlag.SessionID,
					FeatureName:       event.FeatureName,
					RiskLevel:         riskFlag.RiskLevel,
					HallucinationRisk: riskFlag.HallucinationRisk,
					GroundingScore:    riskFlag.GroundingScore,
					Reasons:           riskFlag.Reasons,
					Timestamp:         event.Timestamp,
					RAGVerdict:        scorerOut.RAGVerdict,
					HedgeRisk:         scorerOut.HedgeRisk,
					DriftRisk:         scorerOut.DriftRisk,
					DriftVerdict:      scorerOut.DriftVerdict,
					InjectionRisk:     event.InjectionRisk,
					JailbreakRisk:     event.JailbreakRisk,
					ExfilRisk:         event.ExfilRisk,
					SecurityVerdict:   event.SecurityVerdict,
				}
				if wiJSON, marshalErr := json.Marshal(wi); marshalErr == nil {
					rdb.LPush(ctx, "warnings:list", string(wiJSON))
					rdb.LTrim(ctx, "warnings:list", 0, 99)
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

				if alertEmail := dbStore.AlertEmailFor(event.FeatureName); alertEmail != "" {
					if smtpCfg, err := dbStore.GetSMTPConfig(ctx); err == nil {
						go sendEmailAlert(
							smtpCfg,
							alertEmail,
							fmt.Sprintf("[Ajah Alert] Risk flag — feature: %s", event.FeatureName),
							fmt.Sprintf(
								"Feature: %s\nRisk Level: %s\nHallucination Risk: %.2f\nGrounding Score: %.2f\nReasons: %s\nTime: %s",
								event.FeatureName,
								riskFlag.RiskLevel,
								riskFlag.HallucinationRisk,
								riskFlag.GroundingScore,
								strings.Join(riskFlag.Reasons, ", "),
								event.Timestamp.UTC().Format("2006-01-02 15:04 UTC"),
							),
							logger,
						)
					}
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
			ResponseText: func() string {
				if len(event.Response) > 2000 {
					return event.Response[:2000]
				}
				return event.Response
			}(),
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
			CrossModelVerdict:     crossModelVerdict,
			CrossModelAgreement:   crossModelAgreement,
			InjectionRisk:         event.InjectionRisk,
			JailbreakRisk:         event.JailbreakRisk,
			ExfilRisk:             event.ExfilRisk,
			SecurityVerdict:       event.SecurityVerdict,
		}
		writeErr := writer.Write(ctx, record)

		// OpenTelemetry span export
		if cfg.OTelEndpoint != "" {
			_, span := tracer.Start(ctx,
				"ajah.llm.request",
				oteltrace.WithTimestamp(event.Timestamp),
			)
			span.SetAttributes(
				attribute.String("ajah.provider", event.Provider),
				attribute.String("ajah.model", event.Model),
				attribute.String("ajah.feature", event.FeatureName),
				attribute.String("ajah.user_id", event.UserID),
				attribute.String("ajah.session_id", event.SessionID),
				attribute.Int("ajah.status_code", event.StatusCode),
				attribute.Int64("ajah.latency_ms", event.LatencyMs),
				attribute.Float64("ajah.cost_usd", costRecord.CostUSD),
				attribute.Float64("ajah.hallucination_risk", riskFlag.HallucinationRisk),
				attribute.Float64("ajah.grounding_score", riskFlag.GroundingScore),
				attribute.String("ajah.risk_level", riskFlag.RiskLevel),
				attribute.Bool("ajah.was_pii_masked", maskResult.WasMasked),
				attribute.String("ajah.rag_verdict", scorerOut.RAGVerdict),
			)
			span.End(oteltrace.WithTimestamp(
				event.Timestamp.Add(time.Duration(event.LatencyMs) * time.Millisecond),
			))
		}

		// Prometheus instrumentation (unconditional — fires even on write error)
		metrics.RequestsTotal.WithLabelValues(
			event.Provider,
			event.Model,
			event.FeatureName,
			fmt.Sprintf("%d", event.StatusCode),
		).Inc()
		metrics.CostUSDTotal.WithLabelValues(
			event.FeatureName,
			event.Model,
		).Add(costRecord.CostUSD)
		metrics.LatencyMs.WithLabelValues(
			event.Provider,
			event.Model,
		).Observe(float64(event.LatencyMs))
		metrics.HallucinationRisk.WithLabelValues(
			event.FeatureName,
		).Set(riskFlag.HallucinationRisk)
		if maskResult.WasMasked {
			metrics.PIIDetectionsTotal.WithLabelValues(
				event.FeatureName,
			).Inc()
		}
		if riskFlag.ShouldWarn {
			metrics.WarningsTotal.WithLabelValues(
				riskFlag.RiskLevel,
			).Inc()
		}
		metrics.ClaimDensityRisk.WithLabelValues(
			event.FeatureName,
		).Set(scorerOut.ClaimDensityRisk)
		metrics.HedgeRisk.WithLabelValues(
			event.FeatureName,
		).Set(scorerOut.HedgeRisk)
		metrics.DriftRisk.WithLabelValues(
			event.FeatureName,
		).Set(scorerOut.DriftRisk)

		// Anomaly detection — z-score against per-feature 7-day baselines.
		if event.FeatureName != "" {
			anomalies := anomalyDetector.Detect(
				event.RequestID,
				event.FeatureName,
				riskFlag.HallucinationRisk,
				costRecord.CostUSD,
				event.LatencyMs,
				qualityScore,
			)
			for _, a := range anomalies {
				logger.Warn("anomaly detected",
					zap.String("type", string(a.Type)),
					zap.String("feature", a.FeatureName),
					zap.Float64("value", a.Value),
					zap.Float64("baseline", a.Baseline),
					zap.Float64("z_score", a.ZScore),
				)
				aJSON, _ := json.Marshal(map[string]interface{}{
					"request_id":   a.RequestID,
					"feature_name": a.FeatureName,
					"type":         string(a.Type),
					"value":        a.Value,
					"baseline":     a.Baseline,
					"z_score":      a.ZScore,
					"detected_at":  a.DetectedAt,
				})
				rdb.LPush(ctx, "anomalies:list", string(aJSON))
				rdb.LTrim(ctx, "anomalies:list", 0, 99)
			}
		}

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
								if webhookURL := dbStore.WebhookURLFor(event.FeatureName); webhookURL != "" {
									go fireCostWebhook(
										webhookURL,
										event.FeatureName,
										featureCost,
										threshold,
										event.Model,
										webhookClient,
										logger,
									)
								}

								if alertEmail := dbStore.AlertEmailFor(event.FeatureName); alertEmail != "" {
									if smtpCfg, err := dbStore.GetSMTPConfig(ctx); err == nil {
										go sendEmailAlert(
											smtpCfg,
											alertEmail,
											fmt.Sprintf("[Ajah Alert] Cost spike — feature: %s", event.FeatureName),
											fmt.Sprintf(
												"Feature: %s\nCost today: $%.4f\nThreshold: $%.2f\nModel: %s\nTime: %s",
												event.FeatureName,
												featureCost,
												threshold,
												event.Model,
												event.Timestamp.UTC().Format("2006-01-02 15:04 UTC"),
											),
											logger,
										)
									}
								}
							}
						}
					}
				}
			}
		}

		// Per-user budget alert
		if event.UserID != "" {
			if userSetting, ok := dbStore.UserSettingFor(event.UserID); ok &&
				userSetting.BudgetUSDPerDay > 0 {
				day := event.Timestamp.UTC().Format("2006-01-02")
				userCostKey := fmt.Sprintf("cost:user:%s:daily:%s", event.UserID, day)
				userCostStr, err := rdb.Get(ctx, userCostKey).Result()
				if err == nil {
					userCost, _ := strconv.ParseFloat(userCostStr, 64)
					if userCost > userSetting.BudgetUSDPerDay {
						firedKey := fmt.Sprintf("budget:fired:%s:%s", event.UserID, day)
						set, err := rdb.SetNX(ctx, firedKey, "1", 25*time.Hour).Result()
						if err == nil && set {
							logger.Warn("user budget exceeded",
								zap.String("user_id", event.UserID),
								zap.Float64("cost", userCost),
								zap.Float64("budget", userSetting.BudgetUSDPerDay),
							)
							if userSetting.WebhookURL != "" {
								payload := map[string]interface{}{
									"text": fmt.Sprintf(
										"💸 *User Budget Alert — Ajah*\n"+
											"*User:* %s\n"+
											"*Cost today:* $%.4f\n"+
											"*Budget:* $%.2f",
										event.UserID,
										userCost,
										userSetting.BudgetUSDPerDay,
									),
								}
								go sendWebhook(
									userSetting.WebhookURL,
									payload,
									webhookClient,
									logger,
									"budget:"+event.UserID,
								)
							}
							if userSetting.AlertEmailTo != "" {
								if smtpCfg, err := dbStore.GetSMTPConfig(ctx); err == nil {
									go sendEmailAlert(
										smtpCfg,
										userSetting.AlertEmailTo,
										fmt.Sprintf("[Ajah Alert] Budget exceeded — user: %s", event.UserID),
										fmt.Sprintf(
											"User: %s\nCost today: $%.4f\nBudget: $%.2f\nTime: %s",
											event.UserID,
											userCost,
											userSetting.BudgetUSDPerDay,
											event.Timestamp.UTC().Format("2006-01-02 15:04 UTC"),
										),
										logger,
									)
								}
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

	// OpenTelemetry trace export (disabled unless OTEL_EXPORTER_OTLP_ENDPOINT is set)
	otelShutdown, err := initOTel(emitterCtx, cfg.OTelEndpoint, logger)
	if err != nil {
		logger.Warn("otel init failed — export disabled", zap.Error(err))
		otelShutdown = func(context.Context) error { return nil }
	}
	defer otelShutdown(emitterCtx)

	// Background: refresh threshold cache every 60 s so settings changes
	// take effect without a restart.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				dbStore.RefreshCache(context.Background())
				_ = dbStore.RefreshUserCache(context.Background())
			case <-emitterCtx.Done():
				return
			}
		}
	}()

	// Background: refresh anomaly baselines every 5 min from ClickHouse.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := anomalyDetector.RefreshBaselines(context.Background()); err != nil {
					logger.Warn("anomaly baseline refresh failed", zap.Error(err))
				}
			case <-emitterCtx.Done():
				return
			}
		}
	}()

	// 10. Prometheus metrics registration -------------------------------------
	metrics.Register()

	// 10b. Security detector ---------------------------------------------------
	// SECURITY_BLOCK_ENABLED=true rejects flagged requests with 400.
	// Default is log-only (block threshold = 0 means never block).
	secBlockThresh := 0.0
	if os.Getenv("SECURITY_BLOCK_ENABLED") == "true" {
		secBlockThresh = 0.7
	}
	secDetector := security.New(secBlockThresh)

	// 10c. Proxy handler -------------------------------------------------------
	proxyHandler := proxy.New(cfg, emitter, logger, rdb, secDetector)

	// 11. Router ---------------------------------------------------------------
	r := chi.NewRouter()
	r.Use(corsMiddleware)

	r.With(rateLimitMiddleware(dbStore, rdb, logger)).Post("/v1/chat/completions", proxyHandler.ServeHTTP)
	r.Get("/health", healthHandler(rdb, dbStore, writer, logger))
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/metrics/cost", costMetricsHandler(rdb, logger))
	r.Get("/metrics/traces", tracesHandler(writer, logger))
	r.Get("/metrics/alerts", alertsHandler(rdb, logger))
	r.Get("/settings", getSettingsHandler(dbStore, logger))
	r.Post("/settings", postSettingsHandler(dbStore, logger))
	r.Get("/settings/users", getUserSettingsHandler(dbStore, logger))
	r.Post("/settings/users", postUserSettingsHandler(dbStore, logger))
	r.Get("/sessions", sessionsHandler(writer, logger))
	r.Get("/sessions/{sessionID}", sessionDetailHandler(writer, logger))
	r.Get("/warnings", warningsHandler(rdb, logger))
	r.Get("/warnings/{requestID}", warningByRequestIDHandler(rdb, logger))
	r.Get("/anomalies", anomaliesHandler(rdb, logger))
	r.Get("/export/traces", exportCSVHandler(writer, logger))
	r.Post("/v1/compare", compareHandler(cfg, logger))
	r.Get("/traces/{requestID}", traceByIDHandler(writer, logger))
	r.Post("/traces/{requestID}/replay", replayHandler(writer, cfg, logger))

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

func healthHandler(
	rdb *redis.Client,
	dbStore *db.Store,
	writer *storage.Writer,
	logger *zap.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		type depStatus struct {
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}

		type healthResponse struct {
			Status       string               `json:"status"`
			Version      string               `json:"version"`
			Dependencies map[string]depStatus `json:"dependencies"`
		}

		deps := map[string]depStatus{}
		overall := "ok"

		// Redis
		if err := rdb.Ping(ctx).Err(); err != nil {
			deps["redis"] = depStatus{
				Status: "down",
				Error:  err.Error(),
			}
			overall = "degraded"
		} else {
			deps["redis"] = depStatus{
				Status: "ok"}
		}

		// PostgreSQL
		if err := dbStore.Ping(ctx); err != nil {
			deps["postgres"] = depStatus{
				Status: "down",
				Error:  err.Error(),
			}
			overall = "degraded"
		} else {
			deps["postgres"] = depStatus{
				Status: "ok"}
		}

		// ClickHouse
		if err := writer.Ping(ctx); err != nil {
			deps["clickhouse"] = depStatus{
				Status: "down",
				Error:  err.Error(),
			}
			overall = "degraded"
		} else {
			deps["clickhouse"] = depStatus{
				Status: "ok"}
		}

		resp := healthResponse{
			Status:       overall,
			Version:      version,
			Dependencies: deps,
		}

		w.Header().Set("Content-Type", "application/json")
		if overall != "ok" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(resp)
	}
}

// initOTel sets up OpenTelemetry trace export to an OTLP/HTTP collector.
// If endpoint is empty, OTel export is disabled and a no-op shutdown is
// returned.
func initOTel(
	ctx context.Context,
	endpoint string,
	logger *zap.Logger,
) (func(context.Context) error, error) {
	if endpoint == "" {
		return func(context.Context) error {
			return nil
		}, nil
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("ajah-gateway"),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	logger.Info("otel export enabled", zap.String("endpoint", endpoint))

	return tp.Shutdown, nil
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
	RequestID      string                   `json:"request_id"`
	Prompt         string                   `json:"prompt"`
	Response       string                   `json:"response"`
	Model          string                   `json:"model"`
	FeatureName    string                   `json:"feature_name"`
	SourceContext  string                   `json:"source_context,omitempty"`
	SessionHistory []map[string]interface{} `json:"session_history,omitempty"`
}

type scorerOutcome struct {
	OverallQualityScore     float64  `json:"overall_quality_score"`
	HallucinationScore      float64  `json:"hallucination_score"`
	FactualConsistencyScore float64  `json:"factual_consistency_score"`
	ToxicityScore           float64  `json:"toxicity_score"`
	ClaimDensityRisk        float64  `json:"claim_density_risk"`
	HedgeRisk               float64  `json:"hedge_risk"`
	DriftRisk               float64  `json:"drift_risk"`
	DriftVerdict            string   `json:"drift_verdict"`
	Flags                   []string `json:"flags"`
	RAGVerdict              string   `json:"rag_verdict"`
	RAGGroundingScore       float64  `json:"rag_grounding_score"`
	RAGContradictionScore   float64  `json:"rag_contradiction_score"`
	RAGSupportedClaims      []string `json:"rag_supported_claims"`
	RAGUnsupportedClaims    []string `json:"rag_unsupported_claims"`
	RAGContradictedClaims   []string `json:"rag_contradicted_claims"`
}

// callScorer posts to the quality scorer service and returns the full scorerOutcome.
// Any failure (network, decode, etc.) logs a warning and returns a zero-value
// outcome so the main pipeline is never blocked by scorer availability.
func callScorer(ctx context.Context, client *http.Client, baseURL string, event events.RequestEvent, acc *sessions.Accumulator, logger *zap.Logger) scorerOutcome {
	scorerURL := baseURL + "/score"
	logger.Debug("scorer: calling", zap.String("url", scorerURL), zap.String("request_id", event.RequestID))

	var sessionTurns []map[string]interface{}
	if event.SessionID != "" {
		turns, err := acc.GetTurns(ctx, event.SessionID)
		if err == nil {
			for i, turn := range turns {
				if turn.ResponseText != "" {
					sessionTurns = append(sessionTurns, map[string]interface{}{
						"turn_index":    i,
						"response_text": turn.ResponseText,
					})
				}
			}
		}
	}

	body, err := json.Marshal(scorerPayload{
		RequestID:      event.RequestID,
		Prompt:         event.Prompt,
		Response:       event.Response,
		Model:          event.Model,
		FeatureName:    event.FeatureName,
		SourceContext:  event.SourceContext,
		SessionHistory: sessionTurns,
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

// sendWebhook marshals payload and POSTs it to webhookURL with exponential
// backoff retry (4 attempts: 500 ms / 1 s / 2 s / 4 s). 4xx responses are
// treated as permanent failures and not retried.
func sendWebhook(
	webhookURL string,
	payload map[string]interface{},
	client *http.Client,
	logger *zap.Logger,
	label string,
) {
	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("webhook marshal failed",
			zap.String("label", label),
			zap.Error(err))
		return
	}

	maxAttempts := 4
	baseDelay := 500 * time.Millisecond

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			logger.Info("webhook retry backoff",
				zap.String("label", label),
				zap.Int("attempt", attempt+1),
				zap.Duration("delay", delay))
			time.Sleep(delay)
		}

		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodPost,
			webhookURL,
			bytes.NewReader(body),
		)
		if err != nil {
			logger.Error("webhook build request failed",
				zap.String("label", label),
				zap.Error(err))
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			logger.Warn("webhook attempt failed",
				zap.String("label", label),
				zap.Int("attempt", attempt+1),
				zap.Error(err))
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Info("webhook delivered",
				zap.String("label", label),
				zap.Int("attempt", attempt+1),
				zap.Int("status", resp.StatusCode))
			return
		}

		logger.Warn("webhook non-2xx",
			zap.String("label", label),
			zap.Int("attempt", attempt+1),
			zap.Int("status", resp.StatusCode))

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			logger.Error("webhook 4xx — not retrying",
				zap.String("label", label),
				zap.Int("status", resp.StatusCode))
			return
		}
	}

	logger.Error("webhook failed after all attempts",
		zap.String("label", label),
		zap.Int("max_attempts", maxAttempts))
}

func fireCostWebhook(
	webhookURL string,
	featureName string,
	costUSD float64,
	threshold float64,
	model string,
	client *http.Client,
	logger *zap.Logger,
) {
	payload := map[string]interface{}{
		"text": fmt.Sprintf(
			"🚨 *Cost Alert — Ajah*\n"+
				"*Feature:* %s\n"+
				"*Cost today:* $%.4f\n"+
				"*Threshold:* $%.2f\n"+
				"*Model:* %s",
			featureName,
			costUSD,
			threshold,
			model,
		),
	}
	sendWebhook(webhookURL, payload, client, logger, "cost:"+featureName)
}

// sendEmailAlert sends a plain-text email alert via SMTP. Fire-and-forget —
// always called in a goroutine. No-op if SMTP is not configured or the
// recipient address is empty.
func sendEmailAlert(
	smtpCfg db.SMTPConfig,
	to string,
	subject string,
	body string,
	logger *zap.Logger,
) {
	if smtpCfg.Host == "" || to == "" {
		return
	}
	addr := fmt.Sprintf("%s:%d", smtpCfg.Host, smtpCfg.Port)
	auth := smtp.PlainAuth("", smtpCfg.Username, smtpCfg.Password, smtpCfg.Host)
	msg := []byte(
		"To: " + to + "\r\n" +
			"From: " + smtpCfg.From + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"\r\n" +
			body,
	)
	err := smtp.SendMail(addr, auth, smtpCfg.From, []string{to}, msg)
	if err != nil {
		logger.Error("email alert failed",
			zap.String("to", to),
			zap.Error(err),
		)
		return
	}
	logger.Info("email alert sent",
		zap.String("to", to),
		zap.String("subject", subject),
	)
}

func fireWebhook(
	webhookURL string,
	flag flagging.RiskFlag,
	client *http.Client,
	logger *zap.Logger,
) {
	payload := map[string]interface{}{
		"text": fmt.Sprintf(
			"⚠️  *Risk Alert — Ajah*\n"+
				"*Feature:* %s\n"+
				"*Risk Level:* %s\n"+
				"*Hallucination Risk:* %.2f\n"+
				"*Grounding Score:* %.2f\n"+
				"*Reasons:* %s",
			flag.RequestID,
			flag.RiskLevel,
			flag.HallucinationRisk,
			flag.GroundingScore,
			strings.Join(flag.Reasons, ", "),
		),
	}
	sendWebhook(webhookURL, payload, client, logger, "risk:"+flag.RequestID)
}
