import type { CSSProperties, ReactNode } from 'react'
import { Fragment, useState, useMemo } from 'react'
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
  inputTokens: number
  outputTokens: number
  pii: boolean
  quality: number
  prompt: string
  flags: string[]
  riskLevel: string
  shouldWarn: boolean
  hallucinationRisk: number
  groundingScore: number
  claimDensityRisk: number
  hedgeRisk: number
  driftRisk: number
  ragVerdict: string
  ragGrounding: number
  ragContradiction: number
  ragSupported: string[]
  ragUnsupported: string[]
  ragContradicted: string[]
  crossModelVerdict: string
  crossModelAgreement: number
  sessionId: string
  parentStepId: string
  stepName: string
  toolName: string
  agentStep: string
  provider: string
  statusCode: number
  timestamp: string
  raw: TraceRecord
}

interface StepNode {
  trace: TraceRecord
  children: StepNode[]
}

// ── Mock data ──────────────────────────────────────────────────────────────

const MOCK_ROWS: Row[] = [
  {
    id: '1', ts: '12:48:40', user: 'user_1', feature: 'chat', model: 'llama-3.3-70b',
    cost: 0.000660, latency: 1506, inputTokens: 320, outputTokens: 180,
    pii: false, quality: 0.90,
    prompt: 'What is the capital of France?', flags: [],
    riskLevel: 'low', shouldWarn: false, hallucinationRisk: 0.08, groundingScore: 0.94,
    claimDensityRisk: 0.12, hedgeRisk: 0.05, driftRisk: 0.02,
    ragVerdict: 'not_applicable', ragGrounding: 0, ragContradiction: 0,
    ragSupported: [], ragUnsupported: [], ragContradicted: [],
    crossModelVerdict: 'agree', crossModelAgreement: 0.92,
    sessionId: 'sess-1', parentStepId: '', stepName: 'query', toolName: '',
    agentStep: '1', provider: 'groq', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
  {
    id: '2', ts: '12:42:59', user: 'user_1', feature: 'chat', model: 'llama-3.3-70b',
    cost: 0.000660, latency: 1459, inputTokens: 180, outputTokens: 0,
    pii: false, quality: 0.00,
    prompt: '', flags: ['empty_response'],
    riskLevel: 'medium', shouldWarn: true, hallucinationRisk: 0.31, groundingScore: 0.55,
    claimDensityRisk: 0.22, hedgeRisk: 0.48, driftRisk: 0.11,
    ragVerdict: 'not_applicable', ragGrounding: 0, ragContradiction: 0,
    ragSupported: [], ragUnsupported: [], ragContradicted: [],
    crossModelVerdict: '', crossModelAgreement: 0,
    sessionId: 'sess-1', parentStepId: 'query', stepName: 'fallback', toolName: '',
    agentStep: '2', provider: 'groq', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
  {
    id: '3', ts: '11:25:42', user: 'user_2', feature: 'summarize', model: 'gpt-4o',
    cost: 0.002340, latency: 2341, inputTokens: 620, outputTokens: 410,
    pii: true, quality: 0.85,
    prompt: 'Summarize the following document for me…', flags: [],
    riskLevel: 'low', shouldWarn: false, hallucinationRisk: 0.11, groundingScore: 0.89,
    claimDensityRisk: 0.19, hedgeRisk: 0.08, driftRisk: 0.03,
    ragVerdict: 'supported', ragGrounding: 0.91, ragContradiction: 0.02,
    ragSupported: ['The report covers Q3 results.'], ragUnsupported: [], ragContradicted: [],
    crossModelVerdict: '', crossModelAgreement: 0,
    sessionId: 'sess-2', parentStepId: '', stepName: 'summarize', toolName: '',
    agentStep: '1', provider: 'openai', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
  {
    id: '4', ts: '10:15:33', user: 'user_3', feature: 'classify', model: 'claude-3-5',
    cost: 0.001200, latency: 890, inputTokens: 210, outputTokens: 95,
    pii: false, quality: 0.95,
    prompt: 'Classify the sentiment of: Great product!', flags: [],
    riskLevel: 'low', shouldWarn: false, hallucinationRisk: 0.05, groundingScore: 0.97,
    claimDensityRisk: 0.07, hedgeRisk: 0.03, driftRisk: 0.01,
    ragVerdict: 'not_applicable', ragGrounding: 0, ragContradiction: 0,
    ragSupported: [], ragUnsupported: [], ragContradicted: [],
    crossModelVerdict: '', crossModelAgreement: 0,
    sessionId: 'sess-3', parentStepId: '', stepName: 'classify', toolName: '',
    agentStep: '1', provider: 'anthropic', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
  {
    id: '5', ts: '09:45:12', user: 'user_1', feature: 'chat', model: 'llama-3.3-70b',
    cost: 0.000550, latency: 1100, inputTokens: 280, outputTokens: 160,
    pii: false, quality: 0.88,
    prompt: 'How does machine learning work?', flags: [],
    riskLevel: 'medium', shouldWarn: true, hallucinationRisk: 0.42, groundingScore: 0.61,
    claimDensityRisk: 0.55, hedgeRisk: 0.38, driftRisk: 0.19,
    ragVerdict: 'partially_supported', ragGrounding: 0.61, ragContradiction: 0.08,
    ragSupported: ['ML uses statistical models.'], ragUnsupported: ['Quantum effects enable learning.'], ragContradicted: [],
    crossModelVerdict: 'partial', crossModelAgreement: 0.65,
    sessionId: 'sess-4', parentStepId: '', stepName: 'explain', toolName: '',
    agentStep: '1', provider: 'groq', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
  {
    id: '6', ts: '09:12:05', user: 'user_4', feature: 'translate', model: 'mistral',
    cost: 0.000890, latency: 750, inputTokens: 190, outputTokens: 140,
    pii: true, quality: 0.92,
    prompt: 'Translate: Contact me at [EMAIL MASKED]', flags: ['pii_detected'],
    riskLevel: 'low', shouldWarn: false, hallucinationRisk: 0.07, groundingScore: 0.93,
    claimDensityRisk: 0.09, hedgeRisk: 0.04, driftRisk: 0.02,
    ragVerdict: 'not_applicable', ragGrounding: 0, ragContradiction: 0,
    ragSupported: [], ragUnsupported: [], ragContradicted: [],
    crossModelVerdict: '', crossModelAgreement: 0,
    sessionId: 'sess-5', parentStepId: '', stepName: 'translate', toolName: '',
    agentStep: '1', provider: 'mistral', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
  {
    id: '7', ts: '08:33:44', user: 'user_2', feature: 'summarize', model: 'gpt-4o',
    cost: 0.001800, latency: 1890, inputTokens: 540, outputTokens: 290,
    pii: false, quality: 0.78,
    prompt: 'Summarize this article about AI safety…', flags: [],
    riskLevel: 'high', shouldWarn: true, hallucinationRisk: 0.74, groundingScore: 0.31,
    claimDensityRisk: 0.81, hedgeRisk: 0.62, driftRisk: 0.55,
    ragVerdict: 'contradicted', ragGrounding: 0.22, ragContradiction: 0.81,
    ragSupported: [], ragUnsupported: [], ragContradicted: ['The article states AI poses no risk.'],
    crossModelVerdict: 'disagree', crossModelAgreement: 0.28,
    sessionId: 'sess-6', parentStepId: '', stepName: 'summarize', toolName: '',
    agentStep: '1', provider: 'openai', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
  {
    id: '8', ts: '08:01:22', user: 'user_5', feature: 'chat', model: 'gemini-1.5',
    cost: 0.000420, latency: 650, inputTokens: 120, outputTokens: 80,
    pii: false, quality: 0.93,
    prompt: 'Write a haiku about autumn.', flags: [],
    riskLevel: 'low', shouldWarn: false, hallucinationRisk: 0.06, groundingScore: 0.95,
    claimDensityRisk: 0.08, hedgeRisk: 0.03, driftRisk: 0.01,
    ragVerdict: 'not_applicable', ragGrounding: 0, ragContradiction: 0,
    ragSupported: [], ragUnsupported: [], ragContradicted: [],
    crossModelVerdict: '', crossModelAgreement: 0,
    sessionId: 'sess-7', parentStepId: '', stepName: 'generate', toolName: '',
    agentStep: '1', provider: 'google', statusCode: 200,
    timestamp: new Date().toISOString(), raw: {} as TraceRecord,
  },
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
    inputTokens: t.input_tokens,
    outputTokens: t.output_tokens,
    pii: t.was_pii_masked,
    quality: t.quality_score,
    prompt: t.masked_prompt,
    flags: [],
    riskLevel: t.risk_level ?? 'low',
    shouldWarn: t.should_warn ?? false,
    hallucinationRisk: t.hallucination_risk ?? 0,
    groundingScore: t.grounding_score ?? 0,
    claimDensityRisk: 0,
    hedgeRisk: 0,
    driftRisk: 0,
    ragVerdict: t.rag_verdict ?? 'not_applicable',
    ragGrounding: t.rag_grounding_score ?? 0,
    ragContradiction: t.rag_contradiction_score ?? 0,
    ragSupported: t.rag_supported_claims ?? [],
    ragUnsupported: t.rag_unsupported_claims ?? [],
    ragContradicted: t.rag_contradicted_claims ?? [],
    crossModelVerdict: t.cross_model_verdict ?? '',
    crossModelAgreement: t.cross_model_agreement ?? 0,
    sessionId: t.session_id,
    parentStepId: t.parent_step_id,
    stepName: t.step_name,
    toolName: t.tool_name,
    agentStep: t.agent_step,
    provider: t.provider,
    statusCode: t.status_code,
    timestamp: t.timestamp,
    raw: t,
  }
}

// ── Helpers ────────────────────────────────────────────────────────────────

type FilterKey = 'all' | 'high' | 'warn' | 'pii' | 'contradicted' | 'disagree'

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: 'all', label: 'All traces' },
  { key: 'high', label: 'High risk' },
  { key: 'warn', label: 'Warnings' },
  { key: 'pii', label: 'PII detected' },
  { key: 'contradicted', label: 'RAG contradicted' },
  { key: 'disagree', label: 'Cross-model disagree' },
]

function applyFilter(rows: Row[], key: FilterKey): Row[] {
  switch (key) {
    case 'high': return rows.filter(r => r.riskLevel === 'high')
    case 'warn': return rows.filter(r => r.shouldWarn)
    case 'pii':  return rows.filter(r => r.pii)
    case 'contradicted': return rows.filter(r => r.ragVerdict === 'contradicted')
    case 'disagree': return rows.filter(r => r.crossModelVerdict === 'disagree')
    default: return rows
  }
}

function riskColor(v: number): string {
  if (v >= 0.65) return 'var(--color-danger)'
  if (v >= 0.35) return 'var(--color-warning)'
  return 'var(--color-success)'
}

function qualityColor(v: number): string {
  if (v === 0) return 'var(--color-text-tertiary)'
  if (v >= 0.8) return 'var(--color-success)'
  if (v >= 0.5) return 'var(--color-warning)'
  return 'var(--color-danger)'
}

function riskLevelStyle(level: string): CSSProperties {
  switch (level) {
    case 'high':   return { color: 'var(--color-danger)',  background: 'var(--color-danger-glow)' }
    case 'medium': return { color: 'var(--color-warning)', background: 'rgba(240,164,41,0.12)' }
    default:       return { color: 'var(--color-success)', background: 'rgba(34,196,138,0.12)' }
  }
}

const badgeBase: CSSProperties = {
  display: 'inline-block', fontSize: 'var(--sz-xs)', fontWeight: 600,
  padding: '2px 7px', borderRadius: 4, letterSpacing: '0.03em', whiteSpace: 'nowrap',
}

function RAGBadge({ verdict, large = false }: { verdict: string; large?: boolean }): ReactNode {
  const base: CSSProperties = { ...badgeBase, fontSize: large ? 'var(--sz-sm)' : 'var(--sz-xs)', padding: large ? '4px 10px' : '2px 7px' }
  switch (verdict) {
    case 'supported':         return <span style={{ ...base, color: 'var(--color-success)', background: 'rgba(34,196,138,0.12)' }}>✓ grounded</span>
    case 'partially_supported': return <span style={{ ...base, color: 'var(--color-warning)', background: 'rgba(240,164,41,0.12)' }}>~ partial</span>
    case 'unsupported':       return <span style={{ ...base, color: 'var(--color-text-tertiary)', background: 'var(--color-background-secondary)' }}>✗ ungrounded</span>
    case 'contradicted':      return <span style={{ ...base, color: '#fff', background: 'var(--color-danger)' }}>⚠ contradicted</span>
    default: return <span style={{ ...base, color: 'var(--color-text-tertiary)', background: 'transparent' }}>—</span>
  }
}

function CrossModelBadge({ verdict, large = false }: { verdict: string; large?: boolean }): ReactNode {
  const base: CSSProperties = { ...badgeBase, fontSize: large ? 'var(--sz-sm)' : 'var(--sz-xs)', padding: large ? '4px 10px' : '2px 7px' }
  switch (verdict) {
    case 'agree':   return <span style={{ ...base, color: 'var(--color-success)', background: 'rgba(34,196,138,0.12)' }}>agree</span>
    case 'partial': return <span style={{ ...base, color: 'var(--color-warning)', background: 'rgba(240,164,41,0.12)' }}>partial</span>
    case 'disagree': return <span style={{ ...base, color: 'var(--color-danger)', background: 'var(--color-danger-glow)' }}>disagree</span>
    default: return <span style={{ ...base, color: 'var(--color-text-tertiary)', background: 'transparent' }}>—</span>
  }
}

const thStyle: CSSProperties = {
  padding: '10px 14px', fontSize: 'var(--sz-xs)', fontWeight: 500,
  color: 'var(--color-text-tertiary)', textTransform: 'uppercase',
  letterSpacing: '0.05em', textAlign: 'left',
  borderBottom: '0.5px solid var(--color-border-tertiary)', whiteSpace: 'nowrap',
}

const tdStyle: CSSProperties = {
  padding: '11px 14px', fontSize: 'var(--sz-base)',
  color: 'var(--color-text-secondary)',
  borderBottom: '0.5px solid var(--color-border-tertiary)', whiteSpace: 'nowrap',
}

// ── Risk bar ───────────────────────────────────────────────────────────────

function RiskBar({ value }: { value: number }): ReactNode {
  const color = riskColor(value)
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
      <div style={{
        flex: 1, height: 4, background: 'var(--color-border-tertiary)',
        borderRadius: 2, minWidth: 44, overflow: 'hidden',
      }}>
        <div style={{
          width: `${Math.round(value * 100)}%`, height: '100%',
          background: color, borderRadius: 2, transition: 'width 0.3s ease',
        }} />
      </div>
      <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', minWidth: 30, fontVariantNumeric: 'tabular-nums' }}>
        {value.toFixed(2)}
      </span>
    </div>
  )
}

// ── Claim list ─────────────────────────────────────────────────────────────

function ClaimList({ claims, color, bg }: { claims: string[]; color: string; bg: string }) {
  if (!claims.length) return null
  return (
    <div style={{ marginTop: 4 }}>
      {claims.map((c, i) => (
        <div key={i} style={{
          fontSize: 'var(--sz-xs)', color, background: bg,
          border: `0.5px solid ${color}33`, borderRadius: 4,
          padding: '3px 8px', marginBottom: 3, lineHeight: 1.5,
        }}>{c}</div>
      ))}
    </div>
  )
}

// ── RAG section ────────────────────────────────────────────────────────────

function RAGSection({ row }: { row: Row }): ReactNode {
  const { ragVerdict, ragGrounding, ragContradiction, ragSupported, ragUnsupported, ragContradicted } = row
  if (!ragVerdict || ragVerdict === 'not_applicable' || ragVerdict === 'unavailable' || ragVerdict === '') return null
  return (
    <div style={{ marginTop: 14 }}>
      <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 8px' }}>
        RAG Verification
      </p>
      <div style={{ background: 'var(--color-background-secondary)', border: '0.5px solid var(--color-border-tertiary)', borderRadius: 6, padding: '12px 14px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
          <RAGBadge verdict={ragVerdict} large />
          <div style={{ display: 'flex', gap: 16 }}>
            <div>
              <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Grounding </span>
              <span style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: ragGrounding >= 0.7 ? 'var(--color-success)' : ragGrounding >= 0.4 ? 'var(--color-warning)' : 'var(--color-danger)', fontVariantNumeric: 'tabular-nums' }}>
                {ragGrounding.toFixed(2)}
              </span>
            </div>
            <div>
              <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Contradiction </span>
              <span style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: ragContradiction >= 0.6 ? 'var(--color-danger)' : ragContradiction >= 0.3 ? 'var(--color-warning)' : 'var(--color-success)', fontVariantNumeric: 'tabular-nums' }}>
                {ragContradiction.toFixed(2)}
              </span>
            </div>
          </div>
        </div>
        {ragContradicted.length > 0 && (
          <div style={{ marginTop: 10 }}>
            <p style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-danger)', fontWeight: 500, margin: '0 0 4px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>Contradicted claims ({ragContradicted.length})</p>
            <ClaimList claims={ragContradicted} color="var(--color-danger)" bg="var(--color-danger-glow)" />
          </div>
        )}
        {ragUnsupported.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <p style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-warning)', fontWeight: 500, margin: '0 0 4px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>Unsupported claims ({ragUnsupported.length})</p>
            <ClaimList claims={ragUnsupported} color="var(--color-warning)" bg="rgba(240,164,41,0.08)" />
          </div>
        )}
        {ragSupported.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <p style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-success)', fontWeight: 500, margin: '0 0 4px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>Supported claims ({ragSupported.length})</p>
            <ClaimList claims={ragSupported} color="var(--color-success)" bg="rgba(34,196,138,0.08)" />
          </div>
        )}
      </div>
    </div>
  )
}

