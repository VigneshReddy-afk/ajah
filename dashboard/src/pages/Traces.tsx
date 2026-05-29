import type { CSSProperties, ReactNode } from 'react'
import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { TraceRecord } from '../api/types'

// ── Types ──────────────────────────────────────────────────────────────────

interface Row {
  id: string
  ts: string
  user: string
  feature: string
  model: string
  cost: number
  latency: number
  pii: boolean
  quality: number
  prompt: string
  flags: string[]
  ragVerdict: string
  ragGrounding: number
  ragContradiction: number
  ragSupported: string[]
  ragUnsupported: string[]
  ragContradicted: string[]
}

// ── Mock data ──────────────────────────────────────────────────────────────

const MOCK_ROWS: Row[] = [
  { id: '1', ts: '12:48:40', user: 'user_1', feature: 'chat',      model: 'llama-3.3-70b', cost: 0.000660, latency: 1506, pii: false, quality: 0.90, prompt: 'What is the capital of France?',              flags: [],                                  ragVerdict: 'not_applicable', ragGrounding: 0,    ragContradiction: 0,    ragSupported: [], ragUnsupported: [], ragContradicted: [] },
  { id: '2', ts: '12:42:59', user: 'user_1', feature: 'chat',      model: 'llama-3.3-70b', cost: 0.000660, latency: 1459, pii: false, quality: 0.00, prompt: '',                                             flags: ['empty_response'],                  ragVerdict: 'not_applicable', ragGrounding: 0,    ragContradiction: 0,    ragSupported: [], ragUnsupported: [], ragContradicted: [] },
  { id: '3', ts: '11:25:42', user: 'user_2', feature: 'summarize', model: 'gpt-4o',        cost: 0.002340, latency: 2341, pii: true,  quality: 0.85, prompt: 'Summarize the following document for me…',    flags: [],                                  ragVerdict: 'supported',      ragGrounding: 0.91, ragContradiction: 0.02, ragSupported: ['The report covers Q3 results.'], ragUnsupported: [], ragContradicted: [] },
  { id: '4', ts: '10:15:33', user: 'user_3', feature: 'classify',  model: 'claude-3-5',    cost: 0.001200, latency:  890, pii: false, quality: 0.95, prompt: 'Classify the sentiment of: Great product!',   flags: [],                                  ragVerdict: 'not_applicable', ragGrounding: 0,    ragContradiction: 0,    ragSupported: [], ragUnsupported: [], ragContradicted: [] },
  { id: '5', ts: '09:45:12', user: 'user_1', feature: 'chat',      model: 'llama-3.3-70b', cost: 0.000550, latency: 1100, pii: false, quality: 0.88, prompt: 'How does machine learning work?',              flags: [],                                  ragVerdict: 'partially_supported', ragGrounding: 0.61, ragContradiction: 0.08, ragSupported: ['ML uses statistical models.'], ragUnsupported: ['Quantum effects enable learning.'], ragContradicted: [] },
  { id: '6', ts: '09:12:05', user: 'user_4', feature: 'translate', model: 'mistral',       cost: 0.000890, latency:  750, pii: true,  quality: 0.92, prompt: 'Translate: Contact me at [EMAIL MASKED]',     flags: ['pii_detected'],                    ragVerdict: 'not_applicable', ragGrounding: 0,    ragContradiction: 0,    ragSupported: [], ragUnsupported: [], ragContradicted: [] },
  { id: '7', ts: '08:33:44', user: 'user_2', feature: 'summarize', model: 'gpt-4o',        cost: 0.001800, latency: 1890, pii: false, quality: 0.78, prompt: 'Summarize this article about AI safety…',     flags: [],                                  ragVerdict: 'contradicted',   ragGrounding: 0.22, ragContradiction: 0.81, ragSupported: [], ragUnsupported: [], ragContradicted: ['The article states AI poses no risk.'] },
  { id: '8', ts: '08:01:22', user: 'user_5', feature: 'chat',      model: 'gemini-1.5',    cost: 0.000420, latency:  650, pii: false, quality: 0.93, prompt: 'Write a haiku about autumn.',                  flags: [],                                  ragVerdict: 'not_applicable', ragGrounding: 0,    ragContradiction: 0,    ragSupported: [], ragUnsupported: [], ragContradicted: [] },
  { id: '9', ts: '22:04:13', user: 'user_1', feature: 'chat',      model: 'gpt-4o',        cost: 0.000000, latency:  875, pii: true,  quality: 0.00, prompt: 'Call me at [PHONE MASKED]',                   flags: ['pii_detected', 'empty_response'],  ragVerdict: 'not_applicable', ragGrounding: 0,    ragContradiction: 0,    ragSupported: [], ragUnsupported: [], ragContradicted: [] },
]

