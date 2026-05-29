package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

// FeatureSetting holds per-feature observability configuration.
type FeatureSetting struct {
	FeatureName             string  `json:"feature_name"`
	CostAlertThresholdUSD   float64 `json:"cost_alert_threshold_usd"`
	PIIMaskingEnabled       bool    `json:"pii_masking_enabled"`
	WebhookURL              string  `json:"webhook_url"`
	CrossModelEnabled       bool    `json:"cross_model_enabled"`
	CrossModelProviderURL   string  `json:"cross_model_provider_url"`
	CrossModelAPIKey        string  `json:"cross_model_api_key"`
	CrossModelModel         string  `json:"cross_model_model"`
}

// ProviderKey holds an API key for a named LLM provider.
type ProviderKey struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
}

// Store wraps a PostgreSQL connection and exposes settings operations.
// It also maintains an in-memory cache of cost alert thresholds so that
// the hot event path can read thresholds without hitting the database.
type Store struct {
	db     *sql.DB
	logger *zap.Logger

	mu              sync.RWMutex
	thresholds      map[string]float64
	webhooks        map[string]string
	featureSettings map[string]FeatureSetting
}

// New opens a PostgreSQL connection and pings the server.
func New(dsn string, logger *zap.Logger) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &Store{
		db:              db,
		logger:          logger,
		thresholds:      make(map[string]float64),
		webhooks:        make(map[string]string),
		featureSettings: make(map[string]FeatureSetting),
	}, nil
}

// CreateTables creates the feature_settings and provider_keys tables if they
// do not already exist.
func (s *Store) CreateTables(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS feature_settings (
			feature_name              TEXT PRIMARY KEY,
			cost_alert_threshold_usd  FLOAT8  NOT NULL DEFAULT 1.0,
			pii_masking_enabled       BOOLEAN NOT NULL DEFAULT TRUE,
			webhook_url               TEXT    NOT NULL DEFAULT '',
			cross_model_enabled       BOOLEAN NOT NULL DEFAULT FALSE,
			cross_model_provider_url  TEXT    NOT NULL DEFAULT '',
			cross_model_api_key       TEXT    NOT NULL DEFAULT '',
			cross_model_model         TEXT    NOT NULL DEFAULT '',
			updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create feature_settings table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS provider_keys (
			provider    TEXT PRIMARY KEY,
			api_key     TEXT NOT NULL,
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create provider_keys table: %w", err)
	}

	return nil
}

// MigrateTables adds columns introduced after the initial schema version.
// Safe to call on any existing database.
func (s *Store) MigrateTables(ctx context.Context) error {
	migrations := []string{
		`ALTER TABLE feature_settings ADD COLUMN IF NOT EXISTS webhook_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE feature_settings ADD COLUMN IF NOT EXISTS cross_model_enabled BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE feature_settings ADD COLUMN IF NOT EXISTS cross_model_provider_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE feature_settings ADD COLUMN IF NOT EXISTS cross_model_api_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE feature_settings ADD COLUMN IF NOT EXISTS cross_model_model TEXT NOT NULL DEFAULT ''`,
	}
	for _, m := range migrations {
		if _, err := s.db.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("migrate feature_settings: %w", err)
		}
	}
	return nil
}

