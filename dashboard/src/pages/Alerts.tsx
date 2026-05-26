import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { Alert } from '../api/types'

export default function Alerts() {
  const { data = [], isLoading, error } = useQuery<Alert[]>({
    queryKey: ['alerts'],
    queryFn: () => fetchJSON('/metrics/alerts'),
  })

  if (isLoading) return <div className="p-6 text-gray-500 text-sm">Loading...</div>
  if (error) return <div className="p-6 text-red-400 text-sm">Failed to load alerts</div>

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-xl font-semibold text-white">Cost Alerts</h1>
        {data.length > 0 && (
          <span className="px-2 py-0.5 rounded-full bg-amber-500/20 text-amber-400 text-xs font-medium">
            {data.length} alert{data.length !== 1 ? 's' : ''}
          </span>
        )}
      </div>

      {data.length === 0 ? (
        <div className="bg-gray-900 rounded-xl border border-gray-800 p-12 text-center">
          <p className="text-gray-500 text-sm">No alerts — all features are within budget</p>
        </div>
      ) : (
        <div className="space-y-3">
          {data.map((alert, i) => (
            <div
              key={i}
              className="bg-gray-900 rounded-xl border border-amber-500/25 p-4 flex items-start gap-4"
            >
              <div className="mt-1 w-2 h-2 rounded-full bg-amber-400 shrink-0" />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-sm font-semibold text-amber-400">Cost Spike</span>
                  <span className="text-sm text-gray-300">—</span>
                  <span className="text-sm text-white font-medium">{alert.feature}</span>
                </div>
                <p className="text-xs text-gray-400 mt-1">
                  Daily cost{' '}
                  <span className="text-amber-300 font-mono">${alert.actual.toFixed(4)}</span>{' '}
                  exceeded threshold of{' '}
                  <span className="text-gray-300 font-mono">${alert.threshold.toFixed(2)}</span>
                </p>
              </div>
              <div className="text-xs text-gray-500 shrink-0 whitespace-nowrap">
                {format(new Date(alert.timestamp), 'MMM d, HH:mm')}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
