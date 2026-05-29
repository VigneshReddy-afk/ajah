//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajah/core/internal/attribution"
	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/events"
	"github.com/ajah/core/internal/flagging"
	"github.com/ajah/core/internal/proxy"
	"github.com/ajah/core/internal/sessions"
	"github.com/ajah/core/internal/storage"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

// ── ClickHouse shared container ───────────────────────────────────────────────
// Started once for the whole integration test binary by TestMain.

var chWriter *storage.Writer

func TestMain(m *testing.M) {
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "clickhouse/clickhouse-server:24.3-alpine",
			ExposedPorts: []string{"9000/tcp", "8123/tcp"},
			Env: map[string]string{
				"CLICKHOUSE_USER":     "default",
				"CLICKHOUSE_PASSWORD": "ajahtest",
				"CLICKHOUSE_DB":       "default",
			},
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
	port, err := ctr.MappedPort(ctx, "9000")
	if err != nil {
		log.Fatalf("get container port: %v", err)
	}
	dsn := fmt.Sprintf("clickhouse://default:ajahtest@%s:%s/default", host, port.Port())

	chWriter, err = storage.New(dsn, zap.NewNop())
	if err != nil {
		log.Fatalf("storage.New: %v", err)
	}
	defer func() { _ = chWriter.Close() }()

	if err := chWriter.CreateTable(ctx); err != nil {
		log.Fatalf("CreateTable: %v", err)
	}
	if err := chWriter.CreateSessionTable(ctx); err != nil {
		log.Fatalf("CreateSessionTable: %v", err)
	}
	if err := chWriter.MigrateTable(ctx); err != nil {
		log.Fatalf("MigrateTable: %v", err)
	}

	os.Exit(m.Run())
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("invalid REDIS_URL %q: %v", redisURL, err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("Redis not available at %q: %v", redisURL, err)
	}
	return rdb
}

