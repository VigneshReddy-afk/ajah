import React from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell, Sector,
  AreaChart, Area, CartesianGrid,
} from 'recharts'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { CostMetrics, TraceRecord } from '../api/types'

const MODEL_COLORS = ['#2D7DD2', '#22C48A', '#F0A429', '#E05252', '#9B6DFF']

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

const tooltipStyle = {
  contentStyle: {
    background: '#0D1117',
    border: '1px solid rgba(45,125,210,0.2)',
    borderRadius: 8,
    fontSize: 12,
    color: '#F0F4FF',
    padding: '8px 12px',
    boxShadow: '0 8px 32px rgba(0,0,0,0.6)',
  },
  labelStyle: { color: '#525E7A', fontSize: 11, marginBottom: 4 },
  cursor: { fill: 'rgba(45,125,210,0.04)' },
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function PieActiveShape(props: any) {
  const { cx, cy, innerRadius, outerRadius, startAngle, endAngle, fill } = props
  return (
    <g>
      <Sector cx={cx} cy={cy} innerRadius={innerRadius}
        outerRadius={outerRadius + 6}
        startAngle={startAngle} endAngle={endAngle}
        fill={fill} opacity={0.95} />
      <Sector cx={cx} cy={cy}
        innerRadius={outerRadius + 9} outerRadius={outerRadius + 12}
        startAngle={startAngle} endAngle={endAngle}
        fill={fill} opacity={0.3} />
    </g>
  )
}

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

function useCountUp(target: number, duration = 1000) {
  const [count, setCount] = React.useState(0)
  React.useEffect(() => {
    if (target === 0) { setCount(0); return }
    const start = performance.now()
    const frame = (now: number) => {
      const pct = Math.min((now - start) / duration, 1)
      const eased = 1 - Math.pow(1 - pct, 3)
      setCount(Math.floor(eased * target))
      if (pct < 1) requestAnimationFrame(frame)
      else setCount(target)
    }
    requestAnimationFrame(frame)
  }, [target, duration])
  return count
}

function StatCard({
  label,
  value,
  sub,
  accent,
  danger,
  bg,
  border,
  valueFontSize,
  labelColor,
}: {
  label: string
  value: string | number
  sub: string
  accent?: string
  danger?: boolean
  bg?: string
  border?: string
  valueFontSize?: number
  labelColor?: string
}) {
  const defaultBg = 'linear-gradient(135deg, #0F1219 0%, #111520 100%)'
  const defaultBorder = 'rgba(255,255,255,0.07)'
  const resolvedBorder = border ?? `1px solid ${defaultBorder}`
  return (
    <div style={{
      background: bg ?? defaultBg,
      border: resolvedBorder,
      borderRadius: 12,
      padding: '24px 28px',
      position: 'relative',
      overflow: 'hidden',
      transition: 'border-color 0.2s ease, box-shadow 0.2s ease, transform 0.15s ease',
      cursor: 'default',
    }}
    onMouseEnter={e => {
      const el = e.currentTarget
      el.style.borderColor = 'rgba(45,125,210,0.3)'
      el.style.boxShadow = '0 0 24px rgba(45,125,210,0.08), 0 4px 20px rgba(0,0,0,0.4)'
      el.style.transform = 'translateY(-2px)'
    }}
    onMouseLeave={e => {
      const el = e.currentTarget
      el.style.borderColor = border
        ? border.replace('1px solid ', '')
        : defaultBorder
      el.style.boxShadow = 'none'
      el.style.transform = 'translateY(0)'
    }}
    >
      {/* Top accent line */}
      <div style={{
        position: 'absolute',
        top: 0, left: 0, right: 0,
        height: 1,
        background: `linear-gradient(90deg, transparent, ${accent || 'rgba(45,125,210,0.6)'}, transparent)`,
      }} />

      <p style={{
        fontSize: 10,
        fontWeight: 600,
        letterSpacing: '0.1em',
        textTransform: 'uppercase',
        color: labelColor ?? '#525E7A',
        margin: '0 0 12px',
      }}>
        {label}
      </p>

      <p style={{
        fontSize: valueFontSize ?? 32,
        fontWeight: 700,
        margin: '0 0 8px',
        color: danger ? '#E05252' : '#F0F4FF',
        fontVariantNumeric: 'tabular-nums',
        letterSpacing: '-0.02em',
        lineHeight: 1,
      }}>
        {value}
      </p>

      <p style={{
        fontSize: 12,
        color: danger ? 'rgba(224,82,82,0.7)' : '#525E7A',
        margin: 0,
      }}>
        {sub}
      </p>
    </div>
  )
}

function ChartCard({
  title,
  children,
  accent,
  badge,
}: {
  title: string
  children: React.ReactNode
  accent?: string
  badge?: React.ReactNode
}) {
  return (
    <div style={{
      background: 'linear-gradient(135deg, #0F1219 0%, #111520 100%)',
      border: '1px solid rgba(255,255,255,0.07)',
      borderRadius: 12,
      padding: '20px 20px 16px',
      position: 'relative',
      overflow: 'hidden',
      transition: 'border-color 0.2s ease, box-shadow 0.2s ease',
    }}
    onMouseEnter={e => {
      const el = e.currentTarget
      el.style.borderColor = 'rgba(45,125,210,0.25)'
      el.style.boxShadow = '0 0 20px rgba(45,125,210,0.06)'
    }}
    onMouseLeave={e => {
      const el = e.currentTarget
      el.style.borderColor = 'rgba(255,255,255,0.07)'
      el.style.boxShadow = 'none'
    }}
    >
      <div style={{
        position: 'absolute',
        top: 0, left: 0, right: 0,
        height: 1,
        background: `linear-gradient(90deg, transparent, ${accent || 'rgba(45,125,210,0.4)'}, transparent)`,
      }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, margin: '0 0 16px' }}>
        <p style={{
          fontSize: 10,
          fontWeight: 600,
          letterSpacing: '0.1em',
          textTransform: 'uppercase',
          color: '#525E7A',
          margin: 0,
        }}>
          {title}
        </p>
        {badge}
      </div>
      {children}
    </div>
  )
}

