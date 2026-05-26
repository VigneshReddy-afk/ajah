//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ajah/core/internal/attribution"
	"github.com/ajah/core/internal/config"
	"github.com/ajah/core/internal/events"
	"github.com/ajah/core/internal/proxy"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// TestGatewayIntegration exercises the full request path:
//
//	test client → gateway handler → mock OpenAI → async attribution → Redis
func TestGatewayIntegration(t *testing.T) {
	// ── Redis ────────────────────────────────────────────────────────────────
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

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis not available at %q: %v — run with a live Redis or set REDIS_URL", redisURL, err)
	}

	// Clean up the specific keys this test will write so reruns are idempotent.
	today := time.Now().UTC().Format("2006-01-02")
	userKey := fmt.Sprintf("cost:user:user_1:daily:%s", today)
	featureKey := fmt.Sprintf("cost:feature:chat:daily:%s", today)
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), userKey, featureKey).Err()
	})

	// ── Mock OpenAI provider ─────────────────────────────────────────────────
	mockOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			`{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
		))
	}))
	t.Cleanup(mockOpenAI.Close)

	// ── Full stack assembly ──────────────────────────────────────────────────
	cfg := &config.Config{
		Port:                   "0",
		RedisURL:               redisURL,
		DatabaseURL:            "postgres://localhost:5432/observatory",
		LogLevel:               "info",
		MaxRequestBodyBytes:    10 * 1024 * 1024,
		AsyncWorkerCount:       2,
		ProviderTimeoutSeconds: 10,
	}

	logger := zap.NewNop()

	engine := attribution.New(rdb, logger)

	processFn := func(ctx context.Context, event events.RequestEvent) error {
		_, err := engine.Process(ctx, event)
		return err
	}

	emitter := events.New(cfg.AsyncWorkerCount*10, cfg.AsyncWorkerCount, logger, processFn)
	emitterCtx, cancelEmitter := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelEmitter()
		emitter.Stop()
	})
	emitter.Start(emitterCtx)

	h := proxy.New(cfg, emitter, logger)
	h.SetProviderURL("openai", mockOpenAI.URL)

	gatewaySrv := httptest.NewServer(h)
	t.Cleanup(gatewaySrv.Close)

	// ── Send request ─────────────────────────────────────────────────────────
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest(
		http.MethodPost,
		gatewaySrv.URL+"/v1/chat/completions",
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "sk-test123")
	req.Header.Set("X-User-ID", "user_1")
	req.Header.Set("X-Feature-Name", "chat")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// ── Assertions ───────────────────────────────────────────────────────────
	if resp.StatusCode != http.StatusOK {
		t.Errorf("response status = %d, want 200", resp.StatusCode)
	}

	// Allow the async attribution pipeline to process the event.
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
}
