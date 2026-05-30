import { useEffect, useRef, useState, useMemo, type CSSProperties } from 'react'
import '../lib/pulsar-engine'
import type { PulsarEvent } from '../types/pulsar'
import type { TraceRecord } from '../api/types'

// ── Helpers ───────────────────────────────────────────────────────────────────
const pad2 = (n: number) => String(n).padStart(2, '0')
function hms(ts: number) {
  const d = new Date(ts)
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}
function qColor(q: number) {
  if (q <= 0) return 'var(--color-text-tertiary)'
  if (q >= 0.8) return '#27BE8E'
  if (q >= 0.5) return '#D7A23E'
  return '#E36B42'
}

const ACCENT = '#185FA5'
const card: CSSProperties = {
  background: 'var(--color-background-secondary)',
  border: '0.5px solid var(--color-border-tertiary)',
  borderRadius: 10,
}

// ── Trace → PulsarEvent mapping ───────────────────────────────────────────────
let _autoId = 0

function traceToEvent(t: TraceRecord): PulsarEvent {
  const rag = t.rag_verdict || 'not_applicable'
  const quality = t.quality_score || 0
  return {
    id: ++_autoId,
    t: Date.now(),
    feature: t.feature_name || 'unknown',
    fconf: {
      name: t.feature_name || 'unknown',
      w: 20,
      lat: t.latency_ms || 1000,
      cost: t.cost_usd || 0,
    },
    model: t.model || 'unknown',
    latency: t.latency_ms || 0,
    cost: t.cost_usd || 0,
    quality,
    pii: t.was_pii_masked === true,
    rag,
    flare: rag === 'contradicted' || (quality > 0 && quality < 0.45),
  }
}

// ── Sparkline ─────────────────────────────────────────────────────────────────
function Spark({ data, color = ACCENT, h = 28 }: { data: number[]; color?: string; h?: number }) {
  if (!data || data.length < 2) return <div style={{ height: h }} />
  const max = Math.max(...data, 1e-9)
  const min = Math.min(...data, 0)
  const n = data.length
  const W = 100
  const pts = data.map((v, i) => {
    const x = (i / (n - 1)) * W
    const y = h - ((v - min) / ((max - min) || 1)) * (h - 4) - 2
    return [x, y]
  })
  const line = pts.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1)).join(' ')
  const area = line + ` L ${W} ${h} L 0 ${h} Z`
  return (
    <svg viewBox={`0 0 ${W} ${h}`} preserveAspectRatio="none" style={{ width: '100%', height: h, display: 'block' }}>
      <path d={area} fill={color} opacity="0.12" />
      <path d={line} fill="none" stroke={color} strokeWidth="1.4" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  )
}

// ── Stat card ─────────────────────────────────────────────────────────────────
function Stat({ label, value, sub, spark, sparkColor, valueColor }: {
  label: string; value: string | number; sub: string
  spark: number[]; sparkColor?: string; valueColor?: string
}) {
  return (
    <div style={{ ...card, padding: '13px 15px 8px', display: 'flex', flexDirection: 'column', gap: 2, overflow: 'hidden' }}>
      <span style={{ fontSize: 10, color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.07em', fontWeight: 600 }}>{label}</span>
      <span style={{ fontSize: 25, fontWeight: 600, color: valueColor ?? 'var(--color-text-primary)', fontVariantNumeric: 'tabular-nums', lineHeight: 1.15, letterSpacing: '-0.01em' }}>{value}</span>
      <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 8 }}>
        <span style={{ fontSize: 11, color: 'var(--color-text-tertiary)', whiteSpace: 'nowrap', paddingBottom: 4 }}>{sub}</span>
        <div style={{ width: 78, flexShrink: 0 }}><Spark data={spark} color={sparkColor} /></div>
      </div>
    </div>
  )
}