// ── Cross-model section ────────────────────────────────────────────────────

function CrossModelSection({ row }: { row: Row }): ReactNode {
  if (!row.crossModelVerdict) return null
  return (
    <div style={{ marginTop: 14 }}>
      <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 8px' }}>
        Cross-model Verification
      </p>
      <div style={{ background: 'var(--color-background-secondary)', border: '0.5px solid var(--color-border-tertiary)', borderRadius: 6, padding: '12px 14px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
          <CrossModelBadge verdict={row.crossModelVerdict} large />
          <div>
            <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Agreement </span>
            <span style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, fontVariantNumeric: 'tabular-nums', color: row.crossModelAgreement >= 0.8 ? 'var(--color-success)' : row.crossModelAgreement >= 0.5 ? 'var(--color-warning)' : 'var(--color-danger)' }}>
              {row.crossModelAgreement.toFixed(2)}
            </span>
          </div>
          <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', fontFamily: 'monospace' }}>
            Primary: {row.model}
          </span>
        </div>
      </div>
    </div>
  )
}

// ── Step tree (copied from Sessions, scoped to a single trace's session) ───

function buildTree(traces: TraceRecord[]): StepNode[] {
  const byKey = new Map<string, StepNode>()
  for (const t of traces) {
    byKey.set(t.step_name || t.trace_id, { trace: t, children: [] })
  }
  const roots: StepNode[] = []
  for (const t of traces) {
    const myKey = t.step_name || t.trace_id
    const node = byKey.get(myKey)!
    if (!t.parent_step_id || !byKey.has(t.parent_step_id)) {
      roots.push(node)
    } else {
      byKey.get(t.parent_step_id)!.children.push(node)
    }
  }
  return roots
}

