package events

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RequestEvent holds data captured for every proxied LLM request.
// Defined here so both the emitter and the proxy package can reference it
// without a circular import.
type RequestEvent struct {
	RequestID    string
	UserID       string
	SessionID    string
	FeatureName  string
	AgentStep    string // legacy field kept for attribution-engine retry-loop key
	StepName     string // canonical name for the agent step (from X-Agent-Step)
	ParentStepID string // from X-Parent-Step-ID; empty for root steps
	ToolName     string // from X-Tool-Name; set when an agent invokes a tool
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	LatencyMs    int64
	StatusCode   int
	Prompt       string
	Response     string
	Timestamp    time.Time
}

// ProcessFn is called by each worker goroutine for every dequeued event.
// Implementations plug in the attribution engine, PII masker, quality scorer, etc.
type ProcessFn func(ctx context.Context, event RequestEvent) error

// Emitter buffers RequestEvents in a channel and drains them through a pool
// of worker goroutines. Emit never blocks the caller.
type Emitter struct {
	ch      chan RequestEvent
	workers int
	logger  *zap.Logger
	process ProcessFn
	wg      sync.WaitGroup
}

// New creates an Emitter.
//   - bufferSize: capacity of the internal channel (recommend AsyncWorkerCount * 10).
//   - workers:    number of concurrent drain goroutines.
//   - process:    function called for each dequeued event.
func New(bufferSize, workers int, logger *zap.Logger, process ProcessFn) *Emitter {
	return &Emitter{
		ch:      make(chan RequestEvent, bufferSize),
		workers: workers,
		logger:  logger,
		process: process,
	}
}

// Emit sends event to the processing channel without blocking.
// If the channel is full the event is dropped and a warning is logged.
func (e *Emitter) Emit(event RequestEvent) {
	select {
	case e.ch <- event:
	default:
		e.logger.Warn("event channel full, dropping event",
			zap.String("request_id", event.RequestID),
			zap.String("provider", event.Provider),
		)
	}
}

// Start launches the worker goroutine pool. Workers run until ctx is cancelled,
// then drain any events remaining in the channel before exiting.
// Cancel ctx first, then call Stop to wait for a clean shutdown.
func (e *Emitter) Start(ctx context.Context) {
	for i := 0; i < e.workers; i++ {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.runWorker(ctx)
		}()
	}
}

// Stop waits for all workers to finish draining, with a hard 5-second timeout.
// ctx must already be cancelled before Stop is called, otherwise workers will
// not exit and Stop will always time out.
func (e *Emitter) Stop() {
	finished := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		e.logger.Info("emitter stopped cleanly")
	case <-time.After(5 * time.Second):
		e.logger.Warn("emitter stop timed out after 5s: some in-flight events may be lost")
	}
}

// runWorker is the per-goroutine event loop. It processes events from the
// channel and, once ctx is cancelled, drains any remaining buffered events
// using a fresh background context so process functions can still complete.
func (e *Emitter) runWorker(ctx context.Context) {
	for {
		select {
		case event := <-e.ch:
			e.callProcess(ctx, event)

		case <-ctx.Done():
			// Drain every event that is already in the buffer before exiting.
			// Use context.Background() so that process functions (DB writes,
			// HTTP calls) are not aborted by the cancelled context.
			for {
				select {
				case event := <-e.ch:
					e.callProcess(context.Background(), event)
				default:
					return
				}
			}
		}
	}
}

func (e *Emitter) callProcess(ctx context.Context, event RequestEvent) {
	if err := e.process(ctx, event); err != nil {
		e.logger.Error("event processing failed",
			zap.String("request_id", event.RequestID),
			zap.Error(err),
		)
	}
}
