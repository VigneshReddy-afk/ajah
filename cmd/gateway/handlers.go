package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/db"
	"github.com/ajah/core/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// corsMiddleware adds permissive CORS headers so the dashboard (served from
// localhost:3000) can call the gateway (localhost:8080) directly from the browser.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware enforces a per-feature requests-per-minute limit using
// a Redis counter keyed by feature name and the current minute (a fixed
// window). Requests without X-Feature-Name, or for features with no limit
// configured, pass through unaffected.
func rateLimitMiddleware(
	store *db.Store,
	rdb *redis.Client,
	logger *zap.Logger,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			featureName := r.Header.Get("X-Feature-Name")
			if featureName == "" {
				next.ServeHTTP(w, r)
				return
			}
			limit := store.RateLimitRPMFor(featureName)
			if limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			now := time.Now()
			minute := now.Format("200601021504")
			key := "ratelimit:" + featureName + ":" + minute
			count, err := rdb.Incr(r.Context(), key).Result()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if count == 1 {
				rdb.Expire(r.Context(), key, 70*time.Second)
			}
			if count > int64(limit) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", 60-now.Second()))
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":            "rate limit exceeded",
					"feature":          featureName,
					"limit":            limit,
					"reset_in_seconds": 60 - now.Second(),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// traceResponse is the JSON shape returned by GET /metrics/traces.
type traceResponse struct {
	TraceID             string    `json:"trace_id"`
	RequestID           string    `json:"request_id"`
	UserID              string    `json:"user_id"`
	SessionID           string    `json:"session_id"`
	FeatureName         string    `json:"feature_name"`
	AgentStep           string    `json:"agent_step"`
	ParentStepID        string    `json:"parent_step_id"`
	StepName            string    `json:"step_name"`
	ToolName            string    `json:"tool_name"`
	Provider            string    `json:"provider"`
	Model               string    `json:"model"`
	InputTokens         int       `json:"input_tokens"`
	OutputTokens        int       `json:"output_tokens"`
	CostUSD             float64   `json:"cost_usd"`
	LatencyMs           int64     `json:"latency_ms"`
	StatusCode          int       `json:"status_code"`
	MaskedPrompt        string    `json:"masked_prompt"`
	WasPIIMasked        bool      `json:"was_pii_masked"`
	QualityScore        float64   `json:"quality_score"`
	Timestamp           time.Time `json:"timestamp"`
	HallucinationRisk   float64   `json:"hallucination_risk"`
	GroundingScore      float64   `json:"grounding_score"`
	RiskLevel           string    `json:"risk_level"`
	ShouldWarn          bool      `json:"should_warn"`
	CrossModelVerdict   string    `json:"cross_model_verdict"`
	CrossModelAgreement float64   `json:"cross_model_agreement"`
}

// warningItem is the JSON shape of a single flagged response stored in Redis
// and returned by GET /warnings.
type warningItem struct {
	RequestID         string    `json:"request_id"`
	SessionID         string    `json:"session_id"`
	FeatureName       string    `json:"feature_name"`
	RiskLevel         string    `json:"risk_level"`
	HallucinationRisk float64   `json:"hallucination_risk"`
	GroundingScore    float64   `json:"grounding_score"`
	Reasons           []string  `json:"reasons"`
	Timestamp         time.Time `json:"timestamp"`
	RAGVerdict        string    `json:"rag_verdict"`
	HedgeRisk         float64   `json:"hedge_risk"`
	DriftRisk         float64   `json:"drift_risk"`
	DriftVerdict      string    `json:"drift_verdict"`
	InjectionRisk     float64   `json:"injection_risk"`
	JailbreakRisk     float64   `json:"jailbreak_risk"`
	ExfilRisk         float64   `json:"exfil_risk"`
	SecurityVerdict   string    `json:"security_verdict"`
}

// tracesHandler returns the 100 most recent traces from ClickHouse.
func tracesHandler(writer *storage.Writer, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		records, err := writer.QueryRecent(ctx, 100)
		if err != nil {
			logger.Error("query recent traces", zap.Error(err))
			http.Error(w, "failed to query traces", http.StatusInternalServerError)
			return
		}

		resp := make([]traceResponse, len(records))
		for i, rec := range records {
			resp[i] = traceResponse{
				TraceID:             rec.TraceID,
				RequestID:           rec.RequestID,
				UserID:              rec.UserID,
				SessionID:           rec.SessionID,
				FeatureName:         rec.FeatureName,
				AgentStep:           rec.AgentStep,
				ParentStepID:        rec.ParentStepID,
				StepName:            rec.StepName,
				ToolName:            rec.ToolName,
				Provider:            rec.Provider,
				Model:               rec.Model,
				InputTokens:         rec.InputTokens,
				OutputTokens:        rec.OutputTokens,
				CostUSD:             rec.CostUSD,
				LatencyMs:           rec.LatencyMs,
				StatusCode:          rec.StatusCode,
				MaskedPrompt:        rec.MaskedPrompt,
				WasPIIMasked:        rec.WasPIIMasked,
				QualityScore:        rec.QualityScore,
				Timestamp:           rec.Timestamp,
				HallucinationRisk:   rec.HallucinationRisk,
				GroundingScore:      rec.GroundingScore,
				RiskLevel:           rec.RiskLevel,
				ShouldWarn:          rec.ShouldWarn,
				CrossModelVerdict:   rec.CrossModelVerdict,
				CrossModelAgreement: rec.CrossModelAgreement,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// traceByIDHandler fetches a single trace from ClickHouse by request ID.
func traceByIDHandler(writer *storage.Writer, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := chi.URLParam(r, "requestID")
		if requestID == "" {
			http.Error(w, "missing requestID", 400)
			return
		}

		trace, err := writer.QueryByRequestID(r.Context(), requestID)
		if err != nil {
			logger.Error("trace by id query failed",
				zap.String("request_id", requestID),
				zap.Error(err))
			http.Error(w, "query failed", 500)
			return
		}
		if trace == nil {
			http.Error(w, "trace not found", 404)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(trace)
	}
}

// replayHandler re-sends the stored prompt for a trace to the original provider
// and returns a side-by-side comparison of the original and replay outcomes.
func replayHandler(writer *storage.Writer, cfg *config.Config, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := chi.URLParam(r, "requestID")
		if requestID == "" {
			http.Error(w, "missing requestID", 400)
			return
		}

		trace, err := writer.QueryByRequestID(r.Context(), requestID)
		if err != nil || trace == nil {
			http.Error(w, "trace not found", 404)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", 401)
			return
		}

		payload := map[string]interface{}{
			"model": trace.Model,
			"messages": []map[string]interface{}{
				{
					"role":    "user",
					"content": trace.MaskedPrompt,
				},
			},
		}
		payloadBytes, _ := json.Marshal(payload)

		key := strings.TrimPrefix(authHeader, "Bearer ")
		providerURL := "https://api.groq.com/openai/v1"
		if strings.HasPrefix(key, "sk-ant-") {
			providerURL = "https://api.anthropic.com"
		} else if strings.HasPrefix(key, "sk-") {
			providerURL = "https://api.openai.com"
		} else if strings.HasPrefix(key, "AIza") {
			providerURL = "https://generativelanguage.googleapis.com/v1beta/openai"
		}

		upstreamURL := providerURL + "/v1/chat/completions"

		start := time.Now()
		upstreamReq, err := http.NewRequestWithContext(
			r.Context(),
			http.MethodPost,
			upstreamURL,
			bytes.NewReader(payloadBytes),
		)
		if err != nil {
			http.Error(w, "request build failed", 500)
			return
		}
		upstreamReq.Header.Set("Content-Type", "application/json")
		upstreamReq.Header.Set("Authorization", authHeader)

		client := &http.Client{
			Timeout: time.Duration(cfg.ProviderTimeoutSeconds) * time.Second,
		}

		resp, err := client.Do(upstreamReq)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			http.Error(w, "upstream failed", 502)
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(respBody, &parsed)

		replayResponse := ""
		if len(parsed.Choices) > 0 {
			replayResponse = parsed.Choices[0].Message.Content
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"original": map[string]interface{}{
				"request_id":         trace.RequestID,
				"model":              trace.Model,
				"prompt":             trace.MaskedPrompt,
				"latency_ms":         trace.LatencyMs,
				"cost_usd":           trace.CostUSD,
				"quality_score":      trace.QualityScore,
				"hallucination_risk": trace.HallucinationRisk,
				"timestamp":          trace.Timestamp,
			},
			"replay": map[string]interface{}{
				"model":         trace.Model,
				"response":      replayResponse,
				"input_tokens":  parsed.Usage.PromptTokens,
				"output_tokens": parsed.Usage.CompletionTokens,
				"latency_ms":    latencyMs,
				"replayed_at":   time.Now().UTC(),
			},
		})
	}
}

// exportCSVHandler streams up to 1000 recent traces as a downloadable CSV.
func exportCSVHandler(writer *storage.Writer, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		traces, err := writer.QueryRecent(ctx, 1000)
		if err != nil {
			logger.Error("csv export query failed", zap.Error(err))
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="ajah-audit-log.csv"`)

		cw := csv.NewWriter(w)

		_ = cw.Write([]string{
			"timestamp",
			"trace_id",
			"request_id",
			"user_id",
			"session_id",
			"feature_name",
			"provider",
			"model",
			"input_tokens",
			"output_tokens",
			"cost_usd",
			"latency_ms",
			"status_code",
			"was_pii_masked",
			"quality_score",
			"hallucination_risk",
			"grounding_score",
			"risk_level",
			"should_warn",
			"rag_verdict",
			"rag_grounding_score",
			"rag_contradiction_score",
			"cross_model_verdict",
			"cross_model_agreement",
			"agent_step",
			"step_name",
			"tool_name",
		})

		for _, t := range traces {
			_ = cw.Write([]string{
				t.Timestamp.UTC().Format(time.RFC3339),
				t.TraceID,
				t.RequestID,
				t.UserID,
				t.SessionID,
				t.FeatureName,
				t.Provider,
				t.Model,
				strconv.Itoa(t.InputTokens),
				strconv.Itoa(t.OutputTokens),
				strconv.FormatFloat(t.CostUSD, 'f', 8, 64),
				strconv.FormatInt(t.LatencyMs, 10),
				strconv.Itoa(t.StatusCode),
				strconv.FormatBool(t.WasPIIMasked),
				strconv.FormatFloat(t.QualityScore, 'f', 4, 64),
				strconv.FormatFloat(t.HallucinationRisk, 'f', 4, 64),
				strconv.FormatFloat(t.GroundingScore, 'f', 4, 64),
				t.RiskLevel,
				strconv.FormatBool(t.ShouldWarn),
				t.RAGVerdict,
				strconv.FormatFloat(t.RAGGroundingScore, 'f', 4, 64),
				strconv.FormatFloat(t.RAGContradictionScore, 'f', 4, 64),
				t.CrossModelVerdict,
				strconv.FormatFloat(t.CrossModelAgreement, 'f', 4, 64),
				t.AgentStep,
				t.StepName,
				t.ToolName,
			})
		}

		cw.Flush()
		if err := cw.Error(); err != nil {
			logger.Error("csv flush failed", zap.Error(err))
		}
	}
}

// alertResponse is the JSON shape of a single cost-spike alert.
type alertResponse struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Feature   string    `json:"feature"`
	Threshold float64   `json:"threshold"`
	Actual    float64   `json:"actual"`
}

// alertsHandler returns the last 100 cost-spike alerts stored in Redis.
func alertsHandler(rdb *redis.Client, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		items, err := rdb.LRange(ctx, "alerts:list", 0, 99).Result()
		if err != nil {
			logger.Error("read alerts list", zap.Error(err))
			http.Error(w, "failed to read alerts", http.StatusInternalServerError)
			return
		}

		alerts := make([]alertResponse, 0, len(items))
		for _, item := range items {
			var a alertResponse
			if err := json.Unmarshal([]byte(item), &a); err != nil {
				logger.Warn("malformed alert in Redis", zap.String("item", item), zap.Error(err))
				continue
			}
			alerts = append(alerts, a)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(alerts)
	}
}

// settingsPayload is used by both GET and POST /settings.
type settingsPayload struct {
	FeatureSettings []db.FeatureSetting `json:"feature_settings"`
	ProviderKeys    []db.ProviderKey    `json:"provider_keys"`
	SMTPConfig      db.SMTPConfig       `json:"smtp_config"`
}

// getSettingsHandler reads feature settings and provider keys from PostgreSQL.
func getSettingsHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		features, err := store.GetSettings(ctx)
		if err != nil {
			logger.Error("get settings", zap.Error(err))
			http.Error(w, "failed to read settings", http.StatusInternalServerError)
			return
		}
		if features == nil {
			features = []db.FeatureSetting{}
		}

		keys, err := store.GetProviderKeys(ctx)
		if err != nil {
			logger.Error("get provider keys", zap.Error(err))
			http.Error(w, "failed to read provider keys", http.StatusInternalServerError)
			return
		}
		if keys == nil {
			keys = []db.ProviderKey{}
		}

		smtpCfg, err := store.GetSMTPConfig(ctx)
		if err != nil {
			smtpCfg = db.SMTPConfig{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(settingsPayload{
			FeatureSettings: features,
			ProviderKeys:    keys,
			SMTPConfig:      smtpCfg,
		})
	}
}

// sessionResponse is the JSON shape of a single session in the list endpoint.
type sessionResponse struct {
	SessionID   string    `json:"session_id"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	TotalCost   float64   `json:"total_cost"`
	TotalTokens int64     `json:"total_tokens"`
	StepCount   int32     `json:"step_count"`
	AvgQuality  float64   `json:"avg_quality"`
	Status      string    `json:"status"`
	FeatureName string    `json:"feature_name"`
	UserID      string    `json:"user_id"`
}

// sessionDetailResponse adds the individual traces to a session.
type sessionDetailResponse struct {
	sessionResponse
	Traces []traceResponse `json:"traces"`
}

// sessionsHandler returns the 50 most recent closed sessions from ClickHouse.
func sessionsHandler(writer *storage.Writer, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		records, err := writer.QueryRecentSessions(ctx, 50)
		if err != nil {
			logger.Error("query recent sessions", zap.Error(err))
			http.Error(w, "failed to query sessions", http.StatusInternalServerError)
			return
		}

		resp := make([]sessionResponse, len(records))
		for i, rec := range records {
			resp[i] = sessionResponse{
				SessionID:   rec.SessionID,
				StartTime:   rec.StartTime,
				EndTime:     rec.LastSeen,
				TotalCost:   rec.TotalCost,
				TotalTokens: rec.TotalTokens,
				StepCount:   rec.StepCount,
				AvgQuality:  rec.AvgQuality,
				Status:      rec.Status,
				FeatureName: rec.FeatureName,
				UserID:      rec.UserID,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"sessions": resp})
	}
}

// sessionDetailHandler returns a single session with all its traces.
func sessionDetailHandler(writer *storage.Writer, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sessionID := chi.URLParam(r, "sessionID")

		rec, err := writer.QuerySessionByID(ctx, sessionID)
		if err != nil {
			logger.Error("query session", zap.String("session_id", sessionID), zap.Error(err))
			http.Error(w, "failed to query session", http.StatusInternalServerError)
			return
		}
		if rec == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		traceRecs, err := writer.QueryTracesBySession(ctx, sessionID)
		if err != nil {
			logger.Error("query session traces", zap.String("session_id", sessionID), zap.Error(err))
			http.Error(w, "failed to query session traces", http.StatusInternalServerError)
			return
		}

		traces := make([]traceResponse, len(traceRecs))
		for i, t := range traceRecs {
			traces[i] = traceResponse{
				TraceID:             t.TraceID,
				RequestID:           t.RequestID,
				UserID:              t.UserID,
				SessionID:           t.SessionID,
				FeatureName:         t.FeatureName,
				AgentStep:           t.AgentStep,
				ParentStepID:        t.ParentStepID,
				StepName:            t.StepName,
				ToolName:            t.ToolName,
				Provider:            t.Provider,
				Model:               t.Model,
				InputTokens:         t.InputTokens,
				OutputTokens:        t.OutputTokens,
				CostUSD:             t.CostUSD,
				LatencyMs:           t.LatencyMs,
				StatusCode:          t.StatusCode,
				MaskedPrompt:        t.MaskedPrompt,
				WasPIIMasked:        t.WasPIIMasked,
				QualityScore:        t.QualityScore,
				Timestamp:           t.Timestamp,
				HallucinationRisk:   t.HallucinationRisk,
				GroundingScore:      t.GroundingScore,
				RiskLevel:           t.RiskLevel,
				ShouldWarn:          t.ShouldWarn,
				CrossModelVerdict:   t.CrossModelVerdict,
				CrossModelAgreement: t.CrossModelAgreement,
			}
		}

		detail := sessionDetailResponse{
			sessionResponse: sessionResponse{
				SessionID:   rec.SessionID,
				StartTime:   rec.StartTime,
				EndTime:     rec.LastSeen,
				TotalCost:   rec.TotalCost,
				TotalTokens: rec.TotalTokens,
				StepCount:   rec.StepCount,
				AvgQuality:  rec.AvgQuality,
				Status:      rec.Status,
				FeatureName: rec.FeatureName,
				UserID:      rec.UserID,
			},
			Traces: traces,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(detail)
	}
}

// flagResult is the JSON shape returned by GET /warnings/{requestID}.
type flagResult struct {
	RequestID string   `json:"request_id"`
	Flagged   bool     `json:"flagged"`
	RiskLevel string   `json:"risk_level"`
	Reasons   []string `json:"reasons"`
}

// warningByRequestIDHandler looks up a per-request risk flag from Redis and
// returns it as JSON. Returns 404 if the flag has not been recorded yet or
// has expired (TTL 1 h).
func warningByRequestIDHandler(rdb *redis.Client, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := chi.URLParam(r, "requestID")
		raw, err := rdb.Get(ctx, "flag:"+requestID).Bytes()
		if err == redis.Nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			logger.Error("read flag", zap.String("request_id", requestID), zap.Error(err))
			http.Error(w, "failed to read flag", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// warningsHandler returns the last 100 high-risk responses from Redis and the
// total warning count for today.
func warningsHandler(rdb *redis.Client, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		today := time.Now().UTC().Format("2006-01-02")

		items, err := rdb.LRange(ctx, "warnings:list", 0, 99).Result()
		if err != nil {
			logger.Error("read warnings list", zap.Error(err))
			http.Error(w, "failed to read warnings", http.StatusInternalServerError)
			return
		}

		warnings := make([]warningItem, 0, len(items))
		for _, item := range items {
			var wi warningItem
			if err := json.Unmarshal([]byte(item), &wi); err != nil {
				logger.Warn("malformed warning in Redis", zap.String("item", item), zap.Error(err))
				continue
			}
			warnings = append(warnings, wi)
		}

		totalToday := readCounter(ctx, rdb, logger, fmt.Sprintf("warnings:daily:%s", today))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"warnings":    warnings,
			"total_today": totalToday,
		})
	}
}

// anomaliesHandler returns the last 100 statistical anomalies from Redis.
func anomaliesHandler(rdb *redis.Client, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		data, err := rdb.LRange(ctx, "anomalies:list", 0, 99).Result()
		if err != nil {
			logger.Error("read anomalies list", zap.Error(err))
			http.Error(w, "redis error", http.StatusInternalServerError)
			return
		}
		anomalies := make([]json.RawMessage, 0, len(data))
		for _, d := range data {
			anomalies = append(anomalies, json.RawMessage(d))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"anomalies": anomalies,
			"total":     len(anomalies),
		})
	}
}

