import { useState } from 'react'
import type { CSSProperties } from 'react'
import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { CostMetrics, WarningItem, WarningsResponse } from '../api/types'

// ── Constants ───────────────────────────────────────────────────────────────

const HIGH_COLOR = '#A32D2D'
const HIGH_BG    = 'rgba(163,45,45,0.12)'
const MED_COLOR  = '#854F0B'
const MED_BG     = 'rgba(133,79,11,0.12)'

const card: CSSProperties = {
  background: 'var(--color-background-secondary)',
  border: '0.5px solid var(--color-border-tertiary)',
  borderRadius: 10,
  padding: 20,
}

const th: CSSProperties = {
  padding: '9px 12px',
  fontSize: 'var(--sz-xs)',
  fontWeight: 500,
  color: 'var(--color-text-tertiary)',
  textTransform: 'uppercase',
  letterSpacing: '0.06em',
  textAlign: 'left',
  whiteSpace: 'nowrap',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
  background: 'var(--color-background-secondary)',
}

const td: CSSProperties = {
  padding: '10px 12px',
  fontSize: 'var(--sz-sm)',
  color: 'var(--color-text-secondary)',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
  verticalAlign: 'top',
}

// ── Sub-components ──────────────────────────────────────────────────────────

function MetricCard({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <div style={card}>
      <p style={{
        fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)',
        textTransform: 'uppercase', letterSpacing: '0.06em', margin: 0,
      }}>
        {label}
      </p>
      <p style={{
        fontSize: 'var(--sz-stat)', fontWeight: 600, margin: '10px 0 0',
        color, fontVariantNumeric: 'tabular-nums',
      }}>
        {value}
      </p>
    </div>
  )
}

function RiskBadge({ level }: { level: string }) {
  const [color, bg] =
    level === 'high'   ? [HIGH_COLOR, HIGH_BG] :
    level === 'medium' ? [MED_COLOR, MED_BG] :
                         ['var(--color-text-secondary)', 'var(--color-background-primary)']
  return (
    <span style={{
      display: 'inline-block',
      fontSize: 'var(--sz-xs)', fontWeight: 600,
      textTransform: 'uppercase', letterSpacing: '0.05em',
      color, background: bg, borderRadius: 4, padding: '2px 8px',
    }}>
      {level}
    </span>
  )
}

function ScoreCell({ value, higherIsGood = false }: { value: number; higherIsGood?: boolean }) {
  const bad   = higherIsGood ? value < 0.3 : value > 0.7
  const warn  = higherIsGood ? value < 0.5 : value >= 0.4
  const color = bad ? HIGH_COLOR : warn ? MED_COLOR : '#1D9E75'
  return (
    <span style={{ color, fontVariantNumeric: 'tabular-nums', fontSize: 'var(--sz-sm)' }}>
      {value.toFixed(2)}
    </span>
  )
}

