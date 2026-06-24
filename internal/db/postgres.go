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
	FeatureName           string  `json:"feature_name"`
	CostAlertThresholdUSD float64 `json:"cost_alert_threshold_usd"`
	PIIMaskingEnabled     bool    `json:"pii_masking_enabled"`
	WebhookURL            string  `json:"webhook_url"`
	CrossModelEnabled     bool    `json:"cross_model_enabled"`
	CrossModelProviderURL string  `json:"cross_model_provider_url"`
	CrossModelAPIKey      string  `json:"cross_model_api_key"`
	CrossModelModel       string  `json:"cross_model_model"`
	RateLimitRPM          int     `json:"rate_limit_rpm"`
	AlertEmailTo          string  `json:"alert_email_to"`
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

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS eval_suites (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			pass_threshold FLOAT8 NOT NULL DEFAULT 0.7,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create eval_suites table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS eval_cases (
			id              TEXT PRIMARY KEY,
			suite_id        TEXT NOT NULL REFERENCES eval_suites(id) ON DELETE CASCADE,
			prompt          TEXT NOT NULL,
			expected_output TEXT NOT NULL DEFAULT '',
			tags            TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create eval_cases table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS eval_runs (
			id            TEXT PRIMARY KEY,
			suite_id      TEXT NOT NULL REFERENCES eval_suites(id) ON DELETE CASCADE,
			model         TEXT NOT NULL,
			provider      TEXT NOT NULL,
			pass_count    INT NOT NULL DEFAULT 0,
			fail_count    INT NOT NULL DEFAULT 0,
			avg_quality   FLOAT8 NOT NULL DEFAULT 0,
			avg_latency_ms FLOAT8 NOT NULL DEFAULT 0,
			avg_cost_usd  FLOAT8 NOT NULL DEFAULT 0,
			run_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create eval_runs table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS eval_results (
			id                TEXT PRIMARY KEY,
			run_id            TEXT NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE,
			case_id           TEXT NOT NULL REFERENCES eval_cases(id) ON DELETE CASCADE,
			actual_output     TEXT NOT NULL DEFAULT '',
			quality_score     FLOAT8 NOT NULL DEFAULT 0,
			hallucination_risk FLOAT8 NOT NULL DEFAULT 0,
			passed            BOOLEAN NOT NULL DEFAULT FALSE,
			latency_ms        INT NOT NULL DEFAULT 0,
			cost_usd          FLOAT8 NOT NULL DEFAULT 0,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create eval_results table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS orgs (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			slug       TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create orgs table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
			email         TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role          TEXT NOT NULL DEFAULT 'member',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create users table: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS auth_sessions (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
			token_hash  TEXT NOT NULL UNIQUE,
			expires_at  TIMESTAMPTZ NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create auth_sessions table: %w", err)
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

// ── Eval types ────────────────────────────────────────────────────────────────

// EvalSuite is a named collection of prompt/expected-output test cases.
type EvalSuite struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	PassThreshold float64   `json:"pass_threshold"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// EvalCase is a single prompt + expected output pair inside a suite.
type EvalCase struct {
	ID             string    `json:"id"`
	SuiteID        string    `json:"suite_id"`
	Prompt         string    `json:"prompt"`
	ExpectedOutput string    `json:"expected_output"`
	Tags           string    `json:"tags"`
	CreatedAt      time.Time `json:"created_at"`
}

// EvalRun is one execution of a suite against a specific model.
type EvalRun struct {
	ID           string    `json:"id"`
	SuiteID      string    `json:"suite_id"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	PassCount    int       `json:"pass_count"`
	FailCount    int       `json:"fail_count"`
	AvgQuality   float64   `json:"avg_quality"`
	AvgLatencyMs float64   `json:"avg_latency_ms"`
	AvgCostUSD   float64   `json:"avg_cost_usd"`
	RunAt        time.Time `json:"run_at"`
}

// EvalResult is the outcome for a single case within a run.
type EvalResult struct {
	ID                string    `json:"id"`
	RunID             string    `json:"run_id"`
	CaseID            string    `json:"case_id"`
	Prompt            string    `json:"prompt,omitempty"`
	ExpectedOutput    string    `json:"expected_output,omitempty"`
	ActualOutput      string    `json:"actual_output"`
	QualityScore      float64   `json:"quality_score"`
	HallucinationRisk float64   `json:"hallucination_risk"`
	Passed            bool      `json:"passed"`
	LatencyMs         int       `json:"latency_ms"`
	CostUSD           float64   `json:"cost_usd"`
	CreatedAt         time.Time `json:"created_at"`
}

// ── Eval methods ──────────────────────────────────────────────────────────────

// CreateEvalSuite inserts a new suite row.
func (s *Store) CreateEvalSuite(ctx context.Context, suite EvalSuite) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO eval_suites (id, name, description, pass_threshold, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, suite.ID, suite.Name, suite.Description, suite.PassThreshold)
	return err
}

// ListEvalSuites returns all suites ordered by created_at desc.
func (s *Store) ListEvalSuites(ctx context.Context) ([]EvalSuite, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, pass_threshold, created_at, updated_at
		FROM eval_suites ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalSuite
	for rows.Next() {
		var e EvalSuite
		if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.PassThreshold, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if out == nil {
		out = []EvalSuite{}
	}
	return out, rows.Err()
}

// GetEvalSuite returns a single suite by ID.
func (s *Store) GetEvalSuite(ctx context.Context, id string) (*EvalSuite, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, pass_threshold, created_at, updated_at
		FROM eval_suites WHERE id = $1
	`, id)
	var e EvalSuite
	if err := row.Scan(&e.ID, &e.Name, &e.Description, &e.PassThreshold, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// DeleteEvalSuite deletes a suite and cascades to cases, runs, results.
func (s *Store) DeleteEvalSuite(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM eval_suites WHERE id = $1`, id)
	return err
}

// CreateEvalCase inserts a new case row.
func (s *Store) CreateEvalCase(ctx context.Context, c EvalCase) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO eval_cases (id, suite_id, prompt, expected_output, tags, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, c.ID, c.SuiteID, c.Prompt, c.ExpectedOutput, c.Tags)
	return err
}

// ListEvalCases returns all cases for a suite ordered by created_at.
func (s *Store) ListEvalCases(ctx context.Context, suiteID string) ([]EvalCase, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, suite_id, prompt, expected_output, tags, created_at
		FROM eval_cases WHERE suite_id = $1 ORDER BY created_at ASC
	`, suiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalCase
	for rows.Next() {
		var c EvalCase
		if err := rows.Scan(&c.ID, &c.SuiteID, &c.Prompt, &c.ExpectedOutput, &c.Tags, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []EvalCase{}
	}
	return out, rows.Err()
}

// DeleteEvalCase deletes a single case.
func (s *Store) DeleteEvalCase(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM eval_cases WHERE id = $1`, id)
	return err
}

// CreateEvalRun inserts a new run row.
func (s *Store) CreateEvalRun(ctx context.Context, run EvalRun) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO eval_runs (id, suite_id, model, provider, pass_count, fail_count,
			avg_quality, avg_latency_ms, avg_cost_usd, run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
	`, run.ID, run.SuiteID, run.Model, run.Provider,
		run.PassCount, run.FailCount, run.AvgQuality, run.AvgLatencyMs, run.AvgCostUSD)
	return err
}

// UpdateEvalRun updates aggregates on an existing run row.
func (s *Store) UpdateEvalRun(ctx context.Context, run EvalRun) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE eval_runs SET
			pass_count = $2, fail_count = $3,
			avg_quality = $4, avg_latency_ms = $5, avg_cost_usd = $6
		WHERE id = $1
	`, run.ID, run.PassCount, run.FailCount, run.AvgQuality, run.AvgLatencyMs, run.AvgCostUSD)
	return err
}

// ListEvalRuns returns runs for a suite, most recent first.
func (s *Store) ListEvalRuns(ctx context.Context, suiteID string) ([]EvalRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, suite_id, model, provider, pass_count, fail_count,
			avg_quality, avg_latency_ms, avg_cost_usd, run_at
		FROM eval_runs WHERE suite_id = $1 ORDER BY run_at DESC
	`, suiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalRun
	for rows.Next() {
		var r EvalRun
		if err := rows.Scan(&r.ID, &r.SuiteID, &r.Model, &r.Provider,
			&r.PassCount, &r.FailCount, &r.AvgQuality, &r.AvgLatencyMs, &r.AvgCostUSD, &r.RunAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []EvalRun{}
	}
	return out, rows.Err()
}

// GetEvalRun returns a single run by ID.
func (s *Store) GetEvalRun(ctx context.Context, id string) (*EvalRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, suite_id, model, provider, pass_count, fail_count,
			avg_quality, avg_latency_ms, avg_cost_usd, run_at
		FROM eval_runs WHERE id = $1
	`, id)
	var r EvalRun
	if err := row.Scan(&r.ID, &r.SuiteID, &r.Model, &r.Provider,
		&r.PassCount, &r.FailCount, &r.AvgQuality, &r.AvgLatencyMs, &r.AvgCostUSD, &r.RunAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// CreateEvalResult inserts a single case result.
func (s *Store) CreateEvalResult(ctx context.Context, res EvalResult) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO eval_results (id, run_id, case_id, actual_output, quality_score,
			hallucination_risk, passed, latency_ms, cost_usd, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
	`, res.ID, res.RunID, res.CaseID, res.ActualOutput, res.QualityScore,
		res.HallucinationRisk, res.Passed, res.LatencyMs, res.CostUSD)
	return err
}

// ListEvalResults returns all results for a run, joined with case prompt/expected.
func (s *Store) ListEvalResults(ctx context.Context, runID string) ([]EvalResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.run_id, r.case_id,
			c.prompt, c.expected_output,
			r.actual_output, r.quality_score, r.hallucination_risk,
			r.passed, r.latency_ms, r.cost_usd, r.created_at
		FROM eval_results r
		JOIN eval_cases c ON c.id = r.case_id
		WHERE r.run_id = $1
		ORDER BY r.created_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalResult
	for rows.Next() {
		var res EvalResult
		if err := rows.Scan(
			&res.ID, &res.RunID, &res.CaseID,
			&res.Prompt, &res.ExpectedOutput,
			&res.ActualOutput, &res.QualityScore, &res.HallucinationRisk,
			&res.Passed, &res.LatencyMs, &res.CostUSD, &res.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	if out == nil {
		out = []EvalResult{}
	}
	return out, rows.Err()
}

// ── Auth types ────────────────────────────────────────────────────────────────

// Org is a tenant organisation.
type Org struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// User is a member of an Org.
type User struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// AuthSession is a logged-in session.
type AuthSession struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	OrgID     string    `json:"org_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ── Auth methods ──────────────────────────────────────────────────────────────

// CreateOrg inserts a new org row.
func (s *Store) CreateOrg(ctx context.Context, org Org) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO orgs (id, name, slug, created_at) VALUES ($1, $2, $3, NOW())`,
		org.ID, org.Name, org.Slug,
	)
	return err
}

// OrgBySlug returns the org with the given slug, or nil if not found.
func (s *Store) OrgBySlug(ctx context.Context, slug string) (*Org, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, created_at FROM orgs WHERE slug = $1`, slug)
	var o Org
	if err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// CreateUser inserts a new user row.
func (s *Store) CreateUser(ctx context.Context, u User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, org_id, email, password_hash, role, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		u.ID, u.OrgID, u.Email, u.PasswordHash, u.Role,
	)
	return err
}

// UserByEmail returns the user with the given email, or nil if not found.
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, email, password_hash, role, created_at
		 FROM users WHERE email = $1`, email)
	var u User
	if err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// UserByID returns the user with the given ID, or nil if not found.
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, email, password_hash, role, created_at
		 FROM users WHERE id = $1`, id)
	var u User
	if err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// ListOrgUsers returns all users in an org.
