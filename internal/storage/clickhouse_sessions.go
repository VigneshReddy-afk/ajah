package storage

import (
	"context"
	"fmt"
	"time"
)

// SessionRecord mirrors sessions.SessionMeta and is written to the agent_sessions table.
type SessionRecord struct {
	SessionID   string
	StartTime   time.Time
	LastSeen    time.Time
	TotalCost   float64
	TotalTokens int64
	StepCount   int32
	AvgQuality  float64
	FeatureName string
	UserID      string
	Status      string
}

const createSessionTableSQL = `
CREATE TABLE IF NOT EXISTS agent_sessions (
    session_id    String,
    start_time    DateTime,
    last_seen     DateTime,
    total_cost    Float64,
    total_tokens  Int64,
    step_count    Int32,
    avg_quality   Float64,
    feature_name  String,
    user_id       String,
    status        String
) ENGINE = MergeTree()
ORDER BY (start_time, user_id, session_id)
`

// CreateSessionTable creates the agent_sessions table if it does not already exist.
func (w *Writer) CreateSessionTable(ctx context.Context) error {
	if err := w.conn.Exec(ctx, createSessionTableSQL); err != nil {
		return fmt.Errorf("create agent_sessions table: %w", err)
	}
	return nil
}

// WriteSession inserts a single SessionRecord into agent_sessions.
func (w *Writer) WriteSession(ctx context.Context, rec SessionRecord) error {
	batch, err := w.conn.PrepareBatch(ctx, `INSERT INTO agent_sessions (
		session_id, start_time, last_seen, total_cost, total_tokens,
		step_count, avg_quality, feature_name, user_id, status
	)`)
	if err != nil {
		return fmt.Errorf("prepare session batch: %w", err)
	}

	if err := batch.Append(
		rec.SessionID,
		rec.StartTime.UTC().Truncate(time.Second),
		rec.LastSeen.UTC().Truncate(time.Second),
		rec.TotalCost,
		rec.TotalTokens,
		rec.StepCount,
		rec.AvgQuality,
		rec.FeatureName,
		rec.UserID,
		rec.Status,
	); err != nil {
		return fmt.Errorf("append session record: %w", err)
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("send session batch: %w", err)
	}
	return nil
}

// QueryRecentSessions returns the most recent limit sessions from agent_sessions,
// ordered by start_time descending.
func (w *Writer) QueryRecentSessions(ctx context.Context, limit int) ([]SessionRecord, error) {
	rows, err := w.conn.Query(ctx, fmt.Sprintf(`
		SELECT session_id, start_time, last_seen, total_cost, total_tokens,
		       step_count, avg_quality, feature_name, user_id, status
		FROM agent_sessions
		ORDER BY start_time DESC
		LIMIT %d`, limit))
	if err != nil {
		return nil, fmt.Errorf("query recent sessions: %w", err)
	}
	defer rows.Close()

	var records []SessionRecord
	for rows.Next() {
		var r SessionRecord
		if err := rows.Scan(
			&r.SessionID, &r.StartTime, &r.LastSeen, &r.TotalCost,
			&r.TotalTokens, &r.StepCount, &r.AvgQuality,
			&r.FeatureName, &r.UserID, &r.Status,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	if records == nil {
		records = []SessionRecord{}
	}
	return records, nil
}

// QuerySessionByID returns the session record for sessionID, or nil if not found.
func (w *Writer) QuerySessionByID(ctx context.Context, sessionID string) (*SessionRecord, error) {
	rows, err := w.conn.Query(ctx, `
		SELECT session_id, start_time, last_seen, total_cost, total_tokens,
		       step_count, avg_quality, feature_name, user_id, status
		FROM agent_sessions
		WHERE session_id = ?
		LIMIT 1`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session by id: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}
	var r SessionRecord
	if err := rows.Scan(
		&r.SessionID, &r.StartTime, &r.LastSeen, &r.TotalCost,
		&r.TotalTokens, &r.StepCount, &r.AvgQuality,
		&r.FeatureName, &r.UserID, &r.Status,
	); err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return &r, nil
}

// QueryTracesBySession returns all traces for sessionID ordered by timestamp ascending.
func (w *Writer) QueryTracesBySession(ctx context.Context, sessionID string) ([]TraceRecord, error) {
	rows, err := w.conn.Query(ctx, `
		SELECT trace_id, request_id, user_id, session_id, feature_name,
		       agent_step, provider, model, input_tokens, output_tokens,
		       cost_usd, latency_ms, status_code, masked_prompt,
		       was_pii_masked, quality_score, timestamp,
		       parent_step_id, step_name, tool_name
		FROM traces
		WHERE session_id = ?
		ORDER BY timestamp ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query traces by session: %w", err)
	}
	defer rows.Close()

	var records []TraceRecord
	for rows.Next() {
		var r TraceRecord
		var inputTokens, outputTokens, statusCode int32
		var wasPIIMasked uint8
		if err := rows.Scan(
			&r.TraceID, &r.RequestID, &r.UserID, &r.SessionID, &r.FeatureName,
			&r.AgentStep, &r.Provider, &r.Model, &inputTokens, &outputTokens,
			&r.CostUSD, &r.LatencyMs, &statusCode, &r.MaskedPrompt,
			&wasPIIMasked, &r.QualityScore, &r.Timestamp,
			&r.ParentStepID, &r.StepName, &r.ToolName,
		); err != nil {
			return nil, fmt.Errorf("scan trace: %w", err)
		}
		r.InputTokens = int(inputTokens)
		r.OutputTokens = int(outputTokens)
		r.StatusCode = int(statusCode)
		r.WasPIIMasked = wasPIIMasked == 1
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate traces: %w", err)
	}
	if records == nil {
		records = []TraceRecord{}
	}
	return records, nil
}
