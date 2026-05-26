package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ajah/core/internal/db"
	"github.com/ajah/core/internal/storage"
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
	TraceID      string    `json:"trace_id"`
	RequestID    string    `json:"request_id"`
	UserID       string    `json:"user_id"`
	SessionID    string    `json:"session_id"`
	FeatureName  string    `json:"feature_name"`
	AgentStep    string    `json:"agent_step"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	LatencyMs    int64     `json:"latency_ms"`
	StatusCode   int       `json:"status_code"`
	MaskedPrompt string    `json:"masked_prompt"`
	WasPIIMasked bool      `json:"was_pii_masked"`
	QualityScore float64   `json:"quality_score"`
	Timestamp    time.Time `json:"timestamp"`
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
				TraceID:      rec.TraceID,
				RequestID:    rec.RequestID,
				UserID:       rec.UserID,
				SessionID:    rec.SessionID,
				FeatureName:  rec.FeatureName,
				AgentStep:    rec.AgentStep,
				Provider:     rec.Provider,
				Model:        rec.Model,
				InputTokens:  rec.InputTokens,
				OutputTokens: rec.OutputTokens,
				CostUSD:      rec.CostUSD,
				LatencyMs:    rec.LatencyMs,
				StatusCode:   rec.StatusCode,
				MaskedPrompt: rec.MaskedPrompt,
				WasPIIMasked: rec.WasPIIMasked,
				QualityScore: rec.QualityScore,
				Timestamp:    rec.Timestamp,
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