// ── Legend ────────────────────────────────────────────────────────────────────
function Dot({ c, ring }: { c: string; ring?: boolean }) {
  return ring
    ? <span style={{ width: 11, height: 11, borderRadius: '50%', border: `1.2px dashed ${c}`, display: 'inline-block', flexShrink: 0 }} />
    : <span style={{ width: 9, height: 9, borderRadius: '50%', background: c, display: 'inline-block', flexShrink: 0, boxShadow: `0 0 7px ${c}b3` }} />
}
function LRow({ children, swatch }: { children: React.ReactNode; swatch: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 9, fontSize: 12, color: 'var(--color-text-secondary)' }}>
      <span style={{ width: 12, display: 'flex', justifyContent: 'center' }}>{swatch}</span>
      {children}
    </div>
  )
}
function Legend() {
  const meta: CSSProperties = { fontSize: 10.5, color: 'var(--color-text-tertiary)' }
  return (
    <div style={{ ...card, padding: '13px 15px 14px', flexShrink: 0 }}>
      <div style={{ fontSize: 10, color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.07em', fontWeight: 600, marginBottom: 11 }}>Signal key</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <LRow swatch={<Dot c="#27BE8E" />}>Grounded · quality ≥ 0.80</LRow>
        <LRow swatch={<Dot c="#D7A23E" />}>Caution · 0.50 – 0.80</LRow>
        <LRow swatch={<Dot c="#E36B42" />}>At risk · &lt; 0.50</LRow>
        <LRow swatch={<Dot c="#C8D0E6" ring />}>PII masked before storage</LRow>
        <LRow swatch={<span style={{ fontSize: 11, color: '#D8483A' }}>▲</span>}>Flare · RAG contradiction</LRow>
      </div>
      <div style={{ marginTop: 12, paddingTop: 11, borderTop: '0.5px solid var(--color-border-tertiary)', display: 'flex', flexDirection: 'column', gap: 4 }}>
        <div style={meta}><b style={{ color: 'var(--color-text-secondary)', fontWeight: 600 }}>Angle</b> — feature sector</div>
        <div style={meta}><b style={{ color: 'var(--color-text-secondary)', fontWeight: 600 }}>Distance</b> — latency (slower travels farther)</div>
        <div style={meta}><b style={{ color: 'var(--color-text-secondary)', fontWeight: 600 }}>Size</b> — cost of the call</div>
      </div>
    </div>
  )
}

// ── Ticker ────────────────────────────────────────────────────────────────────
function TickerRow({ e }: { e: PulsarEvent }) {
  return (
    <div style={{
      display: 'grid', gridTemplateColumns: '62px 1fr auto', alignItems: 'center', gap: 8,
      padding: '7px 14px', borderBottom: '0.5px solid var(--color-border-tertiary)',
      borderLeft: `2px solid ${e.flare ? '#D8483A' : 'transparent'}`,
    }}>
      <span style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 11, color: 'var(--color-text-tertiary)' }}>
        {hms(e.t)}
      </span>
      <div style={{ display: 'flex', alignItems: 'center', gap: 7, minWidth: 0 }}>
        <Dot c={qColor(e.quality)} />
        <span style={{ fontSize: 12, color: 'var(--color-text-primary)', fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{e.feature}</span>
        <span style={{ fontSize: 10.5, color: '#3F86CF', background: 'rgba(45,120,201,0.12)', padding: '1px 6px', borderRadius: 4, whiteSpace: 'nowrap' }}>{e.model}</span>
        {e.pii && <span style={{ fontSize: 9.5, color: '#C8D0E6', border: '0.5px dashed rgba(200,208,230,0.5)', padding: '0px 5px', borderRadius: 4, letterSpacing: '0.04em' }}>PII</span>}
        {e.flare && <span style={{ fontSize: 9.5, color: '#E07A57', background: 'rgba(216,72,58,0.14)', padding: '1px 5px', borderRadius: 4, whiteSpace: 'nowrap' }}>{e.rag === 'contradicted' ? 'contradict' : 'low q'}</span>}
      </div>
      <span style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 11, color: 'var(--color-text-secondary)', fontVariantNumeric: 'tabular-nums' }}>
        ${e.cost.toFixed(4)}
      </span>
    </div>
  )
}