// GetSettings returns all rows from feature_settings.
func (s *Store) GetSettings(ctx context.Context) ([]FeatureSetting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT feature_name, cost_alert_threshold_usd, pii_masking_enabled, webhook_url,
		       cross_model_enabled, cross_model_provider_url, cross_model_api_key, cross_model_model
		FROM feature_settings ORDER BY feature_name`)
	if err != nil {
		return nil, fmt.Errorf("query settings: %w", err)
	}
	defer rows.Close()

	var out []FeatureSetting
	for rows.Next() {
		var f FeatureSetting
		if err := rows.Scan(
			&f.FeatureName, &f.CostAlertThresholdUSD, &f.PIIMaskingEnabled, &f.WebhookURL,
			&f.CrossModelEnabled, &f.CrossModelProviderURL, &f.CrossModelAPIKey, &f.CrossModelModel,
		); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpsertSetting inserts or updates a single feature setting row.
func (s *Store) UpsertSetting(ctx context.Context, f FeatureSetting) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feature_settings (
			feature_name, cost_alert_threshold_usd, pii_masking_enabled, webhook_url,
			cross_model_enabled, cross_model_provider_url, cross_model_api_key, cross_model_model,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (feature_name) DO UPDATE SET
			cost_alert_threshold_usd = EXCLUDED.cost_alert_threshold_usd,
			pii_masking_enabled      = EXCLUDED.pii_masking_enabled,
			webhook_url              = EXCLUDED.webhook_url,
			cross_model_enabled      = EXCLUDED.cross_model_enabled,
			cross_model_provider_url = EXCLUDED.cross_model_provider_url,
			cross_model_api_key      = EXCLUDED.cross_model_api_key,
			cross_model_model        = EXCLUDED.cross_model_model,
			updated_at               = NOW()`,
		f.FeatureName, f.CostAlertThresholdUSD, f.PIIMaskingEnabled, f.WebhookURL,
		f.CrossModelEnabled, f.CrossModelProviderURL, f.CrossModelAPIKey, f.CrossModelModel)
	if err != nil {
		return fmt.Errorf("upsert setting %q: %w", f.FeatureName, err)
	}
	return nil
}

// GetProviderKeys returns all rows from provider_keys.
func (s *Store) GetProviderKeys(ctx context.Context) ([]ProviderKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, api_key FROM provider_keys ORDER BY provider`)
	if err != nil {
		return nil, fmt.Errorf("query provider keys: %w", err)
	}
	defer rows.Close()

	var out []ProviderKey
	for rows.Next() {
		var k ProviderKey
		if err := rows.Scan(&k.Provider, &k.APIKey); err != nil {
			return nil, fmt.Errorf("scan provider key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// UpsertProviderKey inserts or updates a single provider key row.
func (s *Store) UpsertProviderKey(ctx context.Context, k ProviderKey) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_keys (provider, api_key, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (provider) DO UPDATE SET
			api_key    = EXCLUDED.api_key,
			updated_at = NOW()`,
		k.Provider, k.APIKey)
	if err != nil {
		return fmt.Errorf("upsert provider key %q: %w", k.Provider, err)
	}
	return nil
}

// ThresholdFor returns the cost alert threshold for a feature, falling back
// to $1.00 per day if no setting exists.
func (s *Store) ThresholdFor(feature string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.thresholds[feature]; ok {
		return t
	}
	return 1.0
}

// WebhookURLFor returns the webhook URL configured for a feature, or "" if none.
func (s *Store) WebhookURLFor(feature string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.webhooks[feature]
}

// RefreshCache reloads the in-memory threshold, webhook, and feature setting maps from PostgreSQL.
// Safe to call from any goroutine.
func (s *Store) RefreshCache(ctx context.Context) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		s.logger.Warn("failed to refresh threshold cache", zap.Error(err))
		return
	}
	m := make(map[string]float64, len(settings))
	w := make(map[string]string, len(settings))
	fs := make(map[string]FeatureSetting, len(settings))
	for _, f := range settings {
		m[f.FeatureName] = f.CostAlertThresholdUSD
		if f.WebhookURL != "" {
			w[f.FeatureName] = f.WebhookURL
		}
		fs[f.FeatureName] = f
	}
	s.mu.Lock()
	s.thresholds = m
	s.webhooks = w
	s.featureSettings = fs
	s.mu.Unlock()
}

// FeatureSettingFor returns the cached FeatureSetting for a feature, or nil if not found.
func (s *Store) FeatureSettingFor(feature string) *FeatureSetting {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if f, ok := s.featureSettings[feature]; ok {
		return &f
	}
	return nil
}

// Close closes the underlying database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}
