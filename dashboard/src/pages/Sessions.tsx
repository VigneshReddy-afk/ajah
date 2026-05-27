import type { CSSProperties } from 'react'
import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { TraceRecord, SessionRecord, SessionDetailRecord, SessionsResponse } from '../api/types'

// ── Tree types ─────────────────────────────────────────────────────────────

interface StepNode {
  trace: TraceRecord
  children: StepNode[]
}

// ── Mock data ──────────────────────────────────────────────────────────────

const _now = Date.now()

const MOCK_SESSIONS: SessionRecord[] = [
  {
    session_id: 'mock-sess-1',
    start_time: new Date(_now - 8 * 60_000).toISOString(),
    end_time:   new Date(_now - 6 * 60_000).toISOString(),
    total_cost: 0.002340, total_tokens: 1820, step_count: 4,
    avg_quality: 0.88, status: 'completed', feature_name: 'research', user_id: 'user_1',
  },
  {
    session_id: 'mock-sess-2',
    start_time: new Date(_now - 28 * 60_000).toISOString(),
    end_time:   new Date(_now - 26 * 60_000).toISOString(),
    total_cost: 0.000980, total_tokens: 740, step_count: 2,
    avg_quality: 0.45, status: 'completed', feature_name: 'classify', user_id: 'user_2',
  },
  {
    session_id: 'mock-sess-3',
    start_time: new Date(_now - 65 * 60_000).toISOString(),
    end_time:   new Date(_now - 61 * 60_000).toISOString(),
    total_cost: 0.004120, total_tokens: 3200, step_count: 5,
    avg_quality: 0.72, status: 'completed', feature_name: 'summarize', user_id: 'user_3',
  },
]

const MOCK_TRACES: TraceRecord[] = [
  {
    trace_id: 'mt1', request_id: 'r1', user_id: 'user_1', session_id: 'mock-sess-1',
    feature_name: 'research', agent_step: 'plan', provider: 'openai', model: 'gpt-4o',
    input_tokens: 320, output_tokens: 180, cost_usd: 0.000520, latency_ms: 1240,
    status_code: 200, masked_prompt: 'Plan the research steps', was_pii_masked: false,
    quality_score: 0.90, timestamp: new Date(_now - 8 * 60_000).toISOString(),
    parent_step_id: '', step_name: 'plan', tool_name: '',
  },
  {
    trace_id: 'mt2', request_id: 'r2', user_id: 'user_1', session_id: 'mock-sess-1',
    feature_name: 'research', agent_step: 'web_search', provider: 'openai', model: 'gpt-4o',
    input_tokens: 180, output_tokens: 90, cost_usd: 0.000180, latency_ms: 580,
    status_code: 200, masked_prompt: 'Search for recent AI papers', was_pii_masked: false,
    quality_score: 0.85, timestamp: new Date(_now - 7.8 * 60_000).toISOString(),
    parent_step_id: 'plan', step_name: 'web_search', tool_name: 'web_search',
  },
  {
    trace_id: 'mt3', request_id: 'r3', user_id: 'user_1', session_id: 'mock-sess-1',
    feature_name: 'research', agent_step: 'extract', provider: 'openai', model: 'gpt-4o',
    input_tokens: 420, output_tokens: 210, cost_usd: 0.000420, latency_ms: 890,
    status_code: 200, masked_prompt: 'Extract key findings', was_pii_masked: false,
    quality_score: 0.88, timestamp: new Date(_now - 7.5 * 60_000).toISOString(),
    parent_step_id: 'plan', step_name: 'extract', tool_name: '',
  },
  {
    trace_id: 'mt4', request_id: 'r4', user_id: 'user_1', session_id: 'mock-sess-1',
    feature_name: 'research', agent_step: 'respond', provider: 'openai', model: 'gpt-4o',
    input_tokens: 620, output_tokens: 400, cost_usd: 0.001220, latency_ms: 1450,
    status_code: 200, masked_prompt: 'Generate final research report', was_pii_masked: false,
    quality_score: 0.91, timestamp: new Date(_now - 7 * 60_000).toISOString(),
    parent_step_id: '', step_name: 'respond', tool_name: '',
  },
]

// ── Helpers ────────────────────────────────────────────────────────────────

function buildTree(traces: TraceRecord[]): StepNode[] {
  const byKey = new Map<string, StepNode>()
  for (const trace of traces) {
    byKey.set(trace.step_name || trace.trace_id, { trace, children: [] })
  }
  const roots: StepNode[] = []
  for (const trace of traces) {
    const myKey = trace.step_name || trace.trace_id
    const node = byKey.get(myKey)!
    if (!trace.parent_step_id || !byKey.has(trace.parent_step_id)) {
      roots.push(node)
    } else {
      byKey.get(trace.parent_step_id)!.children.push(node)
    }
  }
  return roots
}

function qualityColor(score: number): string {
  if (score === 0) return 'var(--color-text-tertiary)'
  if (score >= 0.8) return '#0F6E56'
  if (score >= 0.5) return '#854F0B'
  return '#A32D2D'
}

