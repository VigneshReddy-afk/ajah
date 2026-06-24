import { useState } from 'react'
import type { CSSProperties } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { format } from 'date-fns'
import { fetchJSON, postJSON } from '../api/client'

// ── Types ──────────────────────────────────────────────────────────────────

interface EvalSuite {
  id: string
  name: string
  description: string
  pass_threshold: number
  created_at: string
  updated_at: string
}

interface EvalCase {
  id: string
  suite_id: string
  prompt: string
  expected_output: string
  tags: string
  created_at: string
}

interface EvalResult {
  id: string
  run_id: string
  case_id: string
  prompt: string
  expected_output: string
  actual_output: string
  quality_score: number
  hallucination_risk: number
  passed: boolean
  latency_ms: number
  cost_usd: number
}

interface EvalRun {
  id: string
  suite_id: string
  model: string
  provider: string
  pass_count: number
  fail_count: number
  avg_quality: number
  avg_latency_ms: number
  avg_cost_usd: number
  run_at: string
}

// ── Styles ─────────────────────────────────────────────────────────────────

const card: CSSProperties = {
  background: 'var(--color-background-secondary)',
  border: '0.5px solid var(--color-border-tertiary)',
  borderRadius: 10,
  padding: 20,
}

const th: CSSProperties = {
  padding: '9px 14px',
  fontSize: 'var(--sz-xs)',
  fontWeight: 500,
  color: 'var(--color-text-tertiary)',
  textTransform: 'uppercase',
  letterSpacing: '0.06em',
  textAlign: 'left',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
  whiteSpace: 'nowrap',
}

const td: CSSProperties = {
  padding: '10px 14px',
  fontSize: 'var(--sz-sm)',
  color: 'var(--color-text-secondary)',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
  verticalAlign: 'top',
}

const input: CSSProperties = {
  width: '100%',
  background: 'var(--color-background-primary)',
  border: '0.5px solid var(--color-border-secondary)',
  borderRadius: 6,
  padding: '8px 10px',
  color: 'var(--color-text-primary)',
  fontSize: 'var(--sz-sm)',
  outline: 'none',
}

const btn: CSSProperties = {
  padding: '7px 14px',
  borderRadius: 6,
  border: '0.5px solid var(--color-border-secondary)',
  background: 'transparent',
  color: 'var(--color-text-secondary)',
  fontSize: 'var(--sz-sm)',
  cursor: 'pointer',
  fontWeight: 500,
}

const btnPrimary: CSSProperties = {
  ...btn,
  background: 'var(--color-accent)',
  border: 'none',
  color: '#fff',
}

// ── Helpers ────────────────────────────────────────────────────────────────

function passRate(run: EvalRun): string {
  const total = run.pass_count + run.fail_count
  if (total === 0) return '—'
  return `${Math.round((run.pass_count / total) * 100)}%`
}

function qualityColor(v: number): string {
  if (v >= 0.8) return 'var(--color-success)'
  if (v >= 0.5) return 'var(--color-warning)'
  return 'var(--color-danger)'
}

// ── Sub-views ──────────────────────────────────────────────────────────────