function ReasonsCell({ reasons }: { reasons: string[] }) {
  const [expanded, setExpanded] = useState(false)

  if (!reasons?.length) {
    return <span style={{ color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-sm)' }}>—</span>
  }

  if (expanded || reasons.length === 1) {
    return (
      <div style={{ fontSize: 'var(--sz-sm)', color: 'var(--color-text-secondary)' }}>
        {reasons.map((r, i) => <div key={i} style={{ marginBottom: i < reasons.length - 1 ? 3 : 0 }}>{r}</div>)}
        {reasons.length > 1 && (
          <button
            onClick={() => setExpanded(false)}
            style={{
              background: 'none', border: 'none', cursor: 'pointer',
              padding: '4px 0 0', fontSize: 'var(--sz-xs)', color: '#185FA5', display: 'block',
            }}
          >
            show less
          </button>
        )}
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', alignItems: 'flex-start', gap: 4 }}>
      <span style={{ fontSize: 'var(--sz-sm)', color: 'var(--color-text-secondary)', flex: 1, minWidth: 0 }}>
        {reasons[0]}
      </span>
      <button
        onClick={() => setExpanded(true)}
        style={{
          background: 'none', border: 'none', cursor: 'pointer',
          padding: '0 0 0 4px', fontSize: 'var(--sz-xs)', color: '#185FA5',
          whiteSpace: 'nowrap', flexShrink: 0,
        }}
      >
        +{reasons.length - 1} more
      </button>
    </div>
  )
}

// ── Page ────────────────────────────────────────────────────────────────────

function isRAGIssue(w: WarningItem): boolean {
  if (w.rag_verdict === 'contradicted' || w.rag_verdict === 'unsupported') return true
  return w.reasons.some(r =>
    r.includes('contradicts') || r.includes('not found in source')
  )
}

export default function Warnings() {
  const [ragOnly, setRagOnly] = useState(false)

  const { data, isLoading, error } = useQuery<WarningsResponse>({
    queryKey: ['warnings'],
    queryFn: () => fetchJSON('/warnings'),
    refetchInterval: 10_000,
  })

  const { data: metrics } = useQuery<CostMetrics>({
    queryKey: ['metrics'],
    queryFn: () => fetchJSON('/metrics/cost'),
    staleTime: 30_000,
  })

  const warnings: WarningItem[] = data?.warnings ?? []
  const totalToday  = data?.total_today ?? 0
  const totalTraces = metrics?.total_traces ?? 0

  // All items in warnings:high:list are high-risk; medium = warned - high
  const highCount  = warnings.length
  const medCount   = Math.max(0, totalToday - highCount)
  const flagRate   = totalTraces > 0
    ? `${(totalToday / totalTraces * 100).toFixed(1)}%`
    : totalToday > 0 ? `${totalToday}` : '—'
  const ragContradictCount = warnings.filter(isRAGIssue).length

  const displayed = ragOnly ? warnings.filter(isRAGIssue) : warnings

  if (isLoading) {
    return <div style={{ padding: 24, color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>Loading…</div>
  }
  if (error) {
    return <div style={{ padding: 24, color: HIGH_COLOR, fontSize: 'var(--sz-base)' }}>Failed to load warnings</div>
  }

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 20 }}>

      {/* ── Metric cards ── */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 16 }}>
        <MetricCard label="High risk today"          value={String(highCount)}       color={HIGH_COLOR} />
        <MetricCard label="Medium risk today"        value={String(medCount)}        color={MED_COLOR}  />
        <MetricCard label="Flag rate"                value={flagRate}                color="var(--color-text-primary)" />
        <MetricCard label="RAG contradictions today" value={String(ragContradictCount)} color={HIGH_COLOR} />
      </div>

      {/* ── Filter bar ── */}
      <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <button
          onClick={() => setRagOnly(false)}
          style={{
            padding: '5px 14px',
            borderRadius: 6,
            border: '0.5px solid var(--color-border-tertiary)',
            background: !ragOnly ? 'var(--color-background-secondary)' : 'transparent',
            color: !ragOnly ? 'var(--color-text-primary)' : 'var(--color-text-tertiary)',
            fontSize: 'var(--sz-xs)',
            fontWeight: !ragOnly ? 600 : 400,
            cursor: 'pointer',
          }}
        >
          All warnings
        </button>
        <button
          onClick={() => setRagOnly(true)}
          style={{
            padding: '5px 14px',
            borderRadius: 6,
            border: ragOnly ? `0.5px solid ${HIGH_COLOR}` : '0.5px solid var(--color-border-tertiary)',
            background: ragOnly ? HIGH_BG : 'transparent',
            color: ragOnly ? HIGH_COLOR : 'var(--color-text-tertiary)',
            fontSize: 'var(--sz-xs)',
            fontWeight: ragOnly ? 600 : 400,
            cursor: 'pointer',
          }}
        >
          RAG Issues Only
        </button>
      </div>

      {/* ── Warnings table ── */}
      <div style={{
        background: 'var(--color-background-secondary)',
        border: '0.5px solid var(--color-border-tertiary)',
        borderRadius: 10,
        overflow: 'hidden',
      }}>
        {displayed.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>
            {ragOnly ? 'No RAG issues found' : 'No flagged responses yet'}
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={th}>Timestamp</th>
                  <th style={th}>Session</th>
                  <th style={th}>Feature</th>
                  <th style={th}>Risk level</th>
                  <th style={{ ...th, textAlign: 'right' }}>Hallucination</th>
                  <th style={{ ...th, textAlign: 'right' }}>Grounding</th>
                  <th style={{ ...th, width: '30%' }}>Reasons</th>
                </tr>
              </thead>
              <tbody>
                {displayed.map((w, i) => (
                  <tr
                    key={w.request_id || i}
                    style={{ background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.012)' }}
                  >
                    <td style={{ ...td, whiteSpace: 'nowrap' }}>
                      {format(new Date(w.timestamp), 'MMM d, HH:mm:ss')}
                    </td>
                    <td style={{ ...td, fontFamily: 'monospace', fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)' }}>
                      {w.session_id ? w.session_id.slice(0, 8) : '—'}
                    </td>
                    <td style={td}>
                      {w.feature_name || '—'}
                    </td>
                    <td style={td}>
                      <RiskBadge level={w.risk_level} />
                    </td>
                    <td style={{ ...td, textAlign: 'right' }}>
                      <ScoreCell value={w.hallucination_risk} />
                    </td>
                    <td style={{ ...td, textAlign: 'right' }}>
                      <ScoreCell value={w.grounding_score} higherIsGood />
                    </td>
                    <td style={{ ...td, maxWidth: 320 }}>
                      <ReasonsCell reasons={w.reasons} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

    </div>
  )
}
