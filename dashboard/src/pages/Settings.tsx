import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchJSON, postJSON } from '../api/client'
import type { FeatureSetting, ProviderKey, Settings } from '../api/types'

const PROVIDER_META: Record<string, { label: string; placeholder: string }> = {
  openai:    { label: 'OpenAI',       placeholder: 'sk-...' },
  anthropic: { label: 'Anthropic',    placeholder: 'sk-ant-...' },
  groq:      { label: 'Groq',         placeholder: 'gsk_...' },
  gemini:    { label: 'Google Gemini', placeholder: 'AIza...' },
  grok:      { label: 'xAI / Grok',   placeholder: 'xai-...' },
  mistral:   { label: 'Mistral',      placeholder: 'mistral-...' },
  together:  { label: 'Together AI',  placeholder: 'together-...' },
  nvidia:    { label: 'NVIDIA',       placeholder: 'nvapi-...' },
  cohere:    { label: 'Cohere',       placeholder: 'cohere-...' },
}

const PROVIDER_ORDER = ['openai', 'anthropic', 'groq', 'gemini', 'grok', 'mistral', 'together', 'nvidia', 'cohere']

const DEFAULT_PROVIDERS: ProviderKey[] = PROVIDER_ORDER.map(p => ({ provider: p, api_key: '' }))

function EyeIcon({ open }: { open: boolean }) {
  return open ? (
    <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
    </svg>
  ) : (
    <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />
    </svg>
  )
}

export default function SettingsPage() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery<Settings>({
    queryKey: ['settings'],
    queryFn: () => fetchJSON('/settings'),
  })

  const [features, setFeatures] = useState<FeatureSetting[]>([])
  const [providers, setProviders] = useState<ProviderKey[]>(DEFAULT_PROVIDERS)
  const [visible, setVisible] = useState<Record<string, boolean>>({})

  useEffect(() => {
    if (!data) return
    setFeatures(data.feature_settings ?? [])
    const merged = DEFAULT_PROVIDERS.map(def => {
      const saved = data.provider_keys?.find(k => k.provider === def.provider)
      return saved ?? def
    })
    setProviders(merged)
  }, [data])

  const mutation = useMutation({
    mutationFn: (payload: Settings) => postJSON<{ ok: boolean }>('/settings', payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })

  const save = () => mutation.mutate({ feature_settings: features, provider_keys: providers })

  const updateFeature = (i: number, patch: Partial<FeatureSetting>) =>
    setFeatures(prev => prev.map((f, j) => (j === i ? { ...f, ...patch } : f)))

  const updateProvider = (provider: string, api_key: string) =>
    setProviders(prev => prev.map(p => (p.provider === provider ? { ...p, api_key } : p)))

  const toggleVisible = (provider: string) =>
    setVisible(prev => ({ ...prev, [provider]: !prev[provider] }))

  if (isLoading) return <div className="p-6 text-gray-500 text-sm">Loading...</div>

  return (
    <div className="p-6 space-y-8 max-w-3xl">
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

      {/* Provider API Keys */}
      <section>
        <h2 className="text-sm font-semibold text-gray-300 mb-3">Provider API Keys</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          {providers.map(pk => {
            const meta = PROVIDER_META[pk.provider]
            const isVisible = !!visible[pk.provider]
            return (
              <div key={pk.provider} className="bg-gray-900 rounded-xl border border-gray-800 p-4">
                <label className="block text-xs text-gray-400 mb-1.5">{meta.label} API Key</label>
                <div className="relative">
                  <input
                    type={isVisible ? 'text' : 'password'}
                    autoComplete="off"
                    placeholder={meta.placeholder}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 pr-9 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    value={pk.api_key}
                    onChange={e => updateProvider(pk.provider, e.target.value)}
                  />
                  <button
                    type="button"
                    onClick={() => toggleVisible(pk.provider)}
                    className="absolute inset-y-0 right-2 flex items-center text-gray-500 hover:text-gray-300 transition-colors"
                    aria-label={isVisible ? 'Hide key' : 'Show key'}
                  >
                    <EyeIcon open={isVisible} />
                  </button>
                </div>
              </div>
            )
          })}

          {/* Azure note — spans both columns */}
          <div className="sm:col-span-2 bg-amber-950/40 border border-amber-700/50 rounded-xl p-4">
            <p className="text-xs font-semibold text-amber-400 mb-1">Azure OpenAI</p>
            <p className="text-xs text-amber-300/80 leading-relaxed">
              Azure requires endpoint configuration.{' '}
              Set <code className="font-mono bg-amber-900/50 px-1 rounded">AZURE_OPENAI_ENDPOINT</code>{' '}
              in your <code className="font-mono bg-amber-900/50 px-1 rounded">.env</code> file.
            </p>
          </div>
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