func mockProvider() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			`{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
		))
	}))
}

// sendRequest posts a chat completion to gatewaySrv with the given session and step headers.
func sendRequest(t *testing.T, gatewaySrv *httptest.Server, sessionID, stepName string) {
	t.Helper()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest(http.MethodPost,
		gatewaySrv.URL+"/v1/chat/completions",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "sk-test123")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "integration-user")
	req.Header.Set("X-Feature-Name", "integration-feature")
	req.Header.Set("X-Session-ID", sessionID)
	req.Header.Set("X-Agent-Step", stepName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("step %q: status = %d, want 200", stepName, resp.StatusCode)
	}
}

// findSession returns the session record matching sessionID from the list, or nil.
func findSession(recs []storage.SessionRecord, sessionID string) *storage.SessionRecord {
	for i := range recs {
		if recs[i].SessionID == sessionID {
			return &recs[i]
		}
	}
	return nil
}

// ── TestGatewayIntegration ────────────────────────────────────────────────────
// Exercises the full request path:
//
//	test client → gateway handler → mock OpenAI → async attribution → Redis

func TestGatewayIntegration(t *testing.T) {
	rdb := redisClient(t)
	ctx := context.Background()

	// Clean up cost keys this test will write so reruns are idempotent.
	today := time.Now().UTC().Format("2006-01-02")
	userKey := fmt.Sprintf("cost:user:user_1:daily:%s", today)
	featureKey := fmt.Sprintf("cost:feature:chat:daily:%s", today)
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), userKey, featureKey).Err()
	})

	mock := mockProvider()
	t.Cleanup(mock.Close)

	cfg := &config.Config{
		Port:                   "0",
		RedisURL:               os.Getenv("REDIS_URL"),
		DatabaseURL:            "postgres://localhost:5432/observatory",
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       2,
		ProviderTimeoutSeconds: 10,
	}
	if cfg.RedisURL == "" {
		cfg.RedisURL = "redis://localhost:6379"
	}

	logger := zap.NewNop()
	engine := attribution.New(rdb, logger)

	var (
		capturedMu sync.Mutex
		captured   events.RequestEvent
		capturedOK bool
	)
	processFn := func(ctx context.Context, event events.RequestEvent) error {
		capturedMu.Lock()
		captured = event
		capturedOK = true
		capturedMu.Unlock()
		_, err := engine.Process(ctx, event)
		return err
	}

	emitter := events.New(cfg.AsyncWorkerCount*10, cfg.AsyncWorkerCount, logger, processFn)
	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	t.Cleanup(func() { cancelEmitter(); emitter.Stop() })
	emitter.Start(emitterCtx)

	h := proxy.New(cfg, emitter, logger)
	h.SetProviderURL("openai", mock.URL)
	gatewaySrv := httptest.NewServer(h)
	t.Cleanup(gatewaySrv.Close)

	req, err := http.NewRequest(http.MethodPost,
		gatewaySrv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`),
	)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "sk-test123")
	req.Header.Set("X-User-ID", "user_1")
	req.Header.Set("X-Feature-Name", "chat")
	req.Header.Set("X-Agent-Step", "step-1")
	req.Header.Set("X-Session-ID", "sess-integration-1")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("response status = %d, want 200", resp.StatusCode)
	}

	time.Sleep(100 * time.Millisecond)

	// gpt-4o: 10 * $0.000005 + 5 * $0.000015 = $0.000125
	userCost, err := rdb.Get(ctx, userKey).Float64()
	if err != nil {
		t.Fatalf("reading Redis key %q: %v", userKey, err)
	}
	if userCost <= 0 {
		t.Errorf("Redis %q = %f, want > 0", userKey, userCost)
	}

	featureCost, err := rdb.Get(ctx, featureKey).Float64()
	if err != nil {
		t.Fatalf("reading Redis key %q: %v", featureKey, err)
	}
	if featureCost <= 0 {
		t.Errorf("Redis %q = %f, want > 0", featureKey, featureCost)
	}

	t.Logf("cost:user:user_1  = $%.8f", userCost)
	t.Logf("cost:feature:chat = $%.8f", featureCost)

	capturedMu.Lock()
	evt := captured
	ok := capturedOK
	capturedMu.Unlock()

	if !ok {
		t.Fatal("no event was captured by processFn")
	}
	if evt.StepName != "step-1" {
		t.Errorf("StepName = %q, want step-1", evt.StepName)
	}
	if evt.SessionID != "sess-integration-1" {
		t.Errorf("SessionID = %q, want sess-integration-1", evt.SessionID)
	}
	t.Logf("StepName  = %q", evt.StepName)
	t.Logf("SessionID = %q", evt.SessionID)
}

// ── TestAgentSessionIntegration ───────────────────────────────────────────────
// Sends 4 requests sharing one session ID, closes the session explicitly, then
// verifies the session is persisted to ClickHouse with correct aggregates.

