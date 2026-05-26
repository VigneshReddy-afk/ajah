import { useQuery } from '@tanstack/react-query'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell,
} from 'recharts'
import { fetchJSON } from '../api/client'
import type { CostMetrics } from '../api/types'

const PALETTE = ['#6366f1', '#8b5cf6', '#ec4899', '#f59e0b', '#10b981', '#3b82f6', '#ef4444']

const tooltipStyle = {
  contentStyle: { background: '#111827', border: '1px solid #374151', borderRadius: 8 },
  labelStyle: { color: '#f3f4f6' },
}

export default function Overview() {
  const { data, isLoading, error } = useQuery<CostMetrics>({
    queryKey: ['metrics'],
    queryFn: () => fetchJSON('/metrics/cost'),
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
    </div>
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

function Empty() {
  return (
    <div className="h-[220px] flex items-center justify-center text-gray-600 text-sm">
      No data yet
    </div>
  )
}

function Loading() {
  return <div className="p-6 text-gray-500 text-sm">Loading...</div>
}

function Error({ msg }: { msg: string }) {
  return <div className="p-6 text-red-400 text-sm">{msg}</div>
}