func (s *Store) ListOrgUsers(ctx context.Context, orgID string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, email, password_hash, role, created_at
		 FROM users WHERE org_id = $1 ORDER BY created_at ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if out == nil {
		out = []User{}
	}
	return out, rows.Err()
}

// CreateAuthSession inserts a new session row.
func (s *Store) CreateAuthSession(ctx context.Context, sess AuthSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_sessions (id, user_id, org_id, token_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		sess.ID, sess.UserID, sess.OrgID, sess.TokenHash, sess.ExpiresAt,
	)
	return err
}

// SessionByTokenHash returns session+user info for a token hash, or nil if not found.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (*AuthSession, *User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.user_id, s.org_id, s.token_hash, s.expires_at, s.created_at,
		       u.email, u.role
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
	`, tokenHash)
	var sess AuthSession
	var email, role string
	if err := row.Scan(
		&sess.ID, &sess.UserID, &sess.OrgID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt,
		&email, &role,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	u := &User{ID: sess.UserID, OrgID: sess.OrgID, Email: email, Role: role}
	return &sess, u, nil
}

// DeleteAuthSession removes a session by its token hash (logout).
func (s *Store) DeleteAuthSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteExpiredSessions removes all sessions past their expiry — call periodically.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE expires_at < NOW()`)
	return err
}

// OrgByID returns the org with the given ID, or nil if not found.
func (s *Store) OrgByID(ctx context.Context, id string) (*Org, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, created_at FROM orgs WHERE id = $1`, id)
	var o Org
	if err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}