function stepBorderColor(score: number): string {
  if (score === 0) return 'var(--color-border-tertiary)'
  if (score >= 0.8) return '#0F6E56'
  if (score >= 0.7) return 'var(--color-border-tertiary)'
  if (score >= 0.5) return '#854F0B'
  return '#A32D2D'
}

function qualityStyleTable(score: number): CSSProperties {
  if (score === 0) return { color: 'var(--color-text-tertiary)' }
  if (score >= 0.8) return { color: '#0F6E56', fontWeight: 500 }
  if (score >= 0.5) return { color: '#854F0B', fontWeight: 500 }
  return { color: '#A32D2D', fontWeight: 500 }
}

function durationSecs(start: string, end: string): number {
  return Math.round((new Date(end).getTime() - new Date(start).getTime()) / 1000)
}

// ── Table styles ───────────────────────────────────────────────────────────

const thStyle: CSSProperties = {
  padding: '10px 14px',
  fontSize: 11,
  fontWeight: 500,
  color: 'var(--color-text-tertiary)',
  textTransform: 'uppercase',
  letterSpacing: '0.05em',
  textAlign: 'left',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
  whiteSpace: 'nowrap',
}

const tdStyle: CSSProperties = {
  padding: '11px 14px',
  fontSize: 13,
  color: 'var(--color-text-secondary)',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
  whiteSpace: 'nowrap',
}

// ── Step card ──────────────────────────────────────────────────────────────

function StepCard({ trace }: { trace: TraceRecord }) {
  const borderColor = stepBorderColor(trace.quality_score)
  return (
    <div style={{
      width: 172,
      background: 'var(--color-background-secondary)',
      border: `1.5px solid ${borderColor}`,
      borderRadius: 8,
      padding: '10px 12px',
      flexShrink: 0,
    }}>
      <div style={{
        fontSize: 12, fontWeight: 600,
        color: 'var(--color-text-primary)',
        marginBottom: 4,
        wordBreak: 'break-word',
        lineHeight: 1.3,
      }}>
        {trace.step_name || trace.agent_step || '(unnamed)'}
      </div>
      {trace.tool_name && (
        <div style={{ marginBottom: 6 }}>
          <span style={{
            fontSize: 10, fontWeight: 500,
            color: '#185FA5', background: 'rgba(24,95,165,0.10)',
            padding: '1px 5px', borderRadius: 3,
          }}>
            {trace.tool_name}
          </span>
        </div>
      )}
      <div style={{ fontSize: 11, color: 'var(--color-text-tertiary)', marginBottom: 2 }}>
        {trace.model}
      </div>
      <div style={{ fontSize: 11, fontVariantNumeric: 'tabular-nums', color: 'var(--color-text-secondary)', marginBottom: 1 }}>
        ${trace.cost_usd.toFixed(5)}
      </div>
      <div style={{ fontSize: 11, fontVariantNumeric: 'tabular-nums', color: 'var(--color-text-tertiary)', marginBottom: 6 }}>
        {trace.latency_ms}ms
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 5, flexWrap: 'wrap' }}>
        <span style={{ fontSize: 11, fontWeight: 500, color: qualityColor(trace.quality_score) }}>
          Q {trace.quality_score.toFixed(2)}
        </span>
        {trace.was_pii_masked && (
          <span style={{
            fontSize: 10, fontWeight: 500,
            color: '#A32D2D', background: 'rgba(163,45,45,0.12)',
            padding: '1px 5px', borderRadius: 3,
          }}>PII</span>
        )}
      </div>
    </div>
  )
}

// ── Arrow connectors ───────────────────────────────────────────────────────

function ArrowH() {
  return (
    <div style={{
      display: 'flex', alignItems: 'flex-start',
      paddingTop: 44,
      color: 'var(--color-text-tertiary)',
      fontSize: 16, userSelect: 'none',
      padding: '44px 6px 0',
    }}>→</div>
  )
}

function ArrowV() {
  return (
    <div style={{
      paddingLeft: 80,
      color: 'var(--color-text-tertiary)',
      fontSize: 14, userSelect: 'none',
      lineHeight: 1, padding: '3px 0 3px 80px',
    }}>↓</div>
  )
}

// ── Recursive step column ──────────────────────────────────────────────────

