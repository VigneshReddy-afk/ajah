package storage_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ajah/core/internal/storage"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

// ---- shared container setup -------------------------------------------------

const (
	chUser     = "default"
	chPassword = "ajahtest"
	chDatabase = "default"
)

var (
	sharedWriter *storage.Writer
	sharedConn   clickhouse.Conn
)

// TestMain starts a single ClickHouse container for the whole test binary,
// creates the traces table once, and tears everything down at the end.
// Individual tests truncate the table before each run for isolation.
func TestMain(m *testing.M) {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "clickhouse/clickhouse-server:24.3-alpine",
			ExposedPorts: []string{"9000/tcp", "8123/tcp"},
			Env: map[string]string{
				// Setting CLICKHOUSE_USER/PASSWORD enables network access
				// for the user and removes the localhost-only restriction.
				"CLICKHOUSE_USER":     chUser,
				"CLICKHOUSE_PASSWORD": chPassword,
				"CLICKHOUSE_DB":       chDatabase,
			},
			// /ping is an unauthenticated HTTP health-check endpoint that
			// returns "Ok." only when the HTTP and native servers are ready.
			// This is more reliable than log-scraping (logs go to a file)
			// or plain port-listening (port binds before the server is ready).
			WaitingFor: wait.ForHTTP("/ping").
				WithPort("8123/tcp").
				WithStatusCodeMatcher(func(status int) bool { return status == 200 }).
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		log.Fatalf("start clickhouse container: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	host, err := ctr.Host(ctx)
	if err != nil {
		log.Fatalf("get container host: %v", err)
	}
	p, err := ctr.MappedPort(ctx, "9000")
	if err != nil {
		log.Fatalf("get container port: %v", err)
	}
	addr := fmt.Sprintf("%s:%s", host, p.Port())
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s", chUser, chPassword, addr, chDatabase)

	sharedWriter, err = storage.New(dsn, zap.NewNop())
	if err != nil {
		log.Fatalf("storage.New: %v", err)
	}
	defer func() { _ = sharedWriter.Close() }()

	if err := sharedWriter.CreateTable(ctx); err != nil {
		log.Fatalf("CreateTable: %v", err)
	}

	sharedConn, err = clickhouse.Open(&clickhouse.Options{
		Addr:        []string{addr},
		Auth:        clickhouse.Auth{Database: chDatabase, Username: chUser, Password: chPassword},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("open verify conn: %v", err)
	}
	defer func() { _ = sharedConn.Close() }()

	os.Exit(m.Run())
}

// ---- helpers ----------------------------------------------------------------

// truncate clears all rows from traces before a test that needs a clean slate.
func truncate(t *testing.T) {
	t.Helper()
	if err := sharedConn.Exec(context.Background(), "TRUNCATE TABLE traces"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// countRows returns the total row count in the traces table.
func countRows(t *testing.T) uint64 {
	t.Helper()
	var n uint64
	if err := sharedConn.QueryRow(context.Background(),
		"SELECT count() FROM traces").Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// sample returns a fully-populated TraceRecord for test use.
func sample() storage.TraceRecord {
	return storage.TraceRecord{
		TraceID:      "trace-001",
		RequestID:    "req-001",
		UserID:       "user-1",
		SessionID:    "sess-1",
		FeatureName:  "chat",
		AgentStep:    "step-1",
		Provider:     "openai",
		Model:        "gpt-4o",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.00125,
		LatencyMs:    420,
		StatusCode:   200,
		MaskedPrompt: "hello world",
		WasPIIMasked: false,
		QualityScore: 0.95,
		// DateTime in ClickHouse has second-level precision.
		Timestamp: time.Now().UTC().Truncate(time.Second),
	}
}

// ---- tests ------------------------------------------------------------------

func TestWriter_CreateTable(t *testing.T) {
	// Idempotent: a second call must not error (IF NOT EXISTS).
	if err := sharedWriter.CreateTable(context.Background()); err != nil {
		t.Fatalf("second CreateTable: %v", err)
	}
}

func TestWriter_Write_SingleRecord(t *testing.T) {
	truncate(t)

	if err := sharedWriter.Write(context.Background(), sample()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n := countRows(t); n != 1 {
		t.Errorf("row count = %d, want 1", n)
	}
}

func TestWriter_Write_VerifiesFields(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	rec := sample()
	rec.TraceID = "trace-verify"
	rec.UserID = "user-verify"
	rec.CostUSD = 0.00042
	rec.WasPIIMasked = true
	rec.LatencyMs = 777

	if err := sharedWriter.Write(ctx, rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var (
		traceID      string
		userID       string
		costUSD      float64
		latencyMs    int64
		wasPIIMasked uint8
		ts           time.Time
	)
	err := sharedConn.QueryRow(ctx,
		`SELECT trace_id, user_id, cost_usd, latency_ms, was_pii_masked, timestamp
		 FROM traces WHERE trace_id = ?`, rec.TraceID,
	).Scan(&traceID, &userID, &costUSD, &latencyMs, &wasPIIMasked, &ts)
	if err != nil {
		t.Fatalf("query row: %v", err)
	}

	if traceID != rec.TraceID {
		t.Errorf("trace_id = %q, want %q", traceID, rec.TraceID)
	}
	if userID != rec.UserID {
		t.Errorf("user_id = %q, want %q", userID, rec.UserID)
	}
	if costUSD != rec.CostUSD {
		t.Errorf("cost_usd = %f, want %f", costUSD, rec.CostUSD)
	}
	if latencyMs != rec.LatencyMs {
		t.Errorf("latency_ms = %d, want %d", latencyMs, rec.LatencyMs)
	}
	if wasPIIMasked != 1 {
		t.Errorf("was_pii_masked = %d, want 1", wasPIIMasked)
	}
	if !ts.UTC().Equal(rec.Timestamp) {
		t.Errorf("timestamp = %v, want %v", ts.UTC(), rec.Timestamp)
	}
}

func TestWriter_BatchWrite_MultipleRecords(t *testing.T) {
	truncate(t)

	const n = 10
	records := make([]storage.TraceRecord, n)
	for i := range records {
		r := sample()
		r.TraceID = fmt.Sprintf("trace-batch-%02d", i)
		r.UserID = fmt.Sprintf("user-%d", i%3)
		records[i] = r
	}

	if err := sharedWriter.BatchWrite(context.Background(), records); err != nil {
		t.Fatalf("BatchWrite: %v", err)
	}
	if got := countRows(t); got != n {
		t.Errorf("row count = %d, want %d", got, n)
	}
}

func TestWriter_BatchWrite_EmptySlice(t *testing.T) {
	// No truncate needed — both calls must be pure no-ops.
	if err := sharedWriter.BatchWrite(context.Background(), nil); err != nil {
		t.Errorf("BatchWrite(nil): %v", err)
	}
	if err := sharedWriter.BatchWrite(context.Background(), []storage.TraceRecord{}); err != nil {
		t.Errorf("BatchWrite([]): %v", err)
	}
}

func TestWriter_PIIMasked_RoundTrip(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	masked := sample()
	masked.TraceID = "trace-pii-true"
	masked.WasPIIMasked = true

	clean := sample()
	clean.TraceID = "trace-pii-false"
	clean.WasPIIMasked = false

	if err := sharedWriter.BatchWrite(ctx, []storage.TraceRecord{masked, clean}); err != nil {
		t.Fatalf("BatchWrite: %v", err)
	}

	rows, err := sharedConn.Query(ctx,
		"SELECT trace_id, was_pii_masked FROM traces ORDER BY trace_id")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	want := map[string]uint8{"trace-pii-false": 0, "trace-pii-true": 1}
	got := make(map[string]uint8)
	for rows.Next() {
		var id string
		var flag uint8
		if err := rows.Scan(&id, &flag); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = flag
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	for id, wantFlag := range want {
		if gotFlag, ok := got[id]; !ok {
			t.Errorf("row %q not found", id)
		} else if gotFlag != wantFlag {
			t.Errorf("row %q: was_pii_masked = %d, want %d", id, gotFlag, wantFlag)
		}
	}
}

func TestWriter_AllStringFieldsRoundTrip(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	rec := storage.TraceRecord{
		TraceID:      "trace-strings",
		RequestID:    "req-strings",
		UserID:       "user-strings",
		SessionID:    "sess-strings",
		FeatureName:  "feature-strings",
		AgentStep:    "agent-strings",
		Provider:     "anthropic",
		Model:        "claude-3-5-sonnet",
		InputTokens:  1,
		OutputTokens: 1,
		CostUSD:      0.000001,
		LatencyMs:    1,
		StatusCode:   201,
		MaskedPrompt: "masked text here",
		WasPIIMasked: false,
		QualityScore: 0.5,
		Timestamp:    time.Now().UTC().Truncate(time.Second),
	}

	if err := sharedWriter.Write(ctx, rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var requestID, sessionID, featureName, agentStep, provider, model, prompt string
	err := sharedConn.QueryRow(ctx, `
		SELECT request_id, session_id, feature_name, agent_step,
		       provider, model, masked_prompt
		FROM traces WHERE trace_id = ?`, rec.TraceID,
	).Scan(&requestID, &sessionID, &featureName, &agentStep, &provider, &model, &prompt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	for _, c := range []struct{ got, want, field string }{
		{requestID, rec.RequestID, "request_id"},
		{sessionID, rec.SessionID, "session_id"},
		{featureName, rec.FeatureName, "feature_name"},
		{agentStep, rec.AgentStep, "agent_step"},
		{provider, rec.Provider, "provider"},
		{model, rec.Model, "model"},
		{prompt, rec.MaskedPrompt, "masked_prompt"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}
