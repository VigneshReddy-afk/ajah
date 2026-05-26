import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { TraceRecord } from '../api/types'

const COLS = ['Timestamp', 'User', 'Feature', 'Model', 'Cost', 'Latency', 'PII', 'Quality']

export default function Traces() {
  const [expanded, setExpanded] = useState<string | null>(null)

  const { data = [], isLoading, error } = useQuery<TraceRecord[]>({
    queryKey: ['traces'],
    queryFn: () => fetchJSON('/metrics/traces'),
  })

  if (isLoading) return <div className="p-6 text-gray-500 text-sm">Loading...</div>
  if (error) return <div className="p-6 text-red-400 text-sm">Failed to load traces</div>

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-white">Traces</h1>
        <span className="text-xs text-gray-500">{data.length} rows — click to expand</span>
      </div>

      <div className="bg-gray-900 rounded-xl border border-gray-800 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-800">
                {COLS.map(h => (
                  <th
                    key={h}
                    className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide"
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {data.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-4 py-10 text-center text-gray-600 text-sm">
                    No traces yet
                  </td>
                </tr>
              )}
              {data.map(trace => (
                <Fragment key={trace.trace_id}>
                  <tr
                    onClick={() =>
                      setExpanded(expanded === trace.trace_id ? null : trace.trace_id)
                    }
                    className="border-b border-gray-800/60 hover:bg-gray-800/50 cursor-pointer transition-colors"
                  >
                    <td className="px-4 py-3 text-gray-300 whitespace-nowrap font-mono text-xs">
                      {format(new Date(trace.timestamp), 'HH:mm:ss')}
                    </td>
                    <td className="px-4 py-3 text-gray-300 max-w-[120px] truncate">
                      {trace.user_id || <span className="text-gray-600">—</span>}
                    </td>
                    <td className="px-4 py-3 text-gray-300">
                      {trace.feature_name || <span className="text-gray-600">—</span>}
                    </td>
                    <td className="px-4 py-3 text-indigo-400">{trace.model}</td>
                    <td className="px-4 py-3 text-gray-300 tabular-nums">
                      ${trace.cost_usd.toFixed(6)}
                    </td>
                    <td className="px-4 py-3 text-gray-300 tabular-nums">
                      {trace.latency_ms}ms
                    </td>
                    <td className="px-4 py-3">
                      {trace.was_pii_masked ? (
                        <span className="px-2 py-0.5 rounded text-xs bg-amber-500/20 text-amber-400 font-medium">
                          YES
                        </span>
                      ) : (
                        <span className="px-2 py-0.5 rounded text-xs bg-gray-800 text-gray-500">
                          NO
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-gray-300 tabular-nums">
                      {trace.quality_score.toFixed(2)}
                    </td>
                  </tr>

                  {expanded === trace.trace_id && (
                    <tr className="bg-gray-800/40 border-b border-gray-800/60">
                      <td colSpan={8} className="px-4 py-4">
                        <p className="text-xs text-gray-500 uppercase tracking-wide mb-1.5">
                          Masked Prompt
                        </p>
                        <p className="text-sm text-gray-200 font-mono leading-relaxed whitespace-pre-wrap">
                          {trace.masked_prompt || (
                            <span className="text-gray-600 italic">empty</span>
                          )}
                        </p>
                        <div className="mt-2 flex gap-4 text-xs text-gray-500">
                          <span>trace: {trace.trace_id}</span>
                          <span>provider: {trace.provider}</span>
                          <span>status: {trace.status_code}</span>
                          <span>in: {trace.input_tokens} / out: {trace.output_tokens}</span>
                        </div>
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