func TestAgentSessionIntegration(t *testing.T) {
	const sessionID = "test-session-001"

	rdb := redisClient(t)
	ctx := context.Background()

	// Clean up Redis session keys on exit (also deleted by CloseSession on success).
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(),
			"session:"+sessionID+":meta",
			"session:"+sessionID+":traces",
		).Err()
	})

	mock := mockProvider()
	t.Cleanup(mock.Close)

	cfg := &config.Config{
		Port:                   "0",
		RedisURL:               os.Getenv("REDIS_URL"),
		DatabaseURL:            "postgres://localhost:5432/observatory",
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       2,
		ProviderTimeoutSeconds: 10,
	}
	if cfg.RedisURL == "" {
		cfg.RedisURL = "redis://localhost:6379"
	}

	logger := zap.NewNop()
	engine := attribution.New(rdb, logger)
	acc := sessions.New(rdb, chWriter, logger)

	processFn := func(ctx context.Context, event events.RequestEvent) error {
		costRecord, err := engine.Process(ctx, event)
		if event.SessionID != "" {
			record := storage.TraceRecord{
				TraceID:      event.RequestID,
				RequestID:    event.RequestID,
				UserID:       event.UserID,
				SessionID:    event.SessionID,
				FeatureName:  event.FeatureName,
				AgentStep:    event.AgentStep,
				StepName:     event.StepName,
				ParentStepID: event.ParentStepID,
				ToolName:     event.ToolName,
				Provider:     event.Provider,
				Model:        event.Model,
				InputTokens:  event.InputTokens,
				OutputTokens: event.OutputTokens,
				CostUSD:      costRecord.CostUSD,
				LatencyMs:    event.LatencyMs,
				StatusCode:   event.StatusCode,
				Timestamp:    event.Timestamp,
			}
			if addErr := acc.AddTrace(ctx, event.SessionID, record); addErr != nil {
				t.Logf("AddTrace warning: %v", addErr)
			}
		}
		return err
	}

	emitter := events.New(cfg.AsyncWorkerCount*10, cfg.AsyncWorkerCount, logger, processFn)
	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	t.Cleanup(func() { cancelEmitter(); emitter.Stop() })
	emitter.Start(emitterCtx)

	h := proxy.New(cfg, emitter, logger)
	h.SetProviderURL("openai", mock.URL)
	gatewaySrv := httptest.NewServer(h)
	t.Cleanup(gatewaySrv.Close)

	// ── Send 4 sequential steps ───────────────────────────────────────────────
	steps := []string{
		"step-1-planner",
		"step-2-researcher",
		"step-3-synthesizer",
		"step-4-reviewer",
	}
	for _, step := range steps {
		sendRequest(t, gatewaySrv, sessionID, step)
	}

	// Allow the async pipeline to finish writing all 4 traces to the session buffer.
	time.Sleep(1 * time.Second)

	// ── Close session and flush to ClickHouse ─────────────────────────────────
	summary, err := acc.CloseSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	if summary.StepCount < 4 {
		t.Errorf("summary.StepCount = %d, want >= 4", summary.StepCount)
	}
	if summary.TotalCost <= 0 {
		t.Errorf("summary.TotalCost = %f, want > 0", summary.TotalCost)
	}
	t.Logf("summary: steps=%d cost=$%.8f tokens=%d",
		summary.StepCount, summary.TotalCost, summary.TotalTokens)

	// ── Verify session is readable from ClickHouse ────────────────────────────
	// (equivalent to querying GET /sessions on the live gateway)
	recs, err := chWriter.QueryRecentSessions(ctx, 50)
	if err != nil {
		t.Fatalf("QueryRecentSessions: %v", err)
	}

	sess := findSession(recs, sessionID)
	if sess == nil {
		t.Fatalf("session %q not found in ClickHouse sessions list (got %d records)", sessionID, len(recs))
	}

	if sess.StepCount < 4 {
		t.Errorf("ClickHouse step_count = %d, want >= 4", sess.StepCount)
	}
	if sess.TotalCost <= 0 {
		t.Errorf("ClickHouse total_cost = %f, want > 0", sess.TotalCost)
	}
	if sess.Status != "completed" {
		t.Errorf("ClickHouse status = %q, want completed", sess.Status)
	}
	t.Logf("ClickHouse session: steps=%d cost=$%.8f status=%s",
		sess.StepCount, sess.TotalCost, sess.Status)
}

// ── TestSessionReaping ────────────────────────────────────────────────────────
// Verifies the background reaper closes idle sessions and persists them to
// ClickHouse once sessionTimeout elapses.

