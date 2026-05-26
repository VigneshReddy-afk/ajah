package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"go.uber.org/zap"
)

// TraceRecord is a single observability record stored in ClickHouse.
type TraceRecord struct {
	TraceID      string
	RequestID    string
	UserID       string
	SessionID    string
	FeatureName  string
	AgentStep    string
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	LatencyMs    int64
	StatusCode   int
	MaskedPrompt string
	WasPIIMasked bool
	QualityScore float64
	Timestamp    time.Time
}

// Writer holds a ClickHouse connection and exposes write operations.
type Writer struct {
	conn   clickhouse.Conn
	logger *zap.Logger
}

const createTableSQL = `
CREATE TABLE IF NOT EXISTS traces (
    trace_id       String,
    request_id     String,
    user_id        String,
    session_id     String,
    feature_name   String,
    agent_step     String,
    provider       String,
    model          String,
    input_tokens   Int32,
    output_tokens  Int32,
    cost_usd       Float64,
    latency_ms     Int64,
    status_code    Int32,
    masked_prompt  String,
    was_pii_masked UInt8,
    quality_score  Float64,
    timestamp      DateTime
) ENGINE = MergeTree()
ORDER BY (timestamp, user_id, feature_name)
`

// New opens a ClickHouse connection using dsn and pings the server to verify
// connectivity. The DSN must follow the form:
//
//	clickhouse://[user[:password]@]host:port/database
func New(dsn string, logger *zap.Logger) (*Writer, error) {
	opts, err := parseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse connection: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}

	return &Writer{conn: conn, logger: logger}, nil
}

// CreateTable creates the traces table if it does not already exist.
func (w *Writer) CreateTable(ctx context.Context) error {
	if err := w.conn.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("create traces table: %w", err)
	}
	return nil
}

// Write inserts a single TraceRecord. It delegates to BatchWrite.
func (w *Writer) Write(ctx context.Context, record TraceRecord) error {
	return w.BatchWrite(ctx, []TraceRecord{record})
}

// BatchWrite inserts all records in a single ClickHouse batch operation.
// An empty slice is a no-op.
func (w *Writer) BatchWrite(ctx context.Context, records []TraceRecord) error {
	if len(records) == 0 {
		return nil
	}

	batch, err := w.conn.PrepareBatch(ctx, `INSERT INTO traces (
		trace_id, request_id, user_id, session_id, feature_name,
		agent_step, provider, model, input_tokens, output_tokens,
		cost_usd, latency_ms, status_code, masked_prompt,
		was_pii_masked, quality_score, timestamp
	)`)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, r := range records {
		if err := batch.Append(
			r.TraceID,
			r.RequestID,
			r.UserID,
			r.SessionID,
			r.FeatureName,
			r.AgentStep,
			r.Provider,
			r.Model,
			int32(r.InputTokens),
			int32(r.OutputTokens),
			r.CostUSD,
			r.LatencyMs,
			int32(r.StatusCode),
			r.MaskedPrompt,
			boolToUint8(r.WasPIIMasked),
			r.QualityScore,
			r.Timestamp,
		); err != nil {
			return fmt.Errorf("append record %q: %w", r.TraceID, err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("send batch: %w", err)
	}

	w.logger.Info("batch written to clickhouse", zap.Int("count", len(records)))
	return nil
}

// Close releases the underlying ClickHouse connection.
func (w *Writer) Close() error {
	return w.conn.Close()
}

// parseDSN converts a clickhouse:// DSN into clickhouse.Options.
func parseDSN(dsn string) (*clickhouse.Options, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DSN: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("DSN missing host")
	}

	database := strings.TrimPrefix(u.Path, "/")
	if database == "" {
		database = "default"
	}
	username := u.User.Username()
	if username == "" {
		username = "default"
	}
	password, _ := u.User.Password()

	return &clickhouse.Options{
		Addr: []string{u.Host},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
		DialTimeout: 10 * time.Second,
	}, nil
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