function stepBorderColor(score: number): string {
  if (score === 0) return 'var(--color-border-tertiary)'
  if (score >= 0.8) return '#0F6E56'
  if (score >= 0.7) return 'var(--color-border-tertiary)'
  if (score >= 0.5) return '#854F0B'
  return 'var(--color-danger)'
}

function MiniStepCard({ trace }: { trace: TraceRecord }): ReactNode {
  return (
    <div style={{
      width: 152, background: 'var(--color-background-card)',
      border: `1.5px solid ${stepBorderColor(trace.quality_score)}`,
      borderRadius: 8, padding: '8px 10px', flexShrink: 0,
    }}>
      <div style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: 'var(--color-text-primary)', marginBottom: 3, wordBreak: 'break-word', lineHeight: 1.3 }}>
        {trace.step_name || trace.agent_step || '(unnamed)'}
      </div>
      {trace.tool_name && (
        <span style={{ fontSize: 'var(--sz-label)', fontWeight: 500, color: 'var(--color-accent)', background: 'var(--color-accent-glow)', padding: '1px 5px', borderRadius: 3, display: 'inline-block', marginBottom: 4 }}>
          {trace.tool_name}
        </span>
      )}
      <div style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', marginBottom: 1 }}>{trace.model}</div>
      <div style={{ fontSize: 'var(--sz-xs)', fontVariantNumeric: 'tabular-nums', color: 'var(--color-text-secondary)', marginBottom: 1 }}>{trace.latency_ms}ms</div>
      <div style={{ fontSize: 'var(--sz-xs)', fontWeight: 500, color: qualityColor(trace.quality_score) }}>Q {trace.quality_score.toFixed(2)}</div>
    </div>
  )
}