function fromAPI(t: TraceRecord): Row {
  return {
    id: t.trace_id,
    ts: format(new Date(t.timestamp), 'HH:mm:ss'),
    user: t.user_id,
    feature: t.feature_name,
    model: t.model,
    cost: t.cost_usd,
    latency: t.latency_ms,
    pii: t.was_pii_masked,
    quality: t.quality_score,
    prompt: t.masked_prompt,
    flags: [],
    ragVerdict: t.rag_verdict ?? 'not_applicable',
    ragGrounding: t.rag_grounding_score ?? 0,
    ragContradiction: t.rag_contradiction_score ?? 0,
    ragSupported: t.rag_supported_claims ?? [],
    ragUnsupported: t.rag_unsupported_claims ?? [],
    ragContradicted: t.rag_contradicted_claims ?? [],
  }
}

// ── Helpers ────────────────────────────────────────────────────────────────

function qualityStyle(score: number): CSSProperties {
  if (score === 0) return { color: 'var(--color-text-tertiary)' }
  if (score >= 0.8)  return { color: '#0F6E56', fontWeight: 500 }
  if (score >= 0.5)  return { color: '#854F0B', fontWeight: 500 }
  return { color: '#A32D2D', fontWeight: 500 }
}

const badgeBase: CSSProperties = {
  display: 'inline-block',
  fontSize: 'var(--sz-xs)',
  fontWeight: 600,
  padding: '2px 7px',
  borderRadius: 4,
  letterSpacing: '0.03em',
  whiteSpace: 'nowrap',
}

function RAGBadge({ verdict, large = false }: { verdict: string; large?: boolean }): ReactNode {
  const base: CSSProperties = { ...badgeBase, fontSize: large ? 'var(--sz-sm)' : 'var(--sz-xs)', padding: large ? '4px 10px' : '2px 7px' }
  switch (verdict) {
    case 'supported':
      return <span style={{ ...base, color: '#0F6E56', background: 'rgba(15,110,86,0.12)' }}>✓ grounded</span>
    case 'partially_supported':
      return <span style={{ ...base, color: '#854F0B', background: 'rgba(133,79,11,0.12)' }}>~ partial</span>
    case 'unsupported':
      return <span style={{ ...base, color: '#A32D2D', background: 'rgba(163,45,45,0.12)' }}>✗ ungrounded</span>
    case 'contradicted':
      return <span style={{ ...base, color: '#fff', background: '#A32D2D' }}>⚠ contradicted</span>
    default:
      return <span style={{ ...base, color: 'var(--color-text-tertiary)', background: 'transparent' }}>—</span>
  }
}

const thStyle: CSSProperties = {
  padding: '10px 14px',
  fontSize: 'var(--sz-xs)',
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
  fontSize: 'var(--sz-base)',
  color: 'var(--color-text-secondary)',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
  whiteSpace: 'nowrap',
}

// ── RAG Expanded Section ───────────────────────────────────────────────────