func TestSessionReaping(t *testing.T) {
	const sessionID = "reap-test"

	rdb := redisClient(t)
	ctx := context.Background()

	// Clean up Redis session keys on exit.
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(),
			"session:"+sessionID+":meta",
			"session:"+sessionID+":traces",
		).Err()
	})

	mock := mockProvider()
	t.Cleanup(mock.Close)

	cfg := &config.Config{
		Port:                   "0",
		RedisURL:               os.Getenv("REDIS_URL"),
		DatabaseURL:            "postgres://localhost:5432/observatory",
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       2,
		ProviderTimeoutSeconds: 10,
	}
	if cfg.RedisURL == "" {
		cfg.RedisURL = "redis://localhost:6379"
	}

	logger := zap.NewNop()
	engine := attribution.New(rdb, logger)
	acc := sessions.New(rdb, chWriter, logger)
	acc.SetSessionTimeout(50 * time.Millisecond)
	acc.SetReaperInterval(30 * time.Millisecond)

	processFn := func(ctx context.Context, event events.RequestEvent) error {
		costRecord, err := engine.Process(ctx, event)
		if event.SessionID != "" {
			record := storage.TraceRecord{
				TraceID:      event.RequestID,
				RequestID:    event.RequestID,
				UserID:       event.UserID,
				SessionID:    event.SessionID,
				FeatureName:  event.FeatureName,
				AgentStep:    event.AgentStep,
				StepName:     event.StepName,
				ParentStepID: event.ParentStepID,
				ToolName:     event.ToolName,
				Provider:     event.Provider,
				Model:        event.Model,
				InputTokens:  event.InputTokens,
				OutputTokens: event.OutputTokens,
				CostUSD:      costRecord.CostUSD,
				LatencyMs:    event.LatencyMs,
				StatusCode:   event.StatusCode,
				Timestamp:    event.Timestamp,
			}
			if addErr := acc.AddTrace(ctx, event.SessionID, record); addErr != nil {
				t.Logf("AddTrace warning: %v", addErr)
			}
		}
		return err
	}

	emitter := events.New(cfg.AsyncWorkerCount*10, cfg.AsyncWorkerCount, logger, processFn)
	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	t.Cleanup(func() { cancelEmitter(); emitter.Stop() })
	emitter.Start(emitterCtx)

	h := proxy.New(cfg, emitter, logger)
	h.SetProviderURL("openai", mock.URL)
	gatewaySrv := httptest.NewServer(h)
	t.Cleanup(gatewaySrv.Close)

	// ── Send 2 requests ───────────────────────────────────────────────────────
	sendRequest(t, gatewaySrv, sessionID, "step-1")
	sendRequest(t, gatewaySrv, sessionID, "step-2")

	// Allow the async pipeline to finish writing both traces.
	time.Sleep(200 * time.Millisecond)

	// ── Start reaper and wait for it to close the idle session ────────────────
	reaperCtx, cancelReaper := context.WithCancel(context.Background())
	t.Cleanup(cancelReaper)
	acc.StartReaper(reaperCtx)

	// Session expires after 50ms; reaper ticks every 30ms.
	// Two ticks guarantee the session is reaped even if the first tick fires
	// just before the timeout elapses.
	time.Sleep(250 * time.Millisecond)

	// ── Verify session appears in ClickHouse as completed ─────────────────────
	recs, err := chWriter.QueryRecentSessions(ctx, 50)
	if err != nil {
		t.Fatalf("QueryRecentSessions: %v", err)
	}

	sess := findSession(recs, sessionID)
	if sess == nil {
		t.Fatalf("session %q not found in ClickHouse after reaping (got %d records)", sessionID, len(recs))
	}

	if sess.Status != "completed" {
		t.Errorf("status = %q, want completed", sess.Status)
	}
	if sess.StepCount < 2 {
		t.Errorf("step_count = %d, want >= 2", sess.StepCount)
	}
	if sess.TotalCost <= 0 {
		t.Errorf("total_cost = %f, want > 0", sess.TotalCost)
	}
	t.Logf("reaped session: steps=%d cost=$%.8f status=%s",
		sess.StepCount, sess.TotalCost, sess.Status)
}

// ── TestHallucinationFlagging ─────────────────────────────────────────────────
// Sends a request through the full pipeline with a mock scorer configured to
// return high hallucination (0.85) / low grounding (0.20), then verifies:
//   - The trace in ClickHouse has risk_level "high" and ShouldWarn true
//   - GET /warnings (via Redis) returns the warning with the correct request ID
//   - The warnings:daily counter in Redis was incremented