function MiniStepColumn({ node }: { node: StepNode }): ReactNode {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start' }}>
      <MiniStepCard trace={node.trace} />
      {node.children.length > 0 && (
        <>
          <div style={{ paddingLeft: 72, color: 'var(--color-text-tertiary)', fontSize: 13, lineHeight: 1, padding: '3px 0 3px 72px', userSelect: 'none' }}>↓</div>
          <div style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start' }}>
            {node.children.map((child, i) => (
              <Fragment key={child.trace.trace_id}>
                {i > 0 && (
                  <div style={{ display: 'flex', alignItems: 'flex-start', paddingTop: 40, color: 'var(--color-text-tertiary)', fontSize: 14, userSelect: 'none', padding: '40px 6px 0' }}>→</div>
                )}
                <MiniStepColumn node={child} />
              </Fragment>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function SessionStepTree({ sessionId, allRows }: { sessionId: string; allRows: Row[] }): ReactNode {
  const sessionTraces = allRows
    .filter(r => r.sessionId === sessionId && r.stepName)
    .map(r => r.raw)
  if (sessionTraces.length <= 1) return null
  const roots = buildTree(sessionTraces)
  if (roots.length === 0) return null
  return (
    <div style={{ marginTop: 14 }}>
      <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 8px' }}>
        Session step tree — {sessionId} ({sessionTraces.length} steps)
      </p>
      <div style={{ background: 'var(--color-background-secondary)', border: '0.5px solid var(--color-border-tertiary)', borderRadius: 6, padding: '12px 14px', overflowX: 'auto' }}>
        <div style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start', minWidth: 'max-content' }}>
          {roots.map((root, i) => (
            <Fragment key={root.trace.trace_id}>
              {i > 0 && (
                <div style={{ display: 'flex', alignItems: 'flex-start', paddingTop: 40, color: 'var(--color-text-tertiary)', fontSize: 14, userSelect: 'none', padding: '40px 6px 0' }}>→</div>
              )}
              <MiniStepColumn node={root} />
            </Fragment>
          ))}
        </div>
      </div>
    </div>
  )
}

// ── Latency context bars ───────────────────────────────────────────────────

function LatencyBars({ row, allRows }: { row: Row; allRows: Row[] }): ReactNode {
  const maxLat = Math.max(...allRows.map(r => r.latency), 1)
  const globalAvg = Math.round(allRows.reduce((s, r) => s + r.latency, 0) / allRows.length)
  const sessionRows = allRows.filter(r => r.sessionId === row.sessionId)
  const sessionAvg = Math.round(sessionRows.reduce((s, r) => s + r.latency, 0) / sessionRows.length)

  const bars = [
    { label: 'This request', value: row.latency, color: 'var(--color-accent)' },
    { label: 'Session avg', value: sessionAvg, color: 'var(--color-success)' },
    { label: 'Global avg', value: globalAvg, color: 'var(--color-text-tertiary)' },
  ]

  return (
    <div style={{ marginTop: 14 }}>
      <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 10px' }}>
        Latency context
      </p>
      {bars.map(b => (
        <div key={b.label} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 7 }}>
          <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', minWidth: 88 }}>{b.label}</span>
          <div style={{ flex: 1, height: 6, background: 'var(--color-border-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
            <div style={{ width: `${Math.round((b.value / maxLat) * 100)}%`, height: '100%', background: b.color, borderRadius: 3, transition: 'width 0.3s ease' }} />
          </div>
          <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-secondary)', minWidth: 44, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{b.value}ms</span>
        </div>
      ))}
    </div>
  )
}

// ── Risk signals panel ─────────────────────────────────────────────────────

function RiskSignals({ row }: { row: Row }): ReactNode {
  return (
    <div style={{ marginTop: 14 }}>
      <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 10px' }}>
        Risk breakdown
      </p>
      <div style={{ background: 'var(--color-background-secondary)', border: '0.5px solid var(--color-border-tertiary)', borderRadius: 6, padding: '12px 14px' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px 24px' }}>
          {[
            { label: 'Hallucination', value: row.hallucinationRisk },
            { label: 'Grounding', value: 1 - row.groundingScore, invert: true, raw: row.groundingScore },
            { label: 'Claim density', value: row.claimDensityRisk },
            { label: 'Hedge risk', value: row.hedgeRisk },
            { label: 'Drift risk', value: row.driftRisk },
          ].map(s => (
            <div key={s.label}>
              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)' }}>{s.label}</span>
                <span style={{ fontSize: 'var(--sz-xs)', color: riskColor(s.value), fontVariantNumeric: 'tabular-nums', fontWeight: 500 }}>
                  {s.invert ? (s.raw ?? 0).toFixed(2) : s.value.toFixed(2)}
                </span>
              </div>
              <div style={{ height: 4, background: 'var(--color-border-tertiary)', borderRadius: 2, overflow: 'hidden' }}>
                <div style={{ width: `${Math.round(s.value * 100)}%`, height: '100%', background: riskColor(s.value), borderRadius: 2 }} />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

// ── Expanded detail panel ──────────────────────────────────────────────────

function DetailPanel({ row, allRows }: { row: Row; allRows: Row[] }): ReactNode {
  return (
    <tr>
      <td colSpan={12} style={{
        padding: '18px 14px 20px',
        background: 'var(--color-background-primary)',
        borderBottom: '0.5px solid var(--color-border-tertiary)',
      }}>
        {/* Token stats row */}
        <div style={{ display: 'flex', gap: 24, marginBottom: 14, flexWrap: 'wrap' }}>
          {[
            { label: 'Input tokens', value: row.inputTokens.toLocaleString() },
            { label: 'Output tokens', value: row.outputTokens.toLocaleString() },
            { label: 'Total tokens', value: (row.inputTokens + row.outputTokens).toLocaleString() },
            { label: 'Provider', value: row.provider || '—' },
            { label: 'Status', value: String(row.statusCode || '—') },
            { label: 'Session', value: row.sessionId ? row.sessionId.slice(0, 12) + '…' : '—' },
          ].map(stat => (
            <div key={stat.label}>
              <div style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 2 }}>{stat.label}</div>
              <div style={{ fontSize: 'var(--sz-sm)', fontWeight: 500, color: 'var(--color-text-primary)', fontVariantNumeric: 'tabular-nums' }}>{stat.value}</div>
            </div>
          ))}
        </div>

        {/* Masked prompt */}
        <p style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: '0 0 8px' }}>
          Masked Prompt
        </p>
        <div style={{
          background: 'var(--color-background-secondary)', border: '0.5px solid var(--color-border-tertiary)',
          borderRadius: 6, padding: '10px 14px', fontFamily: 'monospace', fontSize: 'var(--sz-base)',
          color: 'var(--color-text-primary)', lineHeight: 1.6, whiteSpace: 'pre-wrap',
        }}>
          {row.prompt || <span style={{ color: 'var(--color-text-tertiary)', fontStyle: 'italic' }}>empty</span>}
        </div>

        {/* Flags */}
        {row.flags.length > 0 && (
          <div style={{ display: 'flex', gap: 6, marginTop: 10, flexWrap: 'wrap' }}>
            {row.flags.map(flag => (
              <span key={flag} style={{
                fontSize: 'var(--sz-label)', fontWeight: 500,
                color: 'var(--color-warning)', background: 'rgba(240,164,41,0.12)',
                border: '0.5px solid rgba(240,164,41,0.2)', padding: '2px 8px', borderRadius: 4,
              }}>{flag}</span>
            ))}
          </div>
        )}

        {/* Two-column layout for risk + latency */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 24, marginTop: 2 }}>
          <div>
            <RiskSignals row={row} />
            <RAGSection row={row} />
            <CrossModelSection row={row} />
          </div>
          <div>
            <LatencyBars row={row} allRows={allRows} />
            <SessionStepTree sessionId={row.sessionId} allRows={allRows} />
          </div>
        </div>
      </td>
    </tr>
  )
}

// ── Metrics summary row ────────────────────────────────────────────────────

function MetricsSummary({ rows }: { rows: Row[] }): ReactNode {
  if (rows.length === 0) return null
  const avgQ  = rows.reduce((s, r) => s + r.quality, 0) / rows.length
  const avgH  = rows.reduce((s, r) => s + r.hallucinationRisk, 0) / rows.length
  const totalCost = rows.reduce((s, r) => s + r.cost, 0)
  const avgLat = Math.round(rows.reduce((s, r) => s + r.latency, 0) / rows.length)
  const highRisk = rows.filter(r => r.riskLevel === 'high').length
  const piiCount = rows.filter(r => r.pii).length

  const stats = [
    { label: 'Traces', value: rows.length.toLocaleString(), color: undefined },
    { label: 'Avg quality', value: avgQ.toFixed(2), color: qualityColor(avgQ) },
    { label: 'Avg hallucination', value: avgH.toFixed(2), color: riskColor(avgH) },
    { label: 'Avg latency', value: `${avgLat}ms`, color: undefined },
    { label: 'Total cost', value: `$${totalCost.toFixed(4)}`, color: undefined },
    { label: 'High risk', value: String(highRisk), color: highRisk > 0 ? 'var(--color-danger)' : undefined },
    { label: 'PII detected', value: String(piiCount), color: piiCount > 0 ? 'var(--color-warning)' : undefined },
  ]

  return (
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
      {stats.map(s => (
        <div key={s.label} style={{
          background: 'var(--color-background-card)', border: '0.5px solid var(--color-border-tertiary)',
          borderRadius: 8, padding: '10px 14px', minWidth: 90,
        }}>
          <div style={{ fontSize: 'var(--sz-label)', color: 'var(--color-text-tertiary)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{s.label}</div>
          <div style={{ fontSize: 'var(--sz-lg)', fontWeight: 600, color: s.color ?? 'var(--color-text-primary)', fontVariantNumeric: 'tabular-nums' }}>{s.value}</div>
        </div>
      ))}
    </div>
  )
}

// ── Page ───────────────────────────────────────────────────────────────────

export default function Traces() {
  const [expanded, setExpanded]   = useState<string | null>(null)
  const [activeFilter, setFilter] = useState<FilterKey>('all')

  const { data = [], isLoading, error } = useQuery<TraceRecord[]>({
    queryKey: ['traces'],
    queryFn: () => fetchJSON('/metrics/traces'),
  })

  const allRows: Row[] = useMemo(
    () => (data.length > 0 ? data.map(fromAPI) : MOCK_ROWS),
    [data],
  )

  const rows = useMemo(() => applyFilter(allRows, activeFilter), [allRows, activeFilter])

  if (isLoading) return <div style={{ padding: 24, color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>Loading…</div>
  if (error)     return <div style={{ padding: 24, color: 'var(--color-danger)', fontSize: 'var(--sz-base)' }}>Failed to load traces</div>

  return (
    <div style={{ padding: 24 }} className="page-enter">

      {/* Top bar */}
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 10 }}>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          {FILTERS.map(f => (
            <button
              key={f.key}
              className={`filter-tab${activeFilter === f.key ? ' active' : ''}`}
              onClick={() => { setFilter(f.key); setExpanded(null) }}
            >
              {f.label}
            </button>
          ))}
        </div>
        <a
          href="http://localhost:8080/export/traces"
          download="ajah-audit-log.csv"
          style={{
            display: 'inline-flex', alignItems: 'center', gap: 6,
            padding: '6px 12px', background: 'rgba(45,125,210,0.1)',
            border: '1px solid rgba(45,125,210,0.25)', borderRadius: 6,
            color: 'var(--color-accent)', fontSize: 'var(--sz-sm)',
            fontWeight: 600, textDecoration: 'none', cursor: 'pointer',
          }}
          onMouseEnter={e => { (e.currentTarget as HTMLAnchorElement).style.background = 'rgba(45,125,210,0.18)' }}
          onMouseLeave={e => { (e.currentTarget as HTMLAnchorElement).style.background = 'rgba(45,125,210,0.1)' }}
        >
          ↓ Export CSV
        </a>
      </div>

      {/* Metrics summary */}
      <MetricsSummary rows={rows} />

      {/* Subtitle */}
      <div style={{ marginBottom: 10, fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)' }}>
        {rows.length} {rows.length === 1 ? 'trace' : 'traces'} — click row to expand
      </div>

      {/* Table */}
      <div style={{ background: 'var(--color-background-secondary)', border: '0.5px solid var(--color-border-tertiary)', borderRadius: 10, overflow: 'hidden' }}>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['Timestamp', 'User', 'Feature', 'Model', 'Cost', 'Latency', 'Quality', 'Hallucination', 'RAG', 'Cross-model', 'PII', 'Risk'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 && (
                <tr>
                  <td colSpan={12} style={{ padding: '40px 14px', textAlign: 'center', color: 'var(--color-text-tertiary)', fontSize: 13 }}>
                    No traces match this filter
                  </td>
                </tr>
              )}

              {rows.map(row => (
                <Fragment key={row.id}>
                  <tr
                    onClick={() => setExpanded(expanded === row.id ? null : row.id)}
                    style={{ cursor: 'pointer', transition: 'background 0.1s', background: expanded === row.id ? 'var(--color-background-primary)' : 'transparent' }}
                    onMouseEnter={e => { if (expanded !== row.id) (e.currentTarget as HTMLTableRowElement).style.background = 'var(--color-background-primary)' }}
                    onMouseLeave={e => { if (expanded !== row.id) (e.currentTarget as HTMLTableRowElement).style.background = 'transparent' }}
                  >
                    {/* Timestamp */}
                    <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12, color: 'var(--color-text-tertiary)' }}>{row.ts}</td>
                    {/* User */}
                    <td style={tdStyle}>{row.user || '—'}</td>
                    {/* Feature */}
                    <td style={{ ...tdStyle, color: 'var(--color-text-primary)' }}>{row.feature || '—'}</td>
                    {/* Model */}
                    <td style={tdStyle}>
                      <span style={{ fontSize: 'var(--sz-xs)', fontWeight: 500, color: 'var(--color-accent)', background: 'var(--color-accent-glow)', padding: '2px 7px', borderRadius: 4 }}>
                        {row.model}
                      </span>
                    </td>
                    {/* Cost */}
                    <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums' }}>${row.cost.toFixed(6)}</td>
                    {/* Latency */}
                    <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums' }}>{row.latency}ms</td>
                    {/* Quality */}
                    <td style={{ ...tdStyle, fontVariantNumeric: 'tabular-nums', color: qualityColor(row.quality), fontWeight: 500 }}>
                      {row.quality.toFixed(2)}
                    </td>
                    {/* Hallucination risk bar */}
                    <td style={{ ...tdStyle, minWidth: 100 }}>
                      <RiskBar value={row.hallucinationRisk} />
                    </td>
                    {/* RAG */}
                    <td style={tdStyle}><RAGBadge verdict={row.ragVerdict} /></td>
                    {/* Cross-model */}
                    <td style={tdStyle}><CrossModelBadge verdict={row.crossModelVerdict} /></td>
                    {/* PII */}
                    <td style={tdStyle}>
                      {row.pii
                        ? <span style={{ fontSize: 'var(--sz-xs)', fontWeight: 500, color: 'var(--color-danger)', background: 'var(--color-danger-glow)', padding: '2px 7px', borderRadius: 4 }}>YES</span>
                        : <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', padding: '2px 7px' }}>NO</span>
                      }
                    </td>
                    {/* Risk level */}
                    <td style={tdStyle}>
                      <span style={{ ...badgeBase, ...riskLevelStyle(row.riskLevel) }}>{row.riskLevel}</span>
                    </td>
                  </tr>

                  {expanded === row.id && <DetailPanel row={row} allRows={allRows} />}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}