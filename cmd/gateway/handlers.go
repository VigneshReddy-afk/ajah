package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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

// traceResponse is the JSON shape returned by GET /metrics/traces.
type traceResponse struct {
	TraceID           string    `json:"trace_id"`
	RequestID         string    `json:"request_id"`
	UserID            string    `json:"user_id"`
	SessionID         string    `json:"session_id"`
	FeatureName       string    `json:"feature_name"`
	AgentStep         string    `json:"agent_step"`
	ParentStepID      string    `json:"parent_step_id"`
	StepName          string    `json:"step_name"`
	ToolName          string    `json:"tool_name"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	CostUSD           float64   `json:"cost_usd"`
	LatencyMs         int64     `json:"latency_ms"`
	StatusCode        int       `json:"status_code"`
	MaskedPrompt      string    `json:"masked_prompt"`
	WasPIIMasked      bool      `json:"was_pii_masked"`
	QualityScore      float64   `json:"quality_score"`
	Timestamp         time.Time `json:"timestamp"`
	HallucinationRisk float64   `json:"hallucination_risk"`
	GroundingScore    float64   `json:"grounding_score"`
	RiskLevel         string    `json:"risk_level"`
	ShouldWarn        bool      `json:"should_warn"`
}

// warningItem is the JSON shape of a single high-risk response stored in Redis
// and returned by GET /warnings.
type warningItem struct {
	RequestID         string    `json:"request_id"`
	SessionID         string    `json:"session_id"`
	RiskLevel         string    `json:"risk_level"`
	HallucinationRisk float64   `json:"hallucination_risk"`
	GroundingScore    float64   `json:"grounding_score"`
	Reasons           []string  `json:"reasons"`
	Timestamp         time.Time `json:"timestamp"`
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
				TraceID:           rec.TraceID,
				RequestID:         rec.RequestID,
				UserID:            rec.UserID,
				SessionID:         rec.SessionID,
				FeatureName:       rec.FeatureName,
				AgentStep:         rec.AgentStep,
				ParentStepID:      rec.ParentStepID,
				StepName:          rec.StepName,
				ToolName:          rec.ToolName,
				Provider:          rec.Provider,
				Model:             rec.Model,
				InputTokens:       rec.InputTokens,
				OutputTokens:      rec.OutputTokens,
				CostUSD:           rec.CostUSD,
				LatencyMs:         rec.LatencyMs,
				StatusCode:        rec.StatusCode,
				MaskedPrompt:      rec.MaskedPrompt,
				WasPIIMasked:      rec.WasPIIMasked,
				QualityScore:      rec.QualityScore,
				Timestamp:         rec.Timestamp,
				HallucinationRisk: rec.HallucinationRisk,
				GroundingScore:    rec.GroundingScore,
				RiskLevel:         rec.RiskLevel,
				ShouldWarn:        rec.ShouldWarn,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
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

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(settingsPayload{
			FeatureSettings: features,
			ProviderKeys:    keys,
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
				TraceID:           t.TraceID,
				RequestID:         t.RequestID,
				UserID:            t.UserID,
				SessionID:         t.SessionID,
				FeatureName:       t.FeatureName,
				AgentStep:         t.AgentStep,
				ParentStepID:      t.ParentStepID,
				StepName:          t.StepName,
				ToolName:          t.ToolName,
				Provider:          t.Provider,
				Model:             t.Model,
				InputTokens:       t.InputTokens,
				OutputTokens:      t.OutputTokens,
				CostUSD:           t.CostUSD,
				LatencyMs:         t.LatencyMs,
				StatusCode:        t.StatusCode,
				MaskedPrompt:      t.MaskedPrompt,
				WasPIIMasked:      t.WasPIIMasked,
				QualityScore:      t.QualityScore,
				Timestamp:         t.Timestamp,
				HallucinationRisk: t.HallucinationRisk,
				GroundingScore:    t.GroundingScore,
				RiskLevel:         t.RiskLevel,
				ShouldWarn:        t.ShouldWarn,
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

// warningsHandler returns the last 100 high-risk responses from Redis and the
// total warning count for today.
func warningsHandler(rdb *redis.Client, logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		today := time.Now().UTC().Format("2006-01-02")

		items, err := rdb.LRange(ctx, "warnings:high:list", 0, 99).Result()
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

		store.RefreshCache(ctx)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}