function StepColumn({ node }: { node: StepNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start' }}>
      <StepCard trace={node.trace} />
      {node.children.length > 0 && (
        <>
          <ArrowV />
          <div style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start' }}>
            {node.children.map((child, i) => (
              <Fragment key={child.trace.trace_id}>
                {i > 0 && <ArrowH />}
                <StepColumn node={child} />
              </Fragment>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

// ── Step tree ──────────────────────────────────────────────────────────────

function StepTree({ traces }: { traces: TraceRecord[] }) {
  const roots = buildTree(traces)
  if (roots.length === 0) {
    return (
      <div style={{ color: 'var(--color-text-tertiary)', fontSize: 13, fontStyle: 'italic' }}>
        No steps recorded for this session.
      </div>
    )
  }
  return (
    <div style={{ overflowX: 'auto', paddingBottom: 4 }}>
      <div style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start', minWidth: 'max-content' }}>
        {roots.map((root, i) => (
          <Fragment key={root.trace.trace_id}>
            {i > 0 && <ArrowH />}
            <StepColumn node={root} />
          </Fragment>
        ))}
      </div>
    </div>
  )
}

// ── Session detail (fetched on expand) ────────────────────────────────────

function SessionDetail({ sessionID, useMock }: { sessionID: string; useMock: boolean }) {
  const { data, isLoading, error } = useQuery<SessionDetailRecord>({
    queryKey: ['session', sessionID],
    queryFn: () => fetchJSON<SessionDetailRecord>(`/sessions/${sessionID}`),
    enabled: !useMock,
  })

  if (useMock) {
    return (
      <>
        <p style={{ margin: '0 0 12px', fontSize: 10, color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
          Step flow — demo data
        </p>
        <StepTree traces={MOCK_TRACES} />
      </>
    )
  }
  if (isLoading) return <div style={{ color: 'var(--color-text-tertiary)', fontSize: 13 }}>Loading steps…</div>
  if (error || !data) return <div style={{ color: '#A32D2D', fontSize: 13 }}>Failed to load session detail</div>

  return (
    <>
      <p style={{ margin: '0 0 12px', fontSize: 10, color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
        Step flow · {data.traces.length} trace{data.traces.length !== 1 ? 's' : ''}
      </p>
      <StepTree traces={data.traces} />
    </>
  )
}

// ── Page ───────────────────────────────────────────────────────────────────

export default function Sessions() {
  const [expanded, setExpanded] = useState<string | null>(null)

  const { data, isLoading, error } = useQuery<SessionsResponse>({
    queryKey: ['sessions'],
    queryFn: () => fetchJSON<SessionsResponse>('/sessions'),
  })

  if (isLoading) return <div style={{ padding: 24, color: 'var(--color-text-tertiary)', fontSize: 13 }}>Loading…</div>
  if (error)     return <div style={{ padding: 24, color: '#A32D2D', fontSize: 13 }}>Failed to load sessions</div>

  const sessions = data?.sessions ?? []
  const useMock = sessions.length === 0
  const rows = useMock ? MOCK_SESSIONS : sessions

  return (
    <div style={{ padding: 24 }}>
      <div style={{ marginBottom: 16 }}>
        <span style={{ fontSize: 11, color: 'var(--color-text-tertiary)' }}>
          {rows.length} session{rows.length !== 1 ? 's' : ''} — click row to expand step flow
        </span>
      </div>

      <div style={{
        background: 'var(--color-background-secondary)',
        border: '0.5px solid var(--color-border-tertiary)',
        borderRadius: 10,
        overflow: 'hidden',
      }}>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['Start time', 'User', 'Feature', 'Steps', 'Total cost', 'Avg quality', 'Duration', 'Status'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map(sess => {
                const isExpanded = expanded === sess.session_id
                const dur = durationSecs(sess.start_time, sess.end_time)
                return (
                  <Fragment key={sess.session_id}>
                    <tr
                      onClick={() => setExpanded(isExpanded ? null : sess.session_id)}
                      style={{
                        cursor: 'pointer',
                        transition: 'background 0.1s',
                        background: isExpanded ? 'var(--color-background-primary)' : 'transparent',
                      }}
                      onMouseEnter={e => (e.currentTarget.style.background = 'var(--color-background-primary)')}
                      onMouseLeave={e => (e.currentTarget.style.background = isExpanded ? 'var(--color-background-primary)' : 'transparent')}
                    >
                      <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12, color: 'var(--color-text-tertiary)' }}>
                        {format(new Date(sess.start_time), 'HH:mm:ss')}
                      </td>
                      <td style={tdStyle}>{sess.user_id || '—'}</td>
                      <td style={{ ...tdStyle, color: 'var(--color-text-primary)' }}>{sess.feature_name || '—'}</td>
                      <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums' }}>{sess.step_count}</td>
                      <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums' }}>${sess.total_cost.toFixed(6)}</td>
                      <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums', ...qualityStyleTable(sess.avg_quality) }}>
                        {sess.avg_quality.toFixed(2)}
                      </td>
                      <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums' }}>{dur}s</td>
                      <td style={tdStyle}>
                        <span style={{
                          fontSize: 11, fontWeight: 500,
                          color: '#0F6E56', background: 'rgba(15,110,86,0.12)',
                          padding: '2px 7px', borderRadius: 4,
                        }}>
                          {sess.status}
                        </span>
                      </td>
                    </tr>

                    {isExpanded && (
                      <tr>
                        <td colSpan={8} style={{
                          padding: '16px 18px 20px',
                          background: 'var(--color-background-primary)',
                          borderBottom: '0.5px solid var(--color-border-tertiary)',
                        }}>
                          <SessionDetail sessionID={sess.session_id} useMock={useMock} />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
