import type { CSSProperties } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell, Sector,
  LineChart, Line, CartesianGrid,
} from 'recharts'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { CostMetrics, TraceRecord } from '../api/types'

// ── Constants ──────────────────────────────────────────────────────────────

const MODEL_COLORS = ['#185FA5', '#1D9E75', '#378ADD', '#D85A30', '#BA7517']

const MOCK_FEATURE = [
  { name: 'chat',      cost: 0.00384 },
  { name: 'summarize', cost: 0.00414 },
  { name: 'classify',  cost: 0.00120 },
  { name: 'translate', cost: 0.00089 },
]
const MOCK_MODEL = [
  { name: 'gpt-4o',        value: 0.00414 },
  { name: 'llama-3.3-70b', value: 0.00242 },
  { name: 'claude-3-5',    value: 0.00120 },
  { name: 'gemini-1.5',    value: 0.00168 },
  { name: 'mistral',       value: 0.00089 },
]
const MOCK_QUALITY = [
  { hour: '08:00', avg: 0.855 },
  { hour: '09:00', avg: 0.905 },
  { hour: '10:00', avg: 0.920 },
  { hour: '11:00', avg: 0.875 },
  { hour: '12:00', avg: 0.450 },
  { hour: '13:00', avg: 0.900 },
]

const card: CSSProperties = {
  background: 'var(--color-background-secondary)',
  border: '0.5px solid var(--color-border-tertiary)',
  borderRadius: 10,
  padding: 20,
}

const chartTooltip = {
  contentStyle: {
    background: '#111318',
    border: '0.5px solid rgba(255,255,255,0.06)',
    borderRadius: 8,
    fontSize: 12,
    color: '#E3E6EF',
    padding: '8px 12px',
  },
  labelStyle: { color: '#8A92A6', fontSize: 11, marginBottom: 4 },
  cursor: { fill: 'rgba(255,255,255,0.025)' },
}

const pieTooltip = {
  contentStyle: {
    background: '#1E2130',
    border: '1px solid rgba(255,255,255,0.18)',
    borderRadius: 8,
    fontSize: 13,
    color: '#E3E6EF',
    padding: '10px 14px',
    boxShadow: '0 8px 28px rgba(0,0,0,0.65)',
  },
  labelStyle: { color: '#8A92A6', fontSize: 11, marginBottom: 4 },
  itemStyle: { color: '#E3E6EF', fontWeight: 500 },
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function PieActiveShape(props: any) {
  const { cx, cy, innerRadius, outerRadius, startAngle, endAngle, fill } = props
  return (
    <g>
      <Sector cx={cx} cy={cy} innerRadius={innerRadius} outerRadius={outerRadius + 7}
        startAngle={startAngle} endAngle={endAngle} fill={fill} opacity={0.95} />
      <Sector cx={cx} cy={cy} innerRadius={outerRadius + 10} outerRadius={outerRadius + 13}
        startAngle={startAngle} endAngle={endAngle} fill={fill} opacity={0.35} />
    </g>
  )
}

// ── Helpers ────────────────────────────────────────────────────────────────

function buildQualityTrend(traces: TraceRecord[]) {
  const byHour: Record<string, number[]> = {}
  for (const t of traces) {
    if (t.quality_score === 0) continue
    const label = format(new Date(t.timestamp), 'HH:00')
    if (!byHour[label]) byHour[label] = []
    byHour[label].push(t.quality_score)
  }
  return Object.entries(byHour)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([hour, scores]) => ({
      hour,
      avg: +(scores.reduce((s, v) => s + v, 0) / scores.length).toFixed(4),
    }))
}

// ── Sub-components ─────────────────────────────────────────────────────────

function StatCard({
  label,
  value,
  sub,
  danger,
}: {
  label: string
  value: string
  sub: string
  danger?: boolean
}) {
  return (
    <div style={card}>
      <p style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.06em', margin: 0 }}>
        {label}
      </p>
      <p style={{
        fontSize: 'var(--sz-stat)', fontWeight: 600, margin: '10px 0 6px',
        color: danger ? '#A32D2D' : 'var(--color-text-primary)',
        fontVariantNumeric: 'tabular-nums',
      }}>
        {value}
      </p>
      <p style={{ fontSize: 'var(--sz-sm)', color: danger ? '#A32D2D' : 'var(--color-text-tertiary)', margin: 0 }}>
        {sub}
      </p>
    </div>
  )
}

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ ...card, padding: '20px 20px 16px' }}>
      <p style={{ fontSize: 'var(--sz-sm)', fontWeight: 500, color: 'var(--color-text-secondary)', margin: '0 0 16px', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
        {title}
      </p>
      {children}
    </div>
  )
}

