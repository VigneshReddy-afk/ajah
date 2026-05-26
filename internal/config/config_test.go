package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Explicitly blank all relevant env vars so machine-level values don't bleed in.
	for _, key := range []string{
		"PORT", "REDIS_URL", "DATABASE_URL", "CLICKHOUSE_URL", "LOG_LEVEL",
		"MAX_REQUEST_BODY_BYTES", "ASYNC_WORKER_COUNT", "PROVIDER_TIMEOUT_SECONDS",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Port != defaultPort {
		t.Errorf("Port = %q, want %q", cfg.Port, defaultPort)
	}
	if cfg.RedisURL != defaultRedisURL {
		t.Errorf("RedisURL = %q, want %q", cfg.RedisURL, defaultRedisURL)
	}
	if cfg.DatabaseURL != defaultDatabaseURL {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, defaultDatabaseURL)
	}
	if cfg.ClickHouseURL != defaultClickHouseURL {
		t.Errorf("ClickHouseURL = %q, want %q", cfg.ClickHouseURL, defaultClickHouseURL)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, defaultLogLevel)
	}
	if cfg.MaxRequestBodyBytes != defaultMaxRequestBodyBytes {
		t.Errorf("MaxRequestBodyBytes = %d, want %d", cfg.MaxRequestBodyBytes, defaultMaxRequestBodyBytes)
	}
	if cfg.AsyncWorkerCount != defaultAsyncWorkerCount {
		t.Errorf("AsyncWorkerCount = %d, want %d", cfg.AsyncWorkerCount, defaultAsyncWorkerCount)
	}
	if cfg.ProviderTimeoutSeconds != defaultProviderTimeoutSeconds {
		t.Errorf("ProviderTimeoutSeconds = %d, want %d", cfg.ProviderTimeoutSeconds, defaultProviderTimeoutSeconds)
	}
}

func TestLoad_WithEnvVars(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("REDIS_URL", "redis://myhost:6380")
	t.Setenv("DATABASE_URL", "postgres://myhost:5432/mydb")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("MAX_REQUEST_BODY_BYTES", "5242880")
	t.Setenv("ASYNC_WORKER_COUNT", "20")
	t.Setenv("PROVIDER_TIMEOUT_SECONDS", "60")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.RedisURL != "redis://myhost:6380" {
		t.Errorf("RedisURL = %q, want %q", cfg.RedisURL, "redis://myhost:6380")
	}
	if cfg.DatabaseURL != "postgres://myhost:5432/mydb" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://myhost:5432/mydb")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.MaxRequestBodyBytes != 5242880 {
		t.Errorf("MaxRequestBodyBytes = %d, want %d", cfg.MaxRequestBodyBytes, int64(5242880))
	}
	if cfg.AsyncWorkerCount != 20 {
		t.Errorf("AsyncWorkerCount = %d, want %d", cfg.AsyncWorkerCount, 20)
	}
	if cfg.ProviderTimeoutSeconds != 60 {
		t.Errorf("ProviderTimeoutSeconds = %d, want %d", cfg.ProviderTimeoutSeconds, 60)
	}
}

func TestLoad_InvalidIntEnvVar(t *testing.T) {
	t.Setenv("MAX_REQUEST_BODY_BYTES", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid MAX_REQUEST_BODY_BYTES, got nil")
	}
}

func TestValidate_MissingDatabaseURL(t *testing.T) {
	cfg := &Config{
		Port:                   "8080",
		RedisURL:               "redis://localhost:6379",
		DatabaseURL:            "",
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       10,
		ProviderTimeoutSeconds: 30,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() expected error for empty DatabaseURL, got nil")
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Port:                   "8080",
		RedisURL:               "redis://localhost:6379",
		DatabaseURL:            "postgres://localhost:5432/observatory",
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       10,
		ProviderTimeoutSeconds: 30,
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}