function Ticker({ rows }: { rows: PulsarEvent[] }) {
  return (
    <div style={{ ...card, flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 15px 11px', borderBottom: '0.5px solid var(--color-border-tertiary)', flexShrink: 0 }}>
        <span style={{ fontSize: 10, color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.07em', fontWeight: 600 }}>Latest calls</span>
        <span style={{ fontSize: 10, color: 'var(--color-text-tertiary)' }}>newest first</span>
      </div>
      <div style={{ flex: 1, overflowY: 'auto', WebkitMaskImage: 'linear-gradient(180deg,#000 88%,transparent)' }}>
        {rows.map((e) => <TickerRow key={e.id} e={e} />)}
      </div>
    </div>
  )
}

// ── Stats shape ───────────────────────────────────────────────────────────────
interface Stats {
  calls: number; cost: number; lat: number; qual: number; flares: number
  sCalls: number[]; sCost: number[]; sLat: number[]; sQual: number[]
}
const EMPTY_STATS: Stats = {
  calls: 0, cost: 0, lat: 0, qual: 0, flares: 0,
  sCalls: [], sCost: [], sLat: [], sQual: [],
}

// ── Main page ─────────────────────────────────────────────────────────────────
export default function Pulsar() {
  const canvasRef    = useRef<HTMLCanvasElement>(null)
  const engineRef    = useRef<InstanceType<typeof window.PulsarField> | null>(null)
  const bufRef       = useRef<PulsarEvent[]>([])
  const seenIds      = useRef<Set<string>>(new Set())
  const lastRealTime = useRef(0)

  const [paused,     setPaused]    = useState(false)
  const [ticker,     setTicker]    = useState<PulsarEvent[]>([])
  const [stats,      setStats]     = useState<Stats>(EMPTY_STATS)
  const [isLiveData, setIsLiveData] = useState(false)

  const dateStr = useMemo(() =>
    new Date().toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }),
  [])

  useEffect(() => {
    if (!canvasRef.current) return

    // Engine starts with rate:0 — simulation never fires
    const eng = new window.PulsarField(canvasRef.current)
    engineRef.current = eng
    eng.setConfig({ liveliness: 7, rate: 0, accent: ACCENT, sectors: true, paused: false })
    eng.on((evt: PulsarEvent) => {
      bufRef.current.push(evt)
      setTicker((prev) => [evt, ...prev].slice(0, 16))
    })
    eng.start()

    // Stats rolling update — 60s window, 30 buckets
    const statsTimer = setInterval(() => {
      const now = Date.now()
      const cut = now - 60_000
      bufRef.current = bufRef.current.filter((e) => e.t >= cut)
      const buf = bufRef.current
      const n = buf.length
      const cost = buf.reduce((s, e) => s + e.cost, 0)
      const lat = n ? buf.reduce((s, e) => s + e.latency, 0) / n : 0
      const qv = buf.filter((e) => e.quality > 0)
      const qual = qv.length ? qv.reduce((s, e) => s + e.quality, 0) / qv.length : 0
      const flares = buf.filter((e) => e.flare).length
      const B = 30, span = 60_000 / B
      const cC = Array(B).fill(0), cS = Array(B).fill(0)
      const lS = Array(B).fill(0), lN = Array(B).fill(0)
      const qS = Array(B).fill(0), qN = Array(B).fill(0)
      for (const e of buf) {
        let bi = Math.floor((e.t - cut) / span)
        bi = Math.max(0, Math.min(bi, B - 1))
        cC[bi]++; cS[bi] += e.cost
        lS[bi] += e.latency; lN[bi]++
        if (e.quality > 0) { qS[bi] += e.quality; qN[bi]++ }
      }
      setStats({
        calls: n, cost, lat, qual, flares,
        sCalls: cC, sCost: cS,
        sLat: lS.map((s, i) => (lN[i] ? s / lN[i] : 0)),
        sQual: qS.map((s, i) => (qN[i] ? s / qN[i] : 0)),
      })
    }, 650)

    // Live-data indicator — check every 2s
    const liveTimer = setInterval(() => {
      setIsLiveData(Date.now() - lastRealTime.current < 30_000)
    }, 2_000)

    // Real data polling — every 2 seconds
    const poll = async () => {
      try {
        const res = await fetch('http://localhost:8080/metrics/traces')
        if (!res.ok) return
        const traces: TraceRecord[] = await res.json()
        if (traces.length > 0) lastRealTime.current = Date.now()
        for (const t of traces) {
          if (seenIds.current.has(t.request_id)) continue
          seenIds.current.add(t.request_id)
          eng.emit(traceToEvent(t))
        }
      } catch {
        // gateway unreachable — field stays empty
      }
    }
    poll()
    const pollTimer = setInterval(poll, 2_000)

    return () => {
      eng.stop()
      clearInterval(statsTimer)
      clearInterval(liveTimer)
      clearInterval(pollTimer)
    }
  }, [])

  useEffect(() => {
    engineRef.current?.setConfig({ paused })
  }, [paused])

  // Badge appearance
  const badge = paused
    ? { label: 'Paused',           dot: false, color: 'var(--color-text-tertiary)', bg: 'var(--color-background-primary)', pulse: false }
    : isLiveData
    ? { label: 'Live data',        dot: true,  color: ACCENT,                       bg: 'rgba(24,95,165,0.13)',             pulse: true  }
    : { label: 'Waiting for data', dot: true,  color: 'var(--color-text-tertiary)', bg: 'var(--color-background-primary)', pulse: false }

  const hasData = ticker.length > 0

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden', padding: '14px 20px 16px', gap: 12 }}>

      {/* ── Controls bar ── */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexShrink: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{
            display: 'inline-flex', alignItems: 'center', gap: 6,
            fontSize: 11, fontWeight: 500,
            color: badge.color,
            background: badge.bg,
            padding: '3px 9px 3px 8px', borderRadius: 5,
          }}>
            {badge.dot && (
              <span style={{
                width: 6, height: 6, borderRadius: '50%', flexShrink: 0,
                background: badge.color,
                ...(badge.pulse ? { animation: 'pulsarDot 1.4s ease-in-out infinite' } : {}),
              }} />
            )}
            {badge.label}
          </span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
          <button
            onClick={() => setPaused((p) => !p)}
            style={{
              display: 'flex', alignItems: 'center', gap: 6, height: 28, padding: '0 11px',
              borderRadius: 6, cursor: 'pointer',
              border: '1px solid var(--color-border-tertiary)',
              background: 'transparent', color: 'var(--color-text-secondary)',
              fontSize: 12, fontWeight: 500, fontFamily: 'inherit',
            }}
          >
            {paused ? '▶ Resume' : '⏸ Pause'}
          </button>
          <span style={{ fontSize: 12, color: 'var(--color-text-tertiary)' }}>{dateStr}</span>
        </div>
      </div>

      {/* ── Stat cards ── */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 12, flexShrink: 0 }}>
        <Stat label="Calls / min" value={stats.calls} sub="last 60 s" spark={stats.sCalls} sparkColor={ACCENT} />
        <Stat label="Spend / min" value={'$' + stats.cost.toFixed(4)} sub="rolling" spark={stats.sCost} sparkColor="#3F86CF" />
        <Stat label="Avg latency" value={Math.round(stats.lat) + ' ms'} sub="per call" spark={stats.sLat} sparkColor="#8A6FD6" />
        <Stat
          label="Avg quality"
          value={stats.qual ? stats.qual.toFixed(2) : '—'}
          sub="excl. empty"
          spark={stats.sQual}
          sparkColor="#27BE8E"
          valueColor={stats.qual ? qColor(stats.qual) : undefined}
        />
      </div>

      {/* ── Canvas + right rail ── */}
      <div style={{ flex: 1, display: 'flex', gap: 14, minHeight: 0 }}>

        {/* Canvas field */}
        <div style={{
          ...card,
          flex: 1, position: 'relative', overflow: 'hidden', minWidth: 0,
          background: 'radial-gradient(120% 120% at 50% 46%, #181D29 0%, var(--color-background-secondary) 62%)',
        }}>
          <canvas ref={canvasRef} style={{ position: 'absolute', inset: 0 }} />

          {/* Empty state overlay — hidden once real data arrives */}
          {!hasData && (
            <div style={{
              position: 'absolute', inset: 0,
              display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
              pointerEvents: 'none',
            }}>
              <div style={{ fontSize: 13, color: 'var(--color-text-tertiary)' }}>Waiting for LLM calls</div>
              <div style={{ fontSize: 11, color: 'var(--color-text-tertiary)', marginTop: 6 }}>Send a request through the gateway</div>
            </div>
          )}

          <div style={{ position: 'absolute', top: 14, left: 16, pointerEvents: 'none' }}>
            <div style={{ fontSize: 10, color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.09em', fontWeight: 600 }}>Live pulse field</div>
            <div style={{ fontSize: 11, color: 'var(--color-text-tertiary)', marginTop: 3, opacity: 0.8 }}>every ring is one model call</div>
          </div>
          <div style={{ position: 'absolute', top: 14, right: 16, pointerEvents: 'none' }}>
            <span style={{
              fontSize: 11,
              color: stats.flares > 0 ? '#E07A57' : 'var(--color-text-tertiary)',
              background: stats.flares > 0 ? 'rgba(216,72,58,0.12)' : 'var(--color-background-primary)',
              border: `0.5px solid ${stats.flares > 0 ? 'rgba(216,72,58,0.3)' : 'var(--color-border-tertiary)'}`,
              padding: '3px 9px', borderRadius: 5, fontWeight: 500,
            }}>
              {stats.flares > 0 ? '▲ ' : ''}{stats.flares} flag{stats.flares === 1 ? '' : 's'} · 60s
            </span>
          </div>
          <div style={{ position: 'absolute', bottom: 13, left: 16, fontSize: 10, color: 'var(--color-text-tertiary)', opacity: 0.7, pointerEvents: 'none' }}>
            inner ring = fast · outer ring = slow
          </div>
        </div>

        {/* Right rail */}
        <div style={{ width: 322, flexShrink: 0, display: 'flex', flexDirection: 'column', gap: 14, minHeight: 0 }}>
          <Legend />
          <Ticker rows={ticker} />
        </div>
      </div>

      <style>{`
        @keyframes pulsarDot {
          0%, 100% { opacity: 1; transform: scale(1); }
          50% { opacity: 0.4; transform: scale(0.75); }
        }
      `}</style>
    </div>
  )
}