function EmptyChart({ msg = 'No data yet' }: { msg?: string }) {
  return (
    <div style={{ height: 200, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <span style={{ fontSize: 'var(--sz-base)', color: 'var(--color-text-tertiary)' }}>{msg}</span>
    </div>
  )
}

// ── Page ───────────────────────────────────────────────────────────────────

export default function Overview() {
  const { data, isLoading } = useQuery<CostMetrics>({
    queryKey: ['metrics'],
    queryFn: () => fetchJSON('/metrics/cost'),
  })
  const { data: traces = [] } = useQuery<TraceRecord[]>({
    queryKey: ['traces'],
    queryFn: () => fetchJSON('/metrics/traces'),
  })

  if (isLoading) {
    return (
      <div style={{ padding: 24, color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>Loading…</div>
    )
  }

  const totalCost = data ? Object.values(data.by_user).reduce((a, b) => a + b, 0) : 0
  const featureCount = data ? Object.keys(data.by_feature).length : 0

  const featureData = data && Object.keys(data.by_feature).length > 0
    ? Object.entries(data.by_feature).map(([name, cost]) => ({ name, cost: +cost.toFixed(6) }))
    : MOCK_FEATURE

  const modelData = data && Object.keys(data.by_model).length > 0
    ? Object.entries(data.by_model).map(([name, value]) => ({ name, value: +value.toFixed(6) }))
    : MOCK_MODEL

  const rawTrend = buildQualityTrend(traces)
  const qualityTrend = rawTrend.length > 0 ? rawTrend : MOCK_QUALITY

  const piiCount = data?.pii_masked_count ?? 0
  const totalTraces = data?.total_traces ?? 0

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 20 }}>

      {/* ── Stat cards ── */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3,1fr)', gap: 16 }}>
        <StatCard
          label="Total cost today"
          value={`$${totalCost > 0 ? totalCost.toFixed(5) : '0.01074'}`}
          sub="+12% vs yesterday"
        />
        <StatCard
          label="Total traces"
          value={totalTraces > 0 ? totalTraces.toLocaleString() : '24'}
          sub={featureCount > 0 ? `across ${featureCount} features` : 'across 5 features'}
        />
        <StatCard
          label="PII detections"
          value={piiCount > 0 ? piiCount.toLocaleString() : '3'}
          sub="masked before storage"
          danger={piiCount > 0 || true}
        />
      </div>

      {/* ── Charts row ── */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>

        {/* Cost by Feature */}
        <ChartCard title="Cost by Feature">
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={featureData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
              <XAxis dataKey="name" tick={{ fill: '#505867', fontSize: 11 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: '#505867', fontSize: 11 }} axisLine={false} tickLine={false} tickFormatter={v => `$${v}`} width={50} />
              <Tooltip {...chartTooltip} formatter={(v: number) => [`$${v}`, 'Cost']} />
              <Bar dataKey="cost" fill="#185FA5" radius={[4, 4, 0, 0]} maxBarSize={36} />
            </BarChart>
          </ResponsiveContainer>
        </ChartCard>

        {/* Cost by Model (donut) */}
        <ChartCard title="Cost by Model">
          {modelData.length === 0 ? <EmptyChart /> : (
            <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
              <ResponsiveContainer width="55%" height={200}>
                <PieChart>
                  <Pie
                    data={modelData}
                    dataKey="value"
                    nameKey="name"
                    cx="50%" cy="50%"
                    innerRadius={52} outerRadius={78}
                    paddingAngle={2}
                    activeShape={PieActiveShape}
                  >
                    {modelData.map((_, i) => (
                      <Cell key={i} fill={MODEL_COLORS[i % MODEL_COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip
                    {...pieTooltip}
                    formatter={(v: number) => [`$${v.toFixed(6)}`, 'Cost']}
                  />
                </PieChart>
              </ResponsiveContainer>
              <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 7 }}>
                {modelData.map((m, i) => (
                  <div key={m.name} style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                    <div style={{ width: 8, height: 8, borderRadius: 2, background: MODEL_COLORS[i % MODEL_COLORS.length], flexShrink: 0 }} />
                    <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-secondary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{m.name}</span>
                    <span style={{ fontSize: 'var(--sz-xs)', color: 'var(--color-text-tertiary)', fontVariantNumeric: 'tabular-nums' }}>${m.value.toFixed(5)}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </ChartCard>
      </div>

      {/* ── Quality Trend (full width) ── */}
      <ChartCard title="Output Quality Trend">
        <ResponsiveContainer width="100%" height={200}>
          <LineChart data={qualityTrend} margin={{ top: 4, right: 16, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.04)" vertical={false} />
            <XAxis dataKey="hour" tick={{ fill: '#505867', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis
              domain={[0, 1]}
              tick={{ fill: '#505867', fontSize: 11 }}
              axisLine={false} tickLine={false}
              tickFormatter={v => v.toFixed(1)}
              width={32}
            />
            <Tooltip
              {...chartTooltip}
              formatter={(v: number) => [v.toFixed(4), 'Avg quality']}
            />
            <Line
              type="monotone"
              dataKey="avg"
              stroke="#1D9E75"
              strokeWidth={2}
              dot={{ fill: '#1D9E75', r: 3, strokeWidth: 0 }}
              activeDot={{ r: 5, strokeWidth: 0 }}
            />
          </LineChart>
        </ResponsiveContainer>
      </ChartCard>

    </div>
  )
}