func TestHallucinationFlagging(t *testing.T) {
	const reqID = "flagging-req-001"

	// 1. Mock scorer: always returns high hallucination / low grounding.
	scorer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hallucination_score":0.85,"factual_consistency_score":0.20,"overall_quality_score":0.30}`))
	}))
	t.Cleanup(scorer.Close)

	rdb := redisClient(t)
	ctx := context.Background()

	today := time.Now().UTC().Format("2006-01-02")
	warnCountKey := fmt.Sprintf("warnings:daily:%s", today)

	// Record list length before the test so we can detect our new entry even
	// if the list already has entries from earlier runs.
	initialLen, _ := rdb.LLen(ctx, "warnings:high:list").Result()

	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), "flag:"+reqID).Err()
		// Trim the one entry we pushed rather than wiping the whole list.
		rdb.LTrim(context.Background(), "warnings:high:list", 0, initialLen-1)
	})

	mock := mockProvider()
	t.Cleanup(mock.Close)

	cfg := &config.Config{
		Port:                   "0",
		RedisURL:               os.Getenv("REDIS_URL"),
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       2,
		ProviderTimeoutSeconds: 10,
	}
	if cfg.RedisURL == "" {
		cfg.RedisURL = "redis://localhost:6379"
	}

	logger := zap.NewNop()
	flgr := flagging.New(scorer.URL, logger)
	const dailyKeyTTL = 90 * 24 * time.Hour

	processFn := func(ctx context.Context, event events.RequestEvent) error {
		if event.StatusCode != http.StatusOK {
			return nil
		}

		flagCtx, flagCancel := context.WithTimeout(ctx, 10*time.Second)
		riskFlag := flgr.Evaluate(flagCtx, event.RequestID, event.SessionID, event.Prompt, event.Response, 0.0, "")
		flagCancel()

		// Mirror exactly what the real processFn does.
		if riskFlag.ShouldWarn {
			day := event.Timestamp.UTC().Format("2006-01-02")
			key := fmt.Sprintf("warnings:daily:%s", day)
			if err := rdb.Incr(ctx, key).Err(); err == nil {
				rdb.Expire(ctx, key, dailyKeyTTL)
			}
		}
		if riskFlag.RiskLevel == "high" {
			item, _ := json.Marshal(riskFlag)
			rdb.LPush(ctx, "warnings:high:list", string(item))
			rdb.LTrim(ctx, "warnings:high:list", 0, 99)
		}

		record := storage.TraceRecord{
			TraceID:           event.RequestID,
			RequestID:         event.RequestID,
			UserID:            event.UserID,
			SessionID:         event.SessionID,
			FeatureName:       event.FeatureName,
			Provider:          event.Provider,
			Model:             event.Model,
			InputTokens:       event.InputTokens,
			OutputTokens:      event.OutputTokens,
			LatencyMs:         event.LatencyMs,
			StatusCode:        event.StatusCode,
			Timestamp:         event.Timestamp,
			HallucinationRisk: riskFlag.HallucinationRisk,
			GroundingScore:    riskFlag.GroundingScore,
			RiskLevel:         riskFlag.RiskLevel,
			ShouldWarn:        riskFlag.ShouldWarn,
		}
		return chWriter.Write(ctx, record)
	}

	emitter := events.New(cfg.AsyncWorkerCount*10, cfg.AsyncWorkerCount, logger, processFn)
	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	t.Cleanup(func() { cancelEmitter(); emitter.Stop() })
	emitter.Start(emitterCtx)

	h := proxy.New(cfg, emitter, logger)
	h.SetProviderURL("openai", mock.URL)
	gatewaySrv := httptest.NewServer(h)
	t.Cleanup(gatewaySrv.Close)

	// 2. Send a request with a known X-Request-ID so we can look it up later.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"What is the capital of France?"}]}`
	req, err := http.NewRequest(http.MethodPost,
		gatewaySrv.URL+"/v1/chat/completions",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "sk-test123")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", reqID)
	req.Header.Set("X-User-ID", "flagging-user")
	req.Header.Set("X-Feature-Name", "flagging-feature")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gateway status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Ajah-Request-ID"); got != reqID {
		t.Errorf("X-Ajah-Request-ID = %q, want %q", got, reqID)
	}

	// 3. Wait for async processing.
	time.Sleep(1 * time.Second)

	// ── Assert 1: ClickHouse trace has risk_level "high" ─────────────────────
	traces, err := chWriter.QueryRecent(ctx, 100)
	if err != nil {
		t.Fatalf("QueryRecent: %v", err)
	}
	var trace *storage.TraceRecord
	for i := range traces {
		if traces[i].RequestID == reqID {
			trace = &traces[i]
			break
		}
	}
	if trace == nil {
		t.Fatalf("trace with request_id %q not found in ClickHouse (%d records returned)", reqID, len(traces))
	}
	if trace.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", trace.RiskLevel)
	}
	if !trace.ShouldWarn {
		t.Error("ShouldWarn = false, want true")
	}
	if trace.HallucinationRisk < 0.80 {
		t.Errorf("HallucinationRisk = %.4f, want >= 0.80", trace.HallucinationRisk)
	}
	if trace.GroundingScore > 0.25 {
		t.Errorf("GroundingScore = %.4f, want <= 0.25", trace.GroundingScore)
	}
	t.Logf("ClickHouse trace: risk_level=%s hallucination=%.2f grounding=%.2f should_warn=%v",
		trace.RiskLevel, trace.HallucinationRisk, trace.GroundingScore, trace.ShouldWarn)

	// ── Assert 2: GET /warnings — list has the new entry with our request ID ──
	items, err := rdb.LRange(ctx, "warnings:high:list", 0, 99).Result()
	if err != nil {
		t.Fatalf("read warnings:high:list: %v", err)
	}
	if int64(len(items)) <= initialLen {
		t.Errorf("warnings:high:list length = %d, want > %d (no new entry added)", len(items), initialLen)
	}
	found := false
	for _, item := range items {
		if strings.Contains(item, reqID) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("request_id %q not found in warnings:high:list", reqID)
	}
	t.Logf("warnings:high:list: %d entries, request %q present: %v", len(items), reqID, found)

	// ── Assert 3: warnings:daily counter incremented ──────────────────────────
	count, err := rdb.Get(ctx, warnCountKey).Int64()
	if err != nil {
		t.Fatalf("read %q: %v", warnCountKey, err)
	}
	if count < 1 {
		t.Errorf("warnings:daily:%s = %d, want >= 1", today, count)
	}
	t.Logf("warnings:daily:%s = %d", today, count)
}

