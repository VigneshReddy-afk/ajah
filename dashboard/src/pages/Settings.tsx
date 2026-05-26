import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchJSON, postJSON } from '../api/client'
import type { FeatureSetting, ProviderKey, Settings } from '../api/types'

const DEFAULT_PROVIDERS: ProviderKey[] = [
  { provider: 'openai',    api_key: '' },
  { provider: 'anthropic', api_key: '' },
]

export default function SettingsPage() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery<Settings>({
    queryKey: ['settings'],
    queryFn: () => fetchJSON('/settings'),
  })

  const [features, setFeatures] = useState<FeatureSetting[]>([])
  const [providers, setProviders] = useState<ProviderKey[]>(DEFAULT_PROVIDERS)

  useEffect(() => {
    if (!data) return
    setFeatures(data.feature_settings ?? [])
    if (data.provider_keys?.length) {
      const merged = DEFAULT_PROVIDERS.map(def => {
        const saved = data.provider_keys.find(k => k.provider === def.provider)
        return saved ?? def
      })
      setProviders(merged)
    }
  }, [data])

  const mutation = useMutation({
    mutationFn: (payload: Settings) => postJSON<{ ok: boolean }>('/settings', payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })

  const save = () => mutation.mutate({ feature_settings: features, provider_keys: providers })

  const updateFeature = (i: number, patch: Partial<FeatureSetting>) =>
    setFeatures(prev => prev.map((f, j) => (j === i ? { ...f, ...patch } : f)))

  const updateProvider = (i: number, patch: Partial<ProviderKey>) =>
    setProviders(prev => prev.map((p, j) => (j === i ? { ...p, ...patch } : p)))

  if (isLoading) return <div className="p-6 text-gray-500 text-sm">Loading...</div>

  return (
    <div className="p-6 space-y-8 max-w-2xl">
      <h1 className="text-xl font-semibold text-white">Settings</h1>

      {/* Feature configuration */}
      <section>
        <h2 className="text-sm font-semibold text-gray-300 mb-3">Feature Configuration</h2>
        <div className="space-y-3">
          {features.map((f, i) => (
            <div key={i} className="bg-gray-900 rounded-xl border border-gray-800 p-4 space-y-3">
              <input
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                placeholder="Feature name (e.g. chat)"
                value={f.feature_name}
                onChange={e => updateFeature(i, { feature_name: e.target.value })}
              />
              <div className="flex items-end gap-4">
                <div className="flex-1">
                  <label className="block text-xs text-gray-400 mb-1">
                    Cost alert threshold (USD / day)
                  </label>
                  <input
                    type="number"
                    min="0"
                    step="0.01"
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    value={f.cost_alert_threshold_usd}
                    onChange={e =>
                      updateFeature(i, {
                        cost_alert_threshold_usd: parseFloat(e.target.value) || 0,
                      })
                    }
                  />
                </div>
                <div className="flex items-center gap-2 pb-1">
                  <span className="text-xs text-gray-400">PII Masking</span>
                  <button
                    type="button"
                    onClick={() =>
                      updateFeature(i, { pii_masking_enabled: !f.pii_masking_enabled })
                    }
                    className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                      f.pii_masking_enabled ? 'bg-indigo-600' : 'bg-gray-700'
                    }`}
                  >
                    <span
                      className={`inline-block h-3 w-3 rounded-full bg-white shadow transform transition-transform ${
                        f.pii_masking_enabled ? 'translate-x-5' : 'translate-x-1'
                      }`}
                    />
                  </button>
                </div>
                <button
                  type="button"
                  onClick={() => setFeatures(prev => prev.filter((_, j) => j !== i))}
                  className="pb-1 text-gray-600 hover:text-red-400 text-xs transition-colors"
                >
                  Remove
                </button>
              </div>
            </div>
          ))}

          <button
            type="button"
            onClick={() =>
              setFeatures(prev => [
                ...prev,
                { feature_name: '', cost_alert_threshold_usd: 1.0, pii_masking_enabled: true },
              ])
            }
            className="text-sm text-indigo-400 hover:text-indigo-300 transition-colors"
          >
            + Add feature
          </button>
        </div>
      </section>

      {/* Provider API keys */}
      <section>
        <h2 className="text-sm font-semibold text-gray-300 mb-3">Provider API Keys</h2>
        <div className="space-y-3">
          {providers.map((pk, i) => (
            <div key={pk.provider} className="bg-gray-900 rounded-xl border border-gray-800 p-4">
              <label className="block text-xs text-gray-400 mb-1.5 capitalize">
                {pk.provider} API Key
              </label>
              <input
                type="password"
                autoComplete="off"
                placeholder={pk.provider === 'openai' ? 'sk-...' : 'sk-ant-...'}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                value={pk.api_key}
                onChange={e => updateProvider(i, { api_key: e.target.value })}
              />
            </div>
          ))}
        </div>
      </section>

      {/* Save */}
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={save}
          disabled={mutation.isPending}
          className="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
        >
          {mutation.isPending ? 'Saving...' : 'Save Settings'}
        </button>
        {mutation.isSuccess && (
          <span className="text-sm text-green-400">Saved successfully</span>
        )}
        {mutation.isError && (
          <span className="text-sm text-red-400">Failed to save</span>
        )}
      </div>
    </div>
  )
}