// postSettingsHandler persists feature settings and provider keys to PostgreSQL,
// then refreshes the in-memory threshold cache.
func postSettingsHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var payload settingsPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		for _, f := range payload.FeatureSettings {
			if err := store.UpsertSetting(ctx, f); err != nil {
				logger.Error("upsert setting", zap.String("feature", f.FeatureName), zap.Error(err))
				http.Error(w, "failed to save settings", http.StatusInternalServerError)
				return
			}
		}

		for _, k := range payload.ProviderKeys {
			if k.APIKey == "" {
				continue
			}
			if err := store.UpsertProviderKey(ctx, k); err != nil {
				logger.Error("upsert provider key", zap.String("provider", k.Provider), zap.Error(err))
				http.Error(w, "failed to save provider key", http.StatusInternalServerError)
				return
			}
		}

		if payload.SMTPConfig.Host != "" {
			if err := store.UpsertSMTPConfig(ctx, payload.SMTPConfig); err != nil {
				logger.Error("upsert smtp config", zap.Error(err))
				http.Error(w, "failed to save smtp config", http.StatusInternalServerError)
				return
			}
		}

		store.RefreshCache(ctx)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func getUserSettingsHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := store.GetUserSettings(r.Context())
		if err != nil {
			logger.Error("get user settings failed", zap.Error(err))
			http.Error(w, "db error", 500)
			return
		}
		if settings == nil {
			settings = []db.UserSetting{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"user_settings": settings,
		})
	}
}

func postUserSettingsHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			UserSettings []db.UserSetting `json:"user_settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		for _, u := range payload.UserSettings {
			if u.UserID == "" {
				continue
			}
			if err := store.UpsertUserSetting(r.Context(), u); err != nil {
				logger.Error("upsert user setting failed",
					zap.String("user_id", u.UserID),
					zap.Error(err))
				http.Error(w, "db error", 500)
				return
			}
		}
		_ = store.RefreshUserCache(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

func compareHandler(
	cfg *config.Config,
	logger *zap.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type CompareRequest struct {
			Models   []string                 `json:"models"`
			Messages []map[string]interface{} `json:"messages"`
		}

		type ModelResult struct {
			Model        string  `json:"model"`
			Response     string  `json:"response"`
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			CostUSD      float64 `json:"cost_usd"`
			LatencyMs    int64   `json:"latency_ms"`
			StatusCode   int     `json:"status_code"`
			Error        string  `json:"error,omitempty"`
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read error", 400)
			return
		}

		var req CompareRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}

		if len(req.Models) < 2 {
			http.Error(w, "provide at least 2 models", 400)
			return
		}
		if len(req.Models) > 4 {
			http.Error(w, "maximum 4 models", 400)
			return
		}

		authHeader := r.Header.Get("Authorization")

		results := make([]ModelResult, len(req.Models))
		var wg sync.WaitGroup

		for i, model := range req.Models {
			wg.Add(1)
			go func(idx int, modelName string) {
				defer wg.Done()
				start := time.Now()

				payload := map[string]interface{}{
					"model":    modelName,
					"messages": req.Messages,
				}
				payloadBytes, _ := json.Marshal(payload)

				key := strings.TrimPrefix(authHeader, "Bearer ")
				providerURL := "https://api.groq.com/openai/v1"
				if strings.HasPrefix(key, "sk-ant-") {
					providerURL = "https://api.anthropic.com"
				} else if strings.HasPrefix(key, "sk-") {
					providerURL = "https://api.openai.com"
				} else if strings.HasPrefix(key, "AIza") {
					providerURL = "https://generativelanguage.googleapis.com/v1beta/openai"
				} else if strings.HasPrefix(key, "xai-") {
					providerURL = "https://api.x.ai"
				} else if strings.HasPrefix(key, "mistral-") {
					providerURL = "https://api.mistral.ai"
				}

				upstreamURL := providerURL + "/v1/chat/completions"

				upstreamReq, err := http.NewRequestWithContext(
					r.Context(),
					http.MethodPost,
					upstreamURL,
					bytes.NewReader(payloadBytes),
				)
				if err != nil {
					results[idx] = ModelResult{Model: modelName, Error: err.Error()}
					return
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", authHeader)

				client := &http.Client{
					Timeout: time.Duration(cfg.ProviderTimeoutSeconds) * time.Second,
				}

				resp, err := client.Do(upstreamReq)
				latency := time.Since(start).Milliseconds()

				if err != nil {
					results[idx] = ModelResult{Model: modelName, LatencyMs: latency, Error: err.Error()}
					return
				}
				defer resp.Body.Close()

				respBody, _ := io.ReadAll(resp.Body)

				var parsed struct {
					Choices []struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					} `json:"choices"`
					Usage struct {
						PromptTokens     int `json:"prompt_tokens"`
						CompletionTokens int `json:"completion_tokens"`
					} `json:"usage"`
				}
				_ = json.Unmarshal(respBody, &parsed)

				responseText := ""
				if len(parsed.Choices) > 0 {
					responseText = parsed.Choices[0].Message.Content
				}

				results[idx] = ModelResult{
					Model:        modelName,
					Response:     responseText,
					InputTokens:  parsed.Usage.PromptTokens,
					OutputTokens: parsed.Usage.CompletionTokens,
					LatencyMs:    latency,
					StatusCode:   resp.StatusCode,
				}
			}(i, model)
		}

		wg.Wait()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results":     results,
			"models":      req.Models,
			"compared_at": time.Now().UTC(),
		})
	}
}

// ── Eval handlers ─────────────────────────────────────────────────────────────

// evalID generates a short hex ID from the current nanosecond timestamp.
func evalID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

// listEvalSuitesHandler — GET /evals
func listEvalSuitesHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		suites, err := store.ListEvalSuites(r.Context())
		if err != nil {
			logger.Error("list eval suites", zap.Error(err))
			http.Error(w, "failed to list suites", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"suites": suites})
	}
}

// createEvalSuiteHandler — POST /evals
func createEvalSuiteHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name          string  `json:"name"`
			Description   string  `json:"description"`
			PassThreshold float64 `json:"pass_threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if req.PassThreshold == 0 {
			req.PassThreshold = 0.7
		}
		suite := db.EvalSuite{
			ID:            evalID(),
			Name:          req.Name,
			Description:   req.Description,
			PassThreshold: req.PassThreshold,
		}
		if err := store.CreateEvalSuite(r.Context(), suite); err != nil {
			logger.Error("create eval suite", zap.Error(err))
			http.Error(w, "failed to create suite", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(suite)
	}
}

