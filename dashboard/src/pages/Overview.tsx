import { useQuery } from '@tanstack/react-query'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell, LineChart, Line, CartesianGrid,
} from 'recharts'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { CostMetrics, TraceRecord } from '../api/types'

const PALETTE = ['#6366f1', '#8b5cf6', '#ec4899', '#f59e0b', '#10b981', '#3b82f6', '#ef4444']

const tooltipStyle = {
  contentStyle: { background: '#111827', border: '1px solid #374151', borderRadius: 8 },
  labelStyle: { color: '#f3f4f6' },
}

function buildQualityTrend(traces: TraceRecord[]) {
  const byHour: Record<string, number[]> = {}
  for (const t of traces) {
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

function qualityColor(avg: number) {
  if (avg > 0.7) return '#10b981'
  if (avg >= 0.4) return '#f59e0b'
  return '#ef4444'
}

export default function Overview() {
  const { data, isLoading, error } = useQuery<CostMetrics>({
    queryKey: ['metrics'],
    queryFn: () => fetchJSON('/metrics/cost'),
  })

  const { data: traces } = useQuery<TraceRecord[]>({
    queryKey: ['traces'],
    queryFn: () => fetchJSON('/metrics/traces'),
  })

  if (isLoading) return <Loading />
  if (error || !data) return <Error msg="Failed to load metrics" />

  const totalCost = Object.values(data.by_user).reduce((a, b) => a + b, 0)

  const featureData = Object.entries(data.by_feature).map(([name, cost]) => ({
    name,
    cost: +cost.toFixed(6),
  }))

  const modelData = Object.entries(data.by_model).map(([name, value]) => ({
    name,
    value: +value.toFixed(6),
  }))

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-white">Overview</h1>
        <span className="text-sm text-gray-500">{data.date}</span>
      </div>

      {/* KPI cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <StatCard label="Total Cost Today" value={`$${totalCost.toFixed(6)}`} large />
        <StatCard label="Total Traces" value={data.total_traces.toLocaleString()} />
        <StatCard
          label="PII Detections"
          value={data.pii_masked_count.toLocaleString()}
          warn={data.pii_masked_count > 0}
        />
      </div>

      {/* Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ChartCard title="Cost by Feature">
          {featureData.length === 0 ? (
            <Empty />
          ) : (
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={featureData} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
                <XAxis dataKey="name" tick={{ fill: '#9ca3af', fontSize: 11 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} tickFormatter={v => `$${v}`} width={60} />
                <Tooltip
                  {...tooltipStyle}
                  formatter={(v: number) => [`$${v}`, 'Cost']}
                />
                <Bar dataKey="cost" fill="#6366f1" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </ChartCard>

        <ChartCard title="Cost by Model">
          {modelData.length === 0 ? (
            <Empty />
          ) : (
            <ResponsiveContainer width="100%" height={220}>
              <PieChart>
                <Pie
                  data={modelData}
                  dataKey="value"
                  nameKey="name"
                  cx="50%"
                  cy="50%"
                  outerRadius={80}
                  label={({ name, percent }) =>
                    `${name} (${(percent * 100).toFixed(0)}%)`
                  }
                  labelLine={{ stroke: '#4b5563' }}
                >
                  {modelData.map((_, i) => (
                    <Cell key={i} fill={PALETTE[i % PALETTE.length]} />
                  ))}
                </Pie>
                <Tooltip
                  {...tooltipStyle}
                  formatter={(v: number) => [`$${v}`, 'Cost']}
                />
              </PieChart>
            </ResponsiveContainer>
          )}
        </ChartCard>
      </div>

      {/* Quality Trend */}
      <QualityTrend traces={traces ?? []} />
    </div>
  )
}

function QualityTrend({ traces }: { traces: TraceRecord[] }) {
  if (traces.length === 0) {
    return (
      <ChartCard title="Output Quality Trend">
        <Empty msg="No quality data yet" />
      </ChartCard>
    )
  }

  const allZero = traces.every(t => t.quality_score === 0)
  if (allZero) {
    return (
      <ChartCard title="Output Quality Trend">
        <Empty msg="Waiting for successful LLM responses" />
      </ChartCard>
    )
  }

  const trendData = buildQualityTrend(traces)
  const overallAvg = trendData.reduce((s, d) => s + d.avg, 0) / trendData.length
  const lineColor = qualityColor(overallAvg)

  return (
    <ChartCard title="Output Quality Trend">
      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={trendData} margin={{ top: 4, right: 16, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
          <XAxis dataKey="hour" tick={{ fill: '#9ca3af', fontSize: 11 }} />
          <YAxis
            domain={[0, 1]}
            tick={{ fill: '#9ca3af', fontSize: 11 }}
            tickFormatter={v => v.toFixed(1)}
            width={40}
          />
          <Tooltip
            {...tooltipStyle}
            formatter={(v: number) => [v.toFixed(4), 'Avg Quality']}
          />
          <Line
            type="monotone"
            dataKey="avg"
            stroke={lineColor}
            strokeWidth={2}
            dot={{ fill: lineColor, r: 4 }}
            activeDot={{ r: 6 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </ChartCard>
  )
}

function StatCard({
  label,
  value,
  large,
  warn,
}: {
  label: string
  value: string
  large?: boolean
  warn?: boolean
}) {
  return (
    <div className="bg-gray-900 rounded-xl p-5 border border-gray-800">
      <p className="text-xs text-gray-400 uppercase tracking-wide">{label}</p>
      <p
        className={`mt-2 font-bold tabular-nums ${large ? 'text-3xl' : 'text-2xl'} ${
          warn ? 'text-amber-400' : 'text-white'
        }`}
      >
        {value}
      </p>
    </div>
  )
}

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-gray-900 rounded-xl p-5 border border-gray-800">
      <h2 className="text-sm font-medium text-gray-400 mb-4">{title}</h2>
      {children}
    </div>
  )
}

function Empty({ msg = 'No data yet' }: { msg?: string }) {
  return (
    <div className="h-[220px] flex items-center justify-center text-gray-600 text-sm">
      {msg}
    </div>
  )
}

function Loading() {
  return <div className="p-6 text-gray-500 text-sm">Loading...</div>
}

function Error({ msg }: { msg: string }) {
  return <div className="p-6 text-red-400 text-sm">{msg}</div>
}