// ── TestRAGVerification ───────────────────────────────────────────────────────
// Sends a request through the full pipeline with a mock scorer configured to
// return a "contradicted" RAG verdict. Verifies:
//   - The scorer received source_context in the scoring payload
//   - The trace in ClickHouse has rag_verdict="contradicted" and rag_contradiction_score=0.75
//   - RiskLevel is "high" due to the contradiction override in the flagger
//   - The warning appears in Redis warnings:high:list

func TestRAGVerification(t *testing.T) {
	const reqID = "rag-verification-req-001"

	// Track whether any scorer call carried a non-empty source_context.
	var gotSourceContextMu sync.Mutex
	var gotSourceContext bool

	scorer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var pl struct {
			SourceContext string `json:"source_context"`
		}
		_ = json.Unmarshal(body, &pl)
		if pl.SourceContext != "" {
			gotSourceContextMu.Lock()
			gotSourceContext = true
			gotSourceContextMu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hallucination_score":0.15,"factual_consistency_score":0.85,"toxicity_score":0.001,"overall_quality_score":0.88,"flags":[],"processing_ms":450,"rag_verdict":"contradicted","rag_grounding_score":0.25,"rag_contradiction_score":0.75,"rag_supported_claims":[],"rag_unsupported_claims":["Claim A"],"rag_contradicted_claims":["Claim B contradicts document"]}`))
	}))
	t.Cleanup(scorer.Close)

	rdb := redisClient(t)
	ctx := context.Background()

	today := time.Now().UTC().Format("2006-01-02")
	warnCountKey := fmt.Sprintf("warnings:daily:%s", today)
	initialLen, _ := rdb.LLen(ctx, "warnings:high:list").Result()

	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), "flag:"+reqID).Err()
		rdb.LTrim(context.Background(), "warnings:high:list", 0, initialLen-1)
	})

	mock := mockProvider()
	t.Cleanup(mock.Close)

	cfg := &config.Config{
		Port:                   "0",
		RedisURL:               os.Getenv("REDIS_URL"),
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       2,
		ProviderTimeoutSeconds: 10,
	}
	if cfg.RedisURL == "" {
		cfg.RedisURL = "redis://localhost:6379"
	}

	logger := zap.NewNop()
	flgr := flagging.New(scorer.URL, logger)
	scorerClient := &http.Client{Timeout: 10 * time.Second}
	const dailyKeyTTL = 90 * 24 * time.Hour

	// Local mirror of main.go scorerOutcome — only the fields we need.
	type ragOutcome struct {
		OverallQualityScore   float64 `json:"overall_quality_score"`
		RAGVerdict            string  `json:"rag_verdict"`
		RAGGroundingScore     float64 `json:"rag_grounding_score"`
		RAGContradictionScore float64 `json:"rag_contradiction_score"`
	}

	processFn := func(ctx context.Context, event events.RequestEvent) error {
		if event.StatusCode != http.StatusOK {
			return nil
		}

		// Call scorer explicitly to obtain full RAG outcome (mirrors main.go callScorer).
		var outcome ragOutcome
		if payloadBytes, err := json.Marshal(map[string]string{
			"request_id":     event.RequestID,
			"prompt":         event.Prompt,
			"response":       event.Response,
			"model":          event.Model,
			"feature_name":   event.FeatureName,
			"source_context": event.SourceContext,
		}); err == nil {
			if req, err := http.NewRequestWithContext(ctx, http.MethodPost, scorer.URL+"/score", bytes.NewReader(payloadBytes)); err == nil {
				req.Header.Set("Content-Type", "application/json")
				if resp, err := scorerClient.Do(req); err == nil {
					raw, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					_ = json.Unmarshal(raw, &outcome)
				}
			}
		}

		flagCtx, flagCancel := context.WithTimeout(ctx, 10*time.Second)
		riskFlag := flgr.Evaluate(flagCtx, event.RequestID, event.SessionID,
			event.Prompt, event.Response, outcome.OverallQualityScore, outcome.RAGVerdict)
		flagCancel()

		if riskFlag.ShouldWarn {
			day := event.Timestamp.UTC().Format("2006-01-02")
			key := fmt.Sprintf("warnings:daily:%s", day)
			if err := rdb.Incr(ctx, key).Err(); err == nil {
				rdb.Expire(ctx, key, dailyKeyTTL)
			}
		}
		if riskFlag.RiskLevel == "high" {
			item, _ := json.Marshal(riskFlag)
			rdb.LPush(ctx, "warnings:high:list", string(item))
			rdb.LTrim(ctx, "warnings:high:list", 0, 99)
		}

		record := storage.TraceRecord{
			TraceID:               event.RequestID,
			RequestID:             event.RequestID,
			UserID:                event.UserID,
			SessionID:             event.SessionID,
			FeatureName:           event.FeatureName,
			Provider:              event.Provider,
			Model:                 event.Model,
			InputTokens:           event.InputTokens,
			OutputTokens:          event.OutputTokens,
			LatencyMs:             event.LatencyMs,
			StatusCode:            event.StatusCode,
			Timestamp:             event.Timestamp,
			HallucinationRisk:     riskFlag.HallucinationRisk,
			GroundingScore:        riskFlag.GroundingScore,
			RiskLevel:             riskFlag.RiskLevel,
			ShouldWarn:            riskFlag.ShouldWarn,
			RAGVerdict:            outcome.RAGVerdict,
			RAGGroundingScore:     outcome.RAGGroundingScore,
			RAGContradictionScore: outcome.RAGContradictionScore,
		}
		return chWriter.Write(ctx, record)
	}

	emitter := events.New(cfg.AsyncWorkerCount*10, cfg.AsyncWorkerCount, logger, processFn)
	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	t.Cleanup(func() { cancelEmitter(); emitter.Stop() })
	emitter.Start(emitterCtx)

	h := proxy.New(cfg, emitter, logger)
	h.SetProviderURL("openai", mock.URL)
	gatewaySrv := httptest.NewServer(h)
	t.Cleanup(gatewaySrv.Close)

	// 2. Send a request with X-Source-Context: base64("The sky is blue and clouds are white").
	sourceCtxValue := base64.StdEncoding.EncodeToString([]byte("The sky is blue and clouds are white"))
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"What color is the sky?"}]}`
	req, err := http.NewRequest(http.MethodPost,
		gatewaySrv.URL+"/v1/chat/completions",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "sk-test123")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", reqID)
	req.Header.Set("X-User-ID", "rag-user")
	req.Header.Set("X-Feature-Name", "rag-feature")
	req.Header.Set("X-Source-Context", sourceCtxValue)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gateway status = %d, want 200", resp.StatusCode)
	}

	// 3. Wait for async pipeline to complete.
	time.Sleep(1 * time.Second)

	// ── Assert: scorer received source_context in at least one call ───────────
	gotSourceContextMu.Lock()
	receivedCtx := gotSourceContext
	gotSourceContextMu.Unlock()
	if !receivedCtx {
		t.Error("scorer never received a non-empty source_context in the payload")
	}

	// ── Assert: ClickHouse trace has rag_verdict="contradicted" ───────────────
	traces, err := chWriter.QueryRecent(ctx, 100)
	if err != nil {
		t.Fatalf("QueryRecent: %v", err)
	}
	var trace *storage.TraceRecord
	for i := range traces {
		if traces[i].RequestID == reqID {
			trace = &traces[i]
			break
		}
	}
	if trace == nil {
		t.Fatalf("trace with request_id %q not found in ClickHouse (%d records)", reqID, len(traces))
	}
	if trace.RAGVerdict != "contradicted" {
		t.Errorf("RAGVerdict = %q, want contradicted", trace.RAGVerdict)
	}
	if trace.RAGContradictionScore != 0.75 {
		t.Errorf("RAGContradictionScore = %.4f, want 0.75", trace.RAGContradictionScore)
	}
	// hallucination=0.15, grounding=0.85 → normally "low"; contradiction override → "high"
	if trace.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high (contradiction override)", trace.RiskLevel)
	}
	if !trace.ShouldWarn {
		t.Error("ShouldWarn = false, want true")
	}
	t.Logf("ClickHouse trace: rag_verdict=%s rag_contradiction=%.2f risk_level=%s should_warn=%v",
		trace.RAGVerdict, trace.RAGContradictionScore, trace.RiskLevel, trace.ShouldWarn)

	// ── Assert: warning appears in Redis warnings:high:list ───────────────────
	items, err := rdb.LRange(ctx, "warnings:high:list", 0, 99).Result()
	if err != nil {
		t.Fatalf("read warnings:high:list: %v", err)
	}
	if int64(len(items)) <= initialLen {
		t.Errorf("warnings:high:list length = %d, want > %d (no new entry added)", len(items), initialLen)
	}
	found := false
	for _, item := range items {
		if strings.Contains(item, reqID) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("request_id %q not found in warnings:high:list", reqID)
	}
	t.Logf("warnings:high:list: %d entries, request %q present: %v", len(items), reqID, found)

	// ── Assert: warnings:daily counter incremented ────────────────────────────
	count, err := rdb.Get(ctx, warnCountKey).Int64()
	if err != nil {
		t.Fatalf("read %q: %v", warnCountKey, err)
	}
	if count < 1 {
		t.Errorf("warnings:daily:%s = %d, want >= 1", today, count)
	}
	t.Logf("warnings:daily:%s = %d", today, count)
}