// deleteEvalSuiteHandler — DELETE /evals/{suiteID}
func deleteEvalSuiteHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "suiteID")
		if err := store.DeleteEvalSuite(r.Context(), id); err != nil {
			logger.Error("delete eval suite", zap.Error(err))
			http.Error(w, "failed to delete suite", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// listEvalCasesHandler — GET /evals/{suiteID}/cases
func listEvalCasesHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		suiteID := chi.URLParam(r, "suiteID")
		cases, err := store.ListEvalCases(r.Context(), suiteID)
		if err != nil {
			logger.Error("list eval cases", zap.Error(err))
			http.Error(w, "failed to list cases", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"cases": cases})
	}
}

// createEvalCaseHandler — POST /evals/{suiteID}/cases
func createEvalCaseHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		suiteID := chi.URLParam(r, "suiteID")
		var req struct {
			Prompt         string `json:"prompt"`
			ExpectedOutput string `json:"expected_output"`
			Tags           string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Prompt == "" {
			http.Error(w, "prompt is required", http.StatusBadRequest)
			return
		}
		c := db.EvalCase{
			ID:             evalID(),
			SuiteID:        suiteID,
			Prompt:         req.Prompt,
			ExpectedOutput: req.ExpectedOutput,
			Tags:           req.Tags,
		}
		if err := store.CreateEvalCase(r.Context(), c); err != nil {
			logger.Error("create eval case", zap.Error(err))
			http.Error(w, "failed to create case", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(c)
	}
}

// deleteEvalCaseHandler — DELETE /evals/{suiteID}/cases/{caseID}
func deleteEvalCaseHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "caseID")
		if err := store.DeleteEvalCase(r.Context(), id); err != nil {
			logger.Error("delete eval case", zap.Error(err))
			http.Error(w, "failed to delete case", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// runEvalSuiteHandler — POST /evals/{suiteID}/run
// Fetches all cases, sends each prompt through the LLM API key configured in
// the request body, scores via the scorer, and persists results.
func runEvalSuiteHandler(store *db.Store, scorerURL string, logger *zap.Logger) http.HandlerFunc {
	type runReq struct {
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
		BaseURL string `json:"base_url"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		suiteID := chi.URLParam(r, "suiteID")

		var req runReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.APIKey == "" || req.Model == "" {
			http.Error(w, "api_key and model are required", http.StatusBadRequest)
			return
		}
		if req.BaseURL == "" {
			req.BaseURL = "https://api.openai.com/v1"
		}

		suite, err := store.GetEvalSuite(r.Context(), suiteID)
		if err != nil || suite == nil {
			http.Error(w, "suite not found", http.StatusNotFound)
			return
		}

		cases, err := store.ListEvalCases(r.Context(), suiteID)
		if err != nil || len(cases) == 0 {
			http.Error(w, "no cases in suite", http.StatusBadRequest)
			return
		}

		runID := evalID()
		run := db.EvalRun{
			ID:      runID,
			SuiteID: suiteID,
			Model:   req.Model,
		}
		if err := store.CreateEvalRun(r.Context(), run); err != nil {
			logger.Error("create eval run", zap.Error(err))
			http.Error(w, "failed to create run", http.StatusInternalServerError)
			return
		}

		client := &http.Client{Timeout: 60 * time.Second}
		scorerClient := &http.Client{Timeout: 45 * time.Second}

		var (
			mu        sync.Mutex
			passCount int
			failCount int
			totalQ    float64
			totalLat  float64
			totalCost float64
			results   []db.EvalResult
		)

		var wg sync.WaitGroup
		sem := make(chan struct{}, 3) // max 3 concurrent LLM calls

		for _, c := range cases {
			wg.Add(1)
			go func(ec db.EvalCase) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				start := time.Now()

				// Call LLM
				payload, _ := json.Marshal(map[string]interface{}{
					"model": req.Model,
					"messages": []map[string]string{
						{"role": "user", "content": ec.Prompt},
					},
					"max_tokens": 1024,
				})
				llmReq, _ := http.NewRequestWithContext(r.Context(), "POST",
					req.BaseURL+"/chat/completions", bytes.NewReader(payload))
				llmReq.Header.Set("Authorization", "Bearer "+req.APIKey)
				llmReq.Header.Set("Content-Type", "application/json")

				latencyMs := 0
				actualOutput := ""
				costUSD := 0.0

				llmResp, llmErr := client.Do(llmReq)
				if llmErr == nil {
					latencyMs = int(time.Since(start).Milliseconds())
					defer llmResp.Body.Close()
					body, _ := io.ReadAll(llmResp.Body)
					var parsed struct {
						Choices []struct {
							Message struct {
								Content string `json:"content"`
							} `json:"message"`
						} `json:"choices"`
						Usage struct {
							PromptTokens     int `json:"prompt_tokens"`
							CompletionTokens int `json:"completion_tokens"`
						} `json:"usage"`
					}
					if json.Unmarshal(body, &parsed) == nil && len(parsed.Choices) > 0 {
						actualOutput = parsed.Choices[0].Message.Content
						// rough cost estimate at gpt-4o rates
						costUSD = float64(parsed.Usage.PromptTokens)*0.0000025 +
							float64(parsed.Usage.CompletionTokens)*0.00001
					}
				}

				// Score via scorer
				qualityScore := 0.0
				hallucinationRisk := 0.5
				if actualOutput != "" {
					scorePayload, _ := json.Marshal(map[string]interface{}{
						"request_id": runID + "_" + ec.ID,
						"prompt":     ec.Prompt,
						"response":   actualOutput,
					})
					scoreReq, _ := http.NewRequestWithContext(r.Context(), "POST",
						scorerURL+"/score", bytes.NewReader(scorePayload))
					scoreReq.Header.Set("Content-Type", "application/json")
					if scoreResp, scoreErr := scorerClient.Do(scoreReq); scoreErr == nil {
						defer scoreResp.Body.Close()
						scoreBody, _ := io.ReadAll(scoreResp.Body)
						var scored struct {
							OverallQualityScore float64 `json:"overall_quality_score"`
							HallucinationScore  float64 `json:"hallucination_score"`
						}
						if json.Unmarshal(scoreBody, &scored) == nil {
							qualityScore = scored.OverallQualityScore
							hallucinationRisk = scored.HallucinationScore
						}
					}
				}

				passed := qualityScore >= suite.PassThreshold && hallucinationRisk <= 0.4

				res := db.EvalResult{
					ID:                evalID(),
					RunID:             runID,
					CaseID:            ec.ID,
					ActualOutput:      actualOutput,
					QualityScore:      qualityScore,
					HallucinationRisk: hallucinationRisk,
					Passed:            passed,
					LatencyMs:         latencyMs,
					CostUSD:           costUSD,
				}
				_ = store.CreateEvalResult(r.Context(), res)

				mu.Lock()
				results = append(results, res)
				if passed {
					passCount++
				} else {
					failCount++
				}
				totalQ += qualityScore
				totalLat += float64(latencyMs)
				totalCost += costUSD
				mu.Unlock()
			}(c)
		}

		wg.Wait()

		n := len(cases)
		avgQ, avgLat, avgCost := 0.0, 0.0, 0.0
		if n > 0 {
			avgQ = totalQ / float64(n)
			avgLat = totalLat / float64(n)
			avgCost = totalCost / float64(n)
		}

		finalRun := db.EvalRun{
			ID:           runID,
			SuiteID:      suiteID,
			Model:        req.Model,
			PassCount:    passCount,
			FailCount:    failCount,
			AvgQuality:   avgQ,
			AvgLatencyMs: avgLat,
			AvgCostUSD:   avgCost,
		}
		_ = store.UpdateEvalRun(r.Context(), finalRun)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"run_id":      runID,
			"pass_count":  passCount,
			"fail_count":  failCount,
			"avg_quality": avgQ,
			"results":     results,
		})
	}
}

// getEvalRunHandler — GET /evals/runs/{runID}
func getEvalRunHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := chi.URLParam(r, "runID")
		run, err := store.GetEvalRun(r.Context(), runID)
		if err != nil || run == nil {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		results, err := store.ListEvalResults(r.Context(), runID)
		if err != nil {
			logger.Error("list eval results", zap.Error(err))
			http.Error(w, "failed to load results", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"run":     run,
			"results": results,
		})
	}
}

// listEvalRunsHandler — GET /evals/{suiteID}/runs
func listEvalRunsHandler(store *db.Store, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		suiteID := chi.URLParam(r, "suiteID")
		runs, err := store.ListEvalRuns(r.Context(), suiteID)
		if err != nil {
			logger.Error("list eval runs", zap.Error(err))
			http.Error(w, "failed to list runs", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"runs": runs})
	}
}