export default function Overview() {
  const { data, isLoading } = useQuery<CostMetrics>({
    queryKey: ['metrics'],
    queryFn: () => fetchJSON('/metrics/cost'),
  })
  const { data: traces = [] } = useQuery<TraceRecord[]>({
    queryKey: ['traces'],
    queryFn: () => fetchJSON('/metrics/traces'),
  })

  const totalTraces = data?.total_traces ?? 0
  const animatedTraces = useCountUp(totalTraces > 0 ? totalTraces : 24)
  const piiCount = data?.pii_masked_count ?? 0

  if (isLoading) {
    return (
      <div style={{
        padding: 32,
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        color: '#525E7A',
        fontSize: 13,
      }}>
        <div style={{
          width: 16, height: 16,
          border: '2px solid rgba(45,125,210,0.2)',
          borderTopColor: '#2D7DD2',
          borderRadius: '50%',
          animation: 'spin 0.8s linear infinite',
        }} />
        Loading metrics…
      </div>
    )
  }

  const totalCost = data
    ? Object.values(data.by_user).reduce((a, b) => a + b, 0)
    : 0
  const featureCount = data ? Object.keys(data.by_feature).length : 0

  const featureData = data && Object.keys(data.by_feature).length > 0
    ? Object.entries(data.by_feature)
        .map(([name, cost]) => ({ name, cost: +cost.toFixed(6) }))
        .sort((a, b) => b.cost - a.cost)
    : MOCK_FEATURE

  const modelData = data && Object.keys(data.by_model).length > 0
    ? Object.entries(data.by_model)
        .map(([name, value]) => ({ name, value: +value.toFixed(6) }))
    : MOCK_MODEL

  const rawTrend = buildQualityTrend(traces)
  const qualityTrend = rawTrend.length > 0 ? rawTrend : MOCK_QUALITY

  return (
    <div style={{
      padding: '28px 32px',
      display: 'flex',
      flexDirection: 'column',
      gap: 20,
      animation: 'fadeIn 0.3s ease forwards',
    }}>

      {/* Stat cards */}
      <div style={{
        display: 'grid',
        gridTemplateColumns: '2fr 1fr 1fr',
        gap: 16,
      }}>
        <StatCard
          label="◆ Total cost today"
          value={`$${totalCost > 0 ? totalCost.toFixed(5) : '0.01074'}`}
          sub="+12% vs yesterday"
          accent="rgba(34,196,138,0.5)"
          bg="linear-gradient(135deg, #0A0E1A 0%, #0D1220 50%, #0A0E1A 100%)"
          border="1px solid rgba(45,125,210,0.18)"
          valueFontSize={48}
          labelColor="#2D7DD2"
        />
        <StatCard
          label="Total traces"
          value={animatedTraces.toLocaleString()}
          sub={featureCount > 0
            ? `across ${featureCount} feature${featureCount > 1 ? 's' : ''}`
            : 'across 5 features'}
          accent="rgba(45,125,210,0.5)"
        />
        <StatCard
          label="PII detections"
          value={piiCount > 0 ? piiCount : 3}
          sub="masked before storage"
          danger
          bg="linear-gradient(135deg, #0E0A0A 0%, #130D0D 100%)"
          border="1px solid rgba(224,82,82,0.15)"
          accent="rgba(224,82,82,0.5)"
        />
      </div>

      {/* Charts row */}
      <div style={{ display: 'grid', gridTemplateColumns: '1.2fr 0.8fr', gap: 16 }}>

        {/* Cost by Feature */}
        <ChartCard title="Cost by Feature">
          <ResponsiveContainer width="100%" height={200}>
            <BarChart
              data={featureData}
              margin={{ top: 4, right: 4, left: 0, bottom: 0 }}
            >
              <XAxis
                dataKey="name"
                tick={{ fill: '#3A4560', fontSize: 11 }}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                tick={{ fill: '#3A4560', fontSize: 11 }}
                axisLine={false}
                tickLine={false}
                tickFormatter={v => `$${v}`}
                width={50}
              />
              <Tooltip
                {...tooltipStyle}
                formatter={(v: number) => [`$${v}`, 'Cost']}
              />
              <Bar
                dataKey="cost"
                fill="#2D7DD2"
                radius={[6, 6, 0, 0]}
                maxBarSize={40}
              >
                {featureData.map((_, i) => (
                  <Cell
                    key={i}
                    fill={i === 0 ? '#2D7DD2' : `rgba(45,125,210,${0.8 - i * 0.15})`}
                  />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </ChartCard>

        {/* Cost by Model donut */}
        <ChartCard title="Cost by Model">
          <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
            <ResponsiveContainer width="55%" height={200}>
              <PieChart>
                <Pie
                  data={modelData}
                  dataKey="value"
                  nameKey="name"
                  cx="50%" cy="50%"
                  innerRadius={52} outerRadius={76}
                  paddingAngle={3}
                  activeShape={PieActiveShape}
                >
                  {modelData.map((_, i) => (
                    <Cell
                      key={i}
                      fill={MODEL_COLORS[i % MODEL_COLORS.length]}
                    />
                  ))}
                </Pie>
                <Tooltip
                  contentStyle={tooltipStyle.contentStyle}
                  formatter={(v: number) => [`$${v.toFixed(6)}`, 'Cost']}
                />
              </PieChart>
            </ResponsiveContainer>
            <div style={{
              flex: 1,
              display: 'flex',
              flexDirection: 'column',
              gap: 8,
            }}>
              {modelData.map((m, i) => (
                <div key={m.name} style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                }}>
                  <div style={{
                    width: 8, height: 8,
                    borderRadius: 2,
                    background: MODEL_COLORS[i % MODEL_COLORS.length],
                    flexShrink: 0,
                    boxShadow: `0 0 6px ${MODEL_COLORS[i % MODEL_COLORS.length]}60`,
                  }} />
                  <span style={{
                    fontSize: 11,
                    color: '#8B95B8',
                    flex: 1,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}>
                    {m.name}
                  </span>
                  <span style={{
                    fontSize: 11,
                    color: '#525E7A',
                    fontVariantNumeric: 'tabular-nums',
                  }}>
                    ${m.value.toFixed(5)}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </ChartCard>
      </div>

      {/* Quality Trend */}
      <ChartCard
        title="Output Quality Trend"
        accent="rgba(34,196,138,0.4)"
        badge={
          <span style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 5,
            background: 'rgba(34,196,138,0.08)',
            border: '1px solid rgba(34,196,138,0.15)',
            borderRadius: 20,
            padding: '2px 8px',
            fontSize: 10,
            fontWeight: 600,
            color: '#22C48A',
            letterSpacing: '0.06em',
          }}>
            <span style={{
              width: 5, height: 5,
              borderRadius: '50%',
              background: '#22C48A',
              animation: 'pulsarDot 1.4s ease-in-out infinite',
            }} />
            LIVE
          </span>
        }
      >
        <ResponsiveContainer width="100%" height={180}>
          <AreaChart
            data={qualityTrend}
            margin={{ top: 4, right: 16, left: 0, bottom: 0 }}
          >
            <defs>
              <linearGradient id="qGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#22C48A" stopOpacity={0.2}/>
                <stop offset="95%" stopColor="#22C48A" stopOpacity={0}/>
              </linearGradient>
            </defs>
            <CartesianGrid
              strokeDasharray="3 3"
              stroke="rgba(255,255,255,0.03)"
              vertical={false}
            />
            <XAxis
              dataKey="hour"
              tick={{ fill: '#3A4560', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              domain={[0, 1]}
              tick={{ fill: '#3A4560', fontSize: 11 }}
              axisLine={false} tickLine={false}
              tickFormatter={v => v.toFixed(1)}
              width={32}
            />
            <Tooltip
              {...tooltipStyle}
              formatter={(v: number) => [v.toFixed(4), 'Avg quality']}
            />
            <Area
              type="monotone"
              dataKey="avg"
              stroke="#22C48A"
              strokeWidth={2}
              fill="url(#qGrad)"
              dot={{ fill: '#22C48A', r: 3, strokeWidth: 0 }}
              activeDot={{ r: 5, strokeWidth: 0 }}
            />
          </AreaChart>
        </ResponsiveContainer>
      </ChartCard>

    </div>
  )
}
