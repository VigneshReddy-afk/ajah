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
	RateLimitRPM            int     `json:"rate_limit_rpm"`
	AlertEmailTo            string  `json:"alert_email_to"`
}

// SMTPConfig holds the SMTP server credentials used to send email alerts.
type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
}

// UserSetting holds per-user budget and alert configuration.
type UserSetting struct {
	UserID          string  `json:"user_id"`
	BudgetUSDPerDay float64 `json:"budget_usd_per_day"`
	AlertEmailTo    string  `json:"alert_email_to"`
	WebhookURL      string  `json:"webhook_url"`
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

	userSettingsMu sync.RWMutex
	userSettings   map[string]UserSetting
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
		userSettings:    make(map[string]UserSetting),
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
			rate_limit_rpm            INT     NOT NULL DEFAULT 0,
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

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS smtp_config (
			id         INT PRIMARY KEY DEFAULT 1,
			host       TEXT NOT NULL DEFAULT '',
			port       INT  NOT NULL DEFAULT 587,
			username   TEXT NOT NULL DEFAULT '',
			password   TEXT NOT NULL DEFAULT '',
			from_addr  TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create smtp_config table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_settings (
			user_id              TEXT PRIMARY KEY,
			budget_usd_per_day   FLOAT8 NOT NULL DEFAULT 0,
			alert_email_to       TEXT   NOT NULL DEFAULT '',
			webhook_url          TEXT   NOT NULL DEFAULT '',
			updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create user_settings table: %w", err)
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
		`ALTER TABLE feature_settings ADD COLUMN IF NOT EXISTS rate_limit_rpm INT NOT NULL DEFAULT 0`,
		`ALTER TABLE feature_settings ADD COLUMN IF NOT EXISTS alert_email_to TEXT NOT NULL DEFAULT ''`,
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
		       cross_model_enabled, cross_model_provider_url, cross_model_api_key, cross_model_model,
		       rate_limit_rpm, alert_email_to
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
			&f.RateLimitRPM, &f.AlertEmailTo,
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
			rate_limit_rpm, alert_email_to,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (feature_name) DO UPDATE SET
			cost_alert_threshold_usd = EXCLUDED.cost_alert_threshold_usd,
			pii_masking_enabled      = EXCLUDED.pii_masking_enabled,
			webhook_url              = EXCLUDED.webhook_url,
			cross_model_enabled      = EXCLUDED.cross_model_enabled,
			cross_model_provider_url = EXCLUDED.cross_model_provider_url,
			cross_model_api_key      = EXCLUDED.cross_model_api_key,
			cross_model_model        = EXCLUDED.cross_model_model,
			rate_limit_rpm           = EXCLUDED.rate_limit_rpm,
			alert_email_to           = EXCLUDED.alert_email_to,
			updated_at               = NOW()`,
		f.FeatureName, f.CostAlertThresholdUSD, f.PIIMaskingEnabled, f.WebhookURL,
		f.CrossModelEnabled, f.CrossModelProviderURL, f.CrossModelAPIKey, f.CrossModelModel,
		f.RateLimitRPM, f.AlertEmailTo)
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

// RateLimitRPMFor returns the configured requests-per-minute limit for a
// feature, or 0 if no limit is configured (meaning unlimited).
func (s *Store) RateLimitRPMFor(feature string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if f, ok := s.featureSettings[feature]; ok {
		return f.RateLimitRPM
	}
	return 0
}

// AlertEmailFor returns the email address configured to receive alerts for a
// feature, or "" if none is configured.
func (s *Store) AlertEmailFor(feature string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if f, ok := s.featureSettings[feature]; ok {
		return f.AlertEmailTo
	}
	return ""
}

// GetSMTPConfig returns the configured SMTP server settings used to send
// email alerts.
func (s *Store) GetSMTPConfig(ctx context.Context) (SMTPConfig, error) {
	var c SMTPConfig
	err := s.db.QueryRowContext(ctx, `
		SELECT host, port, username, password, from_addr
		FROM smtp_config WHERE id = 1
	`).Scan(&c.Host, &c.Port, &c.Username, &c.Password, &c.From)
	if err != nil {
		return SMTPConfig{}, err
	}
	return c, nil
}

// UpsertSMTPConfig inserts or updates the singleton SMTP configuration row.
func (s *Store) UpsertSMTPConfig(ctx context.Context, c SMTPConfig) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO smtp_config (id, host, port, username, password, from_addr, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, NOW())
		ON CONFLICT (id) DO UPDATE SET
			host       = EXCLUDED.host,
			port       = EXCLUDED.port,
			username   = EXCLUDED.username,
			password   = EXCLUDED.password,
			from_addr  = EXCLUDED.from_addr,
			updated_at = NOW()`,
		c.Host, c.Port, c.Username, c.Password, c.From)
	return err
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

// Ping checks connectivity to the PostgreSQL database.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// UpsertUserSetting inserts or updates a single user setting row.
func (s *Store) UpsertUserSetting(ctx context.Context, u UserSetting) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_settings
			(user_id, budget_usd_per_day, alert_email_to, webhook_url, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			budget_usd_per_day = EXCLUDED.budget_usd_per_day,
			alert_email_to     = EXCLUDED.alert_email_to,
			webhook_url        = EXCLUDED.webhook_url,
			updated_at         = NOW()
	`, u.UserID, u.BudgetUSDPerDay, u.AlertEmailTo, u.WebhookURL)
	return err
}

// GetUserSettings returns all user_settings rows where a budget is configured.
func (s *Store) GetUserSettings(ctx context.Context) ([]UserSetting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, budget_usd_per_day, alert_email_to, webhook_url
		FROM user_settings
		WHERE budget_usd_per_day > 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var settings []UserSetting
	for rows.Next() {
		var u UserSetting
		if err := rows.Scan(
			&u.UserID,
			&u.BudgetUSDPerDay,
			&u.AlertEmailTo,
			&u.WebhookURL,
		); err != nil {
			return nil, err
		}
		settings = append(settings, u)
	}
	return settings, rows.Err()
}

// RefreshUserCache reloads the in-memory user settings map from PostgreSQL.
func (s *Store) RefreshUserCache(ctx context.Context) error {
	settings, err := s.GetUserSettings(ctx)
	if err != nil {
		return err
	}
	s.userSettingsMu.Lock()
	defer s.userSettingsMu.Unlock()
	s.userSettings = make(map[string]UserSetting, len(settings))
	for _, u := range settings {
		s.userSettings[u.UserID] = u
	}
	return nil
}

// UserSettingFor returns the cached UserSetting for a user ID, and whether it exists.
func (s *Store) UserSettingFor(userID string) (UserSetting, bool) {
	s.userSettingsMu.RLock()
	defer s.userSettingsMu.RUnlock()
	u, ok := s.userSettings[userID]
	return u, ok
}