function ClaimList({ claims, color, bg }: { claims: string[]; color: string; bg: string }) {
  if (!claims.length) return null
  return (
    <div style={{ marginTop: 4 }}>
      {claims.map((c, i) => (
        <div key={i} style={{
          fontSize: 'var(--sz-xs)',
          color,
          background: bg,
          border: `0.5px solid ${color}33`,
          borderRadius: 4,
          padding: '3px 8px',
          marginBottom: 3,
          lineHeight: 1.5,
        }}>
          {c}
        </div>
      ))}
    </div>
  )
}

function RAGSection({ row }: { row: Row }) {
  const { ragVerdict, ragGrounding, ragContradiction, ragSupported, ragUnsupported, ragContradicted } = row
  if (ragVerdict === 'not_applicable' || ragVerdict === 'unavailable' || ragVerdict === '' || !ragVerdict) {
    return null
  }

  return (
    <div style={{ marginTop: 14 }}>
      <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 8px' }}>
        RAG Verification
      </p>
      <div style={{
        background: 'var(--color-background-secondary)',
        border: '0.5px solid var(--color-border-tertiary)',
        borderRadius: 6,
        padding: '12px 14px',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
          <RAGBadge verdict={ragVerdict} large />
          <div style={{ display: 'flex', gap: 16 }}>
            <div>
              <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Grounding </span>
              <span style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: ragGrounding >= 0.7 ? '#0F6E56' : ragGrounding >= 0.4 ? '#854F0B' : '#A32D2D', fontVariantNumeric: 'tabular-nums' }}>
                {ragGrounding.toFixed(2)}
              </span>
            </div>
            <div>
              <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Contradiction </span>
              <span style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: ragContradiction >= 0.6 ? '#A32D2D' : ragContradiction >= 0.3 ? '#854F0B' : '#0F6E56', fontVariantNumeric: 'tabular-nums' }}>
                {ragContradiction.toFixed(2)}
              </span>
            </div>
          </div>
        </div>

        {ragContradicted.length > 0 && (
          <div style={{ marginTop: 10 }}>
            <p style={{ fontSize: 'var(--sz-xs)', color: '#A32D2D', fontWeight: 500, margin: '0 0 4px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
              Contradicted claims ({ragContradicted.length})
            </p>
            <ClaimList claims={ragContradicted} color="#A32D2D" bg="rgba(163,45,45,0.08)" />
          </div>
        )}

        {ragUnsupported.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <p style={{ fontSize: 'var(--sz-xs)', color: '#854F0B', fontWeight: 500, margin: '0 0 4px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
              Unsupported claims ({ragUnsupported.length})
            </p>
            <ClaimList claims={ragUnsupported} color="#854F0B" bg="rgba(133,79,11,0.08)" />
          </div>
        )}

        {ragSupported.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <p style={{ fontSize: 'var(--sz-xs)', color: '#0F6E56', fontWeight: 500, margin: '0 0 4px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
              Supported claims ({ragSupported.length})
            </p>
            <ClaimList claims={ragSupported} color="#0F6E56" bg="rgba(15,110,86,0.08)" />
          </div>
        )}
      </div>
    </div>
  )
}

// ── Page ───────────────────────────────────────────────────────────────────

export default function Traces() {
  const [expanded, setExpanded] = useState<string | null>(null)

  const { data = [], isLoading, error } = useQuery<TraceRecord[]>({
    queryKey: ['traces'],
    queryFn: () => fetchJSON('/metrics/traces'),
  })

  if (isLoading) return <div style={{ padding: 24, color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>Loading…</div>
  if (error)     return <div style={{ padding: 24, color: '#A32D2D', fontSize: 'var(--sz-base)' }}>Failed to load traces</div>

  const rows: Row[] = data.length > 0 ? data.map(fromAPI) : MOCK_ROWS

  return (
    <div style={{ padding: 24 }}>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)' }}>
          {rows.length} rows — click row to expand
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
                {['Timestamp', 'User', 'Feature', 'Model', 'Cost', 'Latency', 'PII', 'Quality', 'RAG'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 && (
                <tr>
                  <td colSpan={9} style={{ padding: '40px 14px', textAlign: 'center', color: 'var(--color-text-tertiary)', fontSize: 13 }}>
                    No traces yet
                  </td>
                </tr>
              )}

              {rows.map(row => (
                <Fragment key={row.id}>
                  <tr
                    onClick={() => setExpanded(expanded === row.id ? null : row.id)}
                    style={{ cursor: 'pointer', transition: 'background 0.1s' }}
                    onMouseEnter={e => (e.currentTarget.style.background = 'var(--color-background-primary)')}
                    onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                  >
                    {/* Timestamp */}
                    <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12, color: 'var(--color-text-tertiary)' }}>
                      {row.ts}
                    </td>
                    {/* User */}
                    <td style={tdStyle}>{row.user || '—'}</td>
                    {/* Feature */}
                    <td style={{ ...tdStyle, color: 'var(--color-text-primary)' }}>{row.feature || '—'}</td>
                    {/* Model badge */}
                    <td style={tdStyle}>
                      <span style={{
                        fontSize: 'var(--sz-xs)', fontWeight: 500,
                        color: '#185FA5',
                        background: 'rgba(24,95,165,0.10)',
                        padding: '2px 7px',
                        borderRadius: 4,
                      }}>
                        {row.model}
                      </span>
                    </td>
                    {/* Cost */}
                    <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums' }}>
                      ${row.cost.toFixed(6)}
                    </td>
                    {/* Latency */}
                    <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums' }}>
                      {row.latency}ms
                    </td>
                    {/* PII */}
                    <td style={tdStyle}>
                      {row.pii ? (
                        <span style={{
                          fontSize: 'var(--sz-xs)', fontWeight: 500,
                          color: '#A32D2D',
                          background: 'rgba(163,45,45,0.12)',
                          padding: '2px 7px',
                          borderRadius: 4,
                        }}>YES</span>
                      ) : (
                        <span style={{
                          fontSize: 'var(--sz-xs)',
                          color: 'var(--color-text-tertiary)',
                          background: 'var(--color-background-primary)',
                          padding: '2px 7px',
                          borderRadius: 4,
                        }}>NO</span>
                      )}
                    </td>
                    {/* Quality */}
                    <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums', ...qualityStyle(row.quality) }}>
                      {row.quality.toFixed(2)}
                    </td>
                    {/* RAG */}
                    <td style={tdStyle}>
                      <RAGBadge verdict={row.ragVerdict} />
                    </td>
                  </tr>

                  {/* Expanded row */}
                  {expanded === row.id && (
                    <tr>
                      <td colSpan={9} style={{
                        padding: '16px 14px 18px',
                        background: 'var(--color-background-primary)',
                        borderBottom: '0.5px solid var(--color-border-tertiary)',
                      }}>
                        <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 8px' }}>
                          Masked Prompt
                        </p>
                        <div style={{
                          background: 'var(--color-background-secondary)',
                          border: '0.5px solid var(--color-border-tertiary)',
                          borderRadius: 6,
                          padding: '10px 14px',
                          fontFamily: 'monospace',
                          fontSize: 'var(--sz-base)',
                          color: 'var(--color-text-primary)',
                          lineHeight: 1.6,
                          whiteSpace: 'pre-wrap',
                        }}>
                          {row.prompt || <span style={{ color: 'var(--color-text-tertiary)', fontStyle: 'italic' }}>empty</span>}
                        </div>

                        {row.flags.length > 0 && (
                          <div style={{ display: 'flex', gap: 6, marginTop: 10, flexWrap: 'wrap' }}>
                            {row.flags.map(flag => (
                              <span key={flag} style={{
                                fontSize: 'var(--sz-label)', fontWeight: 500,
                                color: '#854F0B',
                                background: 'rgba(133,79,11,0.12)',
                                border: '0.5px solid rgba(133,79,11,0.2)',
                                padding: '2px 8px',
                                borderRadius: 4,
                              }}>
                                {flag}
                              </span>
                            ))}
                          </div>
                        )}

                        <RAGSection row={row} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