function RunResultsView({ runID, onBack }: { runID: string; onBack: () => void }) {
  const { data, isLoading } = useQuery<{ run: EvalRun; results: EvalResult[] }>({
    queryKey: ['eval-run', runID],
    queryFn: () => fetchJSON(`/evals/runs/${runID}`),
  })

  if (isLoading) return <div style={{ padding: 24, color: 'var(--color-text-tertiary)' }}>Loading results…</div>
  if (!data) return null

  const { run, results } = data
  const total = run.pass_count + run.fail_count

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <button style={btn} onClick={onBack}>← Back</button>
        <span style={{ fontSize: 'var(--sz-sm)', color: 'var(--color-text-tertiary)' }}>
          Run {runID.slice(0, 12)}… · {run.model} · {format(new Date(run.run_at), 'MMM d, HH:mm')}
        </span>
      </div>

      {/* Summary cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: 12 }}>
        {[
          { label: 'Pass rate', value: total > 0 ? `${Math.round((run.pass_count / total) * 100)}%` : '—', color: 'var(--color-success)' },
          { label: 'Passed', value: String(run.pass_count), color: 'var(--color-success)' },
          { label: 'Failed', value: String(run.fail_count), color: run.fail_count > 0 ? 'var(--color-danger)' : 'var(--color-text-primary)' },
          { label: 'Avg quality', value: run.avg_quality.toFixed(2), color: qualityColor(run.avg_quality) },
          { label: 'Avg latency', value: `${Math.round(run.avg_latency_ms)}ms`, color: 'var(--color-text-primary)' },
        ].map(s => (
          <div key={s.label} style={card}>
            <div style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 6 }}>{s.label}</div>
            <div style={{ fontSize: 'var(--sz-lg)', fontWeight: 600, color: s.color }}>{s.value}</div>
          </div>
        ))}
      </div>

      {/* Results table */}
      <div style={{ ...card, padding: 0, overflow: 'hidden' }}>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={th}>#</th>
                <th style={th}>Result</th>
                <th style={{ ...th, width: '25%' }}>Prompt</th>
                <th style={{ ...th, width: '25%' }}>Actual output</th>
                <th style={{ ...th, width: '20%' }}>Expected</th>
                <th style={{ ...th, textAlign: 'right' }}>Quality</th>
                <th style={{ ...th, textAlign: 'right' }}>Hallucination</th>
                <th style={{ ...th, textAlign: 'right' }}>Latency</th>
                <th style={{ ...th, textAlign: 'right' }}>Cost</th>
              </tr>
            </thead>
            <tbody>
              {results.map((res, i) => (
                <tr key={res.id} style={{ background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.012)' }}>
                  <td style={{ ...td, color: 'var(--color-text-tertiary)', fontVariantNumeric: 'tabular-nums' }}>{i + 1}</td>
                  <td style={td}>
                    <span style={{
                      display: 'inline-block', fontSize: 'var(--sz-xs)', fontWeight: 700,
                      padding: '2px 8px', borderRadius: 4,
                      color: res.passed ? 'var(--color-success)' : 'var(--color-danger)',
                      background: res.passed ? 'rgba(34,196,138,0.12)' : 'rgba(239,68,68,0.12)',
                    }}>
                      {res.passed ? '✓ PASS' : '✗ FAIL'}
                    </span>
                  </td>
                  <td style={{ ...td, maxWidth: 220, wordBreak: 'break-word', whiteSpace: 'normal', fontFamily: 'monospace', fontSize: 11 }}>
                    {res.prompt}
                  </td>
                  <td style={{ ...td, maxWidth: 220, wordBreak: 'break-word', whiteSpace: 'normal', fontSize: 12 }}>
                    {res.actual_output || <span style={{ color: 'var(--color-text-tertiary)', fontStyle: 'italic' }}>empty</span>}
                  </td>
                  <td style={{ ...td, maxWidth: 160, wordBreak: 'break-word', whiteSpace: 'normal', color: 'var(--color-text-tertiary)', fontSize: 12 }}>
                    {res.expected_output || '—'}
                  </td>
                  <td style={{ ...td, textAlign: 'right', fontWeight: 600, color: qualityColor(res.quality_score), fontVariantNumeric: 'tabular-nums' }}>
                    {res.quality_score.toFixed(2)}
                  </td>
                  <td style={{ ...td, textAlign: 'right', fontVariantNumeric: 'tabular-nums', color: res.hallucination_risk >= 0.4 ? 'var(--color-danger)' : 'var(--color-text-secondary)' }}>
                    {res.hallucination_risk.toFixed(2)}
                  </td>
                  <td style={{ ...td, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{res.latency_ms}ms</td>
                  <td style={{ ...td, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>${res.cost_usd.toFixed(6)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

function SuiteDetailView({ suite, onBack }: { suite: EvalSuite; onBack: () => void }) {
  const qc = useQueryClient()
  const [viewRunID, setViewRunID] = useState<string | null>(null)
  const [showRunForm, setShowRunForm] = useState(false)
  const [showAddCase, setShowAddCase] = useState(false)
  const [newPrompt, setNewPrompt] = useState('')
  const [newExpected, setNewExpected] = useState('')
  const [newTags, setNewTags] = useState('')
  const [runAPIKey, setRunAPIKey] = useState('')
  const [runModel, setRunModel] = useState('gpt-4o-mini')
  const [runBaseURL, setRunBaseURL] = useState('https://api.openai.com/v1')
  const [running, setRunning] = useState(false)

  const { data: casesData } = useQuery<{ cases: EvalCase[] }>({
    queryKey: ['eval-cases', suite.id],
    queryFn: () => fetchJSON(`/evals/${suite.id}/cases`),
  })

  const { data: runsData } = useQuery<{ runs: EvalRun[] }>({
    queryKey: ['eval-runs', suite.id],
    queryFn: () => fetchJSON(`/evals/${suite.id}/runs`),
    refetchInterval: running ? 3000 : false,
  })

  const addCase = useMutation({
    mutationFn: () => postJSON(`/evals/${suite.id}/cases`, {
      prompt: newPrompt, expected_output: newExpected, tags: newTags,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['eval-cases', suite.id] })
      setNewPrompt(''); setNewExpected(''); setNewTags(''); setShowAddCase(false)
    },
  })

  const deleteCase = useMutation({
    mutationFn: (id: string) => fetch(`http://localhost:8080/evals/${suite.id}/cases/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['eval-cases', suite.id] }),
  })

  const startRun = async () => {
    if (!runAPIKey || !runModel) return
    setRunning(true)
    try {
      await postJSON(`/evals/${suite.id}/run`, { api_key: runAPIKey, model: runModel, base_url: runBaseURL })
      qc.invalidateQueries({ queryKey: ['eval-runs', suite.id] })
      setShowRunForm(false)
    } finally {
      setRunning(false)
    }
  }

  if (viewRunID) {
    return <RunResultsView runID={viewRunID} onBack={() => setViewRunID(null)} />
  }

  const cases = casesData?.cases ?? []
  const runs = runsData?.runs ?? []

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <button style={btn} onClick={onBack}>← Back</button>
          <div>
            <div style={{ fontSize: 'var(--sz-lg)', fontWeight: 600, color: 'var(--color-text-primary)' }}>{suite.name}</div>
            {suite.description && <div style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', marginTop: 2 }}>{suite.description}</div>}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button style={btn} onClick={() => setShowAddCase(v => !v)}>+ Add case</button>
          <button style={btnPrimary} onClick={() => setShowRunForm(v => !v)}>▶ Run suite</button>
        </div>
      </div>

      {/* Add case form */}
      {showAddCase && (
        <div style={{ ...card, display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: 'var(--color-text-primary)', marginBottom: 4 }}>New test case</div>
          <div>
            <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Prompt *</label>
            <textarea
              style={{ ...input, height: 72, resize: 'vertical', fontFamily: 'monospace' }}
              value={newPrompt}
              onChange={e => setNewPrompt(e.target.value)}
              placeholder="Enter the prompt to test…"
            />
          </div>
          <div>
            <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Expected output (optional — used for reference)</label>
            <textarea
              style={{ ...input, height: 56, resize: 'vertical' }}
              value={newExpected}
              onChange={e => setNewExpected(e.target.value)}
              placeholder="What should the model ideally respond with?"
            />
          </div>
          <div>
            <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Tags (comma-separated)</label>
            <input style={input} value={newTags} onChange={e => setNewTags(e.target.value)} placeholder="e.g. factual, safety, tone" />
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button style={btnPrimary} onClick={() => addCase.mutate()} disabled={!newPrompt}>Save case</button>
            <button style={btn} onClick={() => setShowAddCase(false)}>Cancel</button>
          </div>
        </div>
      )}

      {/* Run form */}
      {showRunForm && (
        <div style={{ ...card, display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: 'var(--color-text-primary)', marginBottom: 4 }}>Run suite against a model</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
            <div>
              <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>API key *</label>
              <input style={{ ...input, fontFamily: 'monospace' }} type="password" value={runAPIKey} onChange={e => setRunAPIKey(e.target.value)} placeholder="sk-…" />
            </div>
            <div>
              <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Model *</label>
              <input style={input} value={runModel} onChange={e => setRunModel(e.target.value)} placeholder="gpt-4o-mini" />
            </div>
            <div style={{ gridColumn: '1 / -1' }}>
              <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Base URL</label>
              <input style={input} value={runBaseURL} onChange={e => setRunBaseURL(e.target.value)} />
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <button style={btnPrimary} onClick={startRun} disabled={running || !runAPIKey || cases.length === 0}>
              {running ? '⟳ Running…' : `▶ Run ${cases.length} case${cases.length !== 1 ? 's' : ''}`}
            </button>
            <button style={btn} onClick={() => setShowRunForm(false)}>Cancel</button>
            {cases.length === 0 && <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-warning)' }}>Add at least one case first</span>}
          </div>
        </div>
      )}

      {/* Cases */}
      <div>
        <div style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: 'var(--color-text-secondary)', marginBottom: 8 }}>
          Test cases ({cases.length})
        </div>
        {cases.length === 0 ? (
          <div style={{ ...card, textAlign: 'center', color: 'var(--color-text-tertiary)', padding: 32 }}>
            No cases yet — add your first test case above
          </div>
        ) : (
          <div style={{ ...card, padding: 0, overflow: 'hidden' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={th}>#</th>
                  <th style={{ ...th, width: '40%' }}>Prompt</th>
                  <th style={{ ...th, width: '35%' }}>Expected output</th>
                  <th style={th}>Tags</th>
                  <th style={th}></th>
                </tr>
              </thead>
              <tbody>
                {cases.map((c, i) => (
                  <tr key={c.id} style={{ background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.012)' }}>
                    <td style={{ ...td, color: 'var(--color-text-tertiary)', fontVariantNumeric: 'tabular-nums' }}>{i + 1}</td>
                    <td style={{ ...td, fontFamily: 'monospace', fontSize: 11, whiteSpace: 'pre-wrap', wordBreak: 'break-word', maxWidth: 320 }}>{c.prompt}</td>
                    <td style={{ ...td, fontSize: 12, color: 'var(--color-text-tertiary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word', maxWidth: 260 }}>
                      {c.expected_output || <span style={{ fontStyle: 'italic' }}>—</span>}
                    </td>
                    <td style={td}>
                      {c.tags ? c.tags.split(',').map(t => (
                        <span key={t} style={{ display: 'inline-block', fontSize: 10, padding: '1px 6px', borderRadius: 3, background: 'var(--color-accent-glow)', color: 'var(--color-accent)', marginRight: 4, marginBottom: 2 }}>{t.trim()}</span>
                      )) : '—'}
                    </td>
                    <td style={td}>
                      <button
                        style={{ ...btn, padding: '3px 8px', fontSize: 11, color: 'var(--color-danger)' }}
                        onClick={() => deleteCase.mutate(c.id)}
                      >Delete</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Run history */}
      {runs.length > 0 && (
        <div>
          <div style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: 'var(--color-text-secondary)', marginBottom: 8 }}>Run history</div>
          <div style={{ ...card, padding: 0, overflow: 'hidden' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={th}>Run at</th>
                  <th style={th}>Model</th>
                  <th style={{ ...th, textAlign: 'right' }}>Pass rate</th>
                  <th style={{ ...th, textAlign: 'right' }}>Passed</th>
                  <th style={{ ...th, textAlign: 'right' }}>Failed</th>
                  <th style={{ ...th, textAlign: 'right' }}>Avg quality</th>
                  <th style={{ ...th, textAlign: 'right' }}>Avg latency</th>
                  <th style={th}></th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run, i) => (
                  <tr key={run.id} style={{ background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.012)' }}>
                    <td style={{ ...td, whiteSpace: 'nowrap' }}>{format(new Date(run.run_at), 'MMM d, HH:mm')}</td>
                    <td style={{ ...td }}>
                      <span style={{ fontSize: 'var(--sz-xs)', fontWeight: 500, color: 'var(--color-accent)', background: 'var(--color-accent-glow)', padding: '2px 7px', borderRadius: 4 }}>{run.model}</span>
                    </td>
                    <td style={{ ...td, textAlign: 'right', fontWeight: 700, color: (run.pass_count / (run.pass_count + run.fail_count)) >= 0.8 ? 'var(--color-success)' : 'var(--color-warning)' }}>
                      {passRate(run)}
                    </td>
                    <td style={{ ...td, textAlign: 'right', color: 'var(--color-success)', fontVariantNumeric: 'tabular-nums' }}>{run.pass_count}</td>
                    <td style={{ ...td, textAlign: 'right', color: run.fail_count > 0 ? 'var(--color-danger)' : 'var(--color-text-tertiary)', fontVariantNumeric: 'tabular-nums' }}>{run.fail_count}</td>
                    <td style={{ ...td, textAlign: 'right', color: qualityColor(run.avg_quality), fontVariantNumeric: 'tabular-nums' }}>{run.avg_quality.toFixed(2)}</td>
                    <td style={{ ...td, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{Math.round(run.avg_latency_ms)}ms</td>
                    <td style={td}>
                      <button style={{ ...btn, padding: '3px 10px', fontSize: 11 }} onClick={() => setViewRunID(run.id)}>View →</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

// ── Main page ──────────────────────────────────────────────────────────────

export default function Evals() {
  const qc = useQueryClient()
  const [selectedSuite, setSelectedSuite] = useState<EvalSuite | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState('')
  const [newDesc, setNewDesc] = useState('')
  const [newThreshold, setNewThreshold] = useState('0.7')

  const { data, isLoading } = useQuery<{ suites: EvalSuite[] }>({
    queryKey: ['eval-suites'],
    queryFn: () => fetchJSON('/evals'),
  })

  const createSuite = useMutation({
    mutationFn: () => postJSON('/evals', {
      name: newName,
      description: newDesc,
      pass_threshold: parseFloat(newThreshold) || 0.7,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['eval-suites'] })
      setNewName(''); setNewDesc(''); setNewThreshold('0.7'); setShowCreate(false)
    },
  })

  const deleteSuite = useMutation({
    mutationFn: (id: string) => fetch(`http://localhost:8080/evals/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['eval-suites'] }),
  })

  if (selectedSuite) {
    return (
      <div style={{ padding: 24 }}>
        <SuiteDetailView suite={selectedSuite} onBack={() => setSelectedSuite(null)} />
      </div>
    )
  }

  const suites = data?.suites ?? []

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 16 }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div>
          <div style={{ fontSize: 'var(--sz-base)', color: 'var(--color-text-tertiary)' }}>
            Run your prompts against golden datasets. Track quality, hallucination risk, and pass rates across models.
          </div>
        </div>
        <button style={btnPrimary} onClick={() => setShowCreate(v => !v)}>+ New suite</button>
      </div>

      {/* Create suite form */}
      {showCreate && (
        <div style={{ ...card, display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div style={{ fontSize: 'var(--sz-sm)', fontWeight: 600, color: 'var(--color-text-primary)', marginBottom: 4 }}>New eval suite</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
            <div>
              <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Suite name *</label>
              <input style={input} value={newName} onChange={e => setNewName(e.target.value)} placeholder="e.g. RAG accuracy suite" />
            </div>
            <div>
              <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Pass threshold (0–1)</label>
              <input style={input} type="number" min="0" max="1" step="0.05" value={newThreshold} onChange={e => setNewThreshold(e.target.value)} />
            </div>
            <div style={{ gridColumn: '1 / -1' }}>
              <label style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', display: 'block', marginBottom: 4 }}>Description</label>
              <input style={input} value={newDesc} onChange={e => setNewDesc(e.target.value)} placeholder="Optional description" />
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button style={btnPrimary} onClick={() => createSuite.mutate()} disabled={!newName}>Create suite</button>
            <button style={btn} onClick={() => setShowCreate(false)}>Cancel</button>
          </div>
        </div>
      )}

      {/* Suite list */}
      {isLoading ? (
        <div style={{ color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>Loading…</div>
      ) : suites.length === 0 ? (
        <div style={{ ...card, textAlign: 'center', padding: 48, color: 'var(--color-text-tertiary)' }}>
          <div style={{ fontSize: 32, marginBottom: 12 }}>🧪</div>
          <div style={{ fontSize: 'var(--sz-sm)', marginBottom: 6 }}>No eval suites yet</div>
          <div style={{ fontSize: 'var(--sz-xs)' }}>Create a suite, add test cases, then run against any model</div>
        </div>
      ) : (
        <div style={{ ...card, padding: 0, overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={th}>Suite name</th>
                <th style={th}>Description</th>
                <th style={{ ...th, textAlign: 'right' }}>Pass threshold</th>
                <th style={th}>Created</th>
                <th style={th}></th>
              </tr>
            </thead>
            <tbody>
              {suites.map((suite, i) => (
                <tr
                  key={suite.id}
                  style={{ background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.012)', cursor: 'pointer' }}
                  onClick={() => setSelectedSuite(suite)}
                >
                  <td style={{ ...td, color: 'var(--color-text-primary)', fontWeight: 500 }}>{suite.name}</td>
                  <td style={{ ...td, color: 'var(--color-text-tertiary)' }}>{suite.description || '—'}</td>
                  <td style={{ ...td, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{suite.pass_threshold.toFixed(2)}</td>
                  <td style={{ ...td, whiteSpace: 'nowrap' }}>{format(new Date(suite.created_at), 'MMM d, yyyy')}</td>
                  <td style={td} onClick={e => e.stopPropagation()}>
                    <button
                      style={{ ...btn, padding: '3px 8px', fontSize: 11, color: 'var(--color-danger)' }}
                      onClick={() => deleteSuite.mutate(suite.id)}
                    >Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
