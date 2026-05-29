import type { CSSProperties } from 'react'
import { Fragment, useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconEye, IconEyeOff } from '@tabler/icons-react'
import { fetchJSON, postJSON } from '../api/client'
import type { FeatureSetting, ProviderKey, Settings } from '../api/types'

// ── Provider metadata ──────────────────────────────────────────────────────

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

const PROVIDER_ORDER = Object.keys(PROVIDER_META)
const DEFAULT_PROVIDERS: ProviderKey[] = PROVIDER_ORDER.map(p => ({ provider: p, api_key: '' }))

const MOCK_FEATURES: FeatureSetting[] = [
  { feature_name: 'chat',      cost_alert_threshold_usd: 1.00, pii_masking_enabled: true,  webhook_url: '', cross_model_enabled: false, cross_model_provider_url: '', cross_model_api_key: '', cross_model_model: '' },
  { feature_name: 'summarize', cost_alert_threshold_usd: 2.00, pii_masking_enabled: true,  webhook_url: '', cross_model_enabled: false, cross_model_provider_url: '', cross_model_api_key: '', cross_model_model: '' },
  { feature_name: 'translate', cost_alert_threshold_usd: 0.50, pii_masking_enabled: false, webhook_url: '', cross_model_enabled: false, cross_model_provider_url: '', cross_model_api_key: '', cross_model_model: '' },
  { feature_name: 'classify',  cost_alert_threshold_usd: 0.50, pii_masking_enabled: false, webhook_url: '', cross_model_enabled: false, cross_model_provider_url: '', cross_model_api_key: '', cross_model_model: '' },
]

// ── Styles ─────────────────────────────────────────────────────────────────

const card: CSSProperties = {
  background: 'var(--color-background-secondary)',
  border: '0.5px solid var(--color-border-tertiary)',
  borderRadius: 10,
  padding: '16px',
}

const inputStyle: CSSProperties = {
  width: '100%',
  background: 'var(--color-background-primary)',
  border: '0.5px solid var(--color-border-secondary)',
  borderRadius: 6,
  padding: '8px 36px 8px 10px',
  fontSize: 'var(--sz-base)',
  color: 'var(--color-text-primary)',
  outline: 'none',
}

const inputPlain: CSSProperties = {
  ...inputStyle,
  padding: '7px 10px',
}

const labelStyle: CSSProperties = {
  display: 'block',
  fontSize: 'var(--sz-xs)',
  color: 'var(--color-text-tertiary)',
  marginBottom: 6,
  fontWeight: 500,
}

const sectionTitle: CSSProperties = {
  fontSize: 'var(--sz-sm)',
  fontWeight: 600,
  color: 'var(--color-text-secondary)',
  textTransform: 'uppercase',
  letterSpacing: '0.06em',
  margin: '0 0 14px',
}

const thStyle: CSSProperties = {
  padding: '10px 14px',
  fontSize: 'var(--sz-xs)',
  fontWeight: 500,
  color: 'var(--color-text-tertiary)',
  textTransform: 'uppercase',
  letterSpacing: '0.05em',
  textAlign: 'left',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
}

const tdStyle: CSSProperties = {
  padding: '11px 14px',
  fontSize: 'var(--sz-base)',
  color: 'var(--color-text-secondary)',
  borderBottom: '0.5px solid var(--color-border-tertiary)',
}

// ── Page ───────────────────────────────────────────────────────────────────

export default function SettingsPage() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery<Settings>({
    queryKey: ['settings'],
    queryFn: () => fetchJSON('/settings'),
  })

  const [features, setFeatures] = useState<FeatureSetting[]>(MOCK_FEATURES)
  const [providers, setProviders] = useState<ProviderKey[]>(DEFAULT_PROVIDERS)
  const [visible, setVisible] = useState<Record<string, boolean>>({})
  const [configuring, setConfiguring] = useState<number | null>(null)

  useEffect(() => {
    if (!data) return
    if (data.feature_settings?.length) {
      setFeatures(data.feature_settings.map(f => ({
        cross_model_enabled: false,
        cross_model_provider_url: '',
        cross_model_api_key: '',
        cross_model_model: '',
        ...f,
      })))
    }
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

  const updateProvider = (provider: string, api_key: string) =>
    setProviders(prev => prev.map(p => p.provider === provider ? { ...p, api_key } : p))

  const toggleVisible = (key: string) =>
    setVisible(prev => ({ ...prev, [key]: !prev[key] }))

  const updateFeature = (i: number, patch: Partial<FeatureSetting>) =>
    setFeatures(prev => prev.map((f, j) => j === i ? { ...f, ...patch } : f))

  if (isLoading) return <div style={{ padding: 24, color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>Loading…</div>

  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 28, maxWidth: 960 }}>

      {/* ── Provider API Keys ── */}
      <section>
        <p style={sectionTitle}>Provider API Keys</p>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
          {providers.map(pk => {
            const meta = PROVIDER_META[pk.provider]
            const show = !!visible[pk.provider]
            return (
              <div key={pk.provider} style={card}>
                <label style={labelStyle}>{meta.label} API Key</label>
                <div style={{ position: 'relative' }}>
                  <input
                    type={show ? 'text' : 'password'}
                    autoComplete="off"
                    placeholder={meta.placeholder}
                    style={inputStyle}
                    value={pk.api_key}
                    onChange={e => updateProvider(pk.provider, e.target.value)}
                  />
                  <button
                    type="button"
                    onClick={() => toggleVisible(pk.provider)}
                    style={{
                      position: 'absolute', right: 8, top: '50%', transform: 'translateY(-50%)',
                      background: 'none', border: 'none', cursor: 'pointer', padding: 2,
                      color: 'var(--color-text-tertiary)',
                      display: 'flex', alignItems: 'center',
                    }}
                  >
                    {show ? <IconEyeOff size={15} strokeWidth={1.75} /> : <IconEye size={15} strokeWidth={1.75} />}
                  </button>
                </div>
              </div>
            )
          })}

          {/* Azure — full width */}
          <div style={{
            gridColumn: '1 / -1',
            background: 'rgba(133,79,11,0.07)',
            border: '0.5px solid rgba(133,79,11,0.25)',
            borderRadius: 10,
            padding: '14px 18px',
          }}>
            <p style={{ margin: '0 0 4px', fontSize: 'var(--sz-sm)', fontWeight: 600, color: '#854F0B' }}>Azure OpenAI</p>
            <p style={{ margin: 0, fontSize: 'var(--sz-sm)', color: 'rgba(133,79,11,0.85)', lineHeight: 1.6 }}>
              Azure requires endpoint configuration.{' '}
              Set{' '}
              <code style={{ fontFamily: 'monospace', background: 'rgba(133,79,11,0.12)', padding: '1px 5px', borderRadius: 3 }}>
                AZURE_OPENAI_ENDPOINT
              </code>{' '}
              in your{' '}
              <code style={{ fontFamily: 'monospace', background: 'rgba(133,79,11,0.12)', padding: '1px 5px', borderRadius: 3 }}>
                .env
              </code>{' '}
              file.
            </p>
          </div>
        </div>
      </section>

      {/* ── Feature Configuration ── */}
      <section>
        <p style={sectionTitle}>Feature Configuration</p>

        <div style={{
          background: 'var(--color-background-secondary)',
          border: '0.5px solid var(--color-border-tertiary)',
          borderRadius: 10,
          overflow: 'hidden',
          marginBottom: 12,
        }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={thStyle}>Feature</th>
                <th style={thStyle}>Cost alert threshold</th>
                <th style={thStyle}>PII masking</th>
                <th style={thStyle}>Status</th>
                <th style={thStyle}>Cross-model</th>
                <th style={{ ...thStyle, width: 60 }}></th>
              </tr>
            </thead>
            <tbody>
              {features.map((f, i) => (
                <Fragment key={i}>
                  <tr>
                    <td style={tdStyle}>
                      <input
                        value={f.feature_name}
                        onChange={e => updateFeature(i, { feature_name: e.target.value })}
                        placeholder="feature name"
                        style={{ ...inputStyle, padding: '6px 10px', width: 140 }}
                      />
                    </td>
                    <td style={tdStyle}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        <span style={{ color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-base)' }}>$</span>
                        <input
                          type="number"
                          min="0"
                          step="0.01"
                          value={f.cost_alert_threshold_usd}
                          onChange={e => updateFeature(i, { cost_alert_threshold_usd: parseFloat(e.target.value) || 0 })}
                          style={{ ...inputStyle, padding: '6px 10px', width: 90 }}
                        />
                        <span style={{ color: 'var(--color-text-tertiary)', fontSize: 'var(--sz-sm)' }}>/ day</span>
                      </div>
                    </td>
                    <td style={tdStyle}>
                      <button
                        type="button"
                        onClick={() => updateFeature(i, { pii_masking_enabled: !f.pii_masking_enabled })}
                        style={{
                          width: 36, height: 20, borderRadius: 10, border: 'none', cursor: 'pointer',
                          background: f.pii_masking_enabled ? '#185FA5' : 'var(--color-background-primary)',
                          position: 'relative', transition: 'background 0.15s',
                          flexShrink: 0,
                        }}
                      >
                        <span style={{
                          position: 'absolute', top: 2, width: 16, height: 16,
                          borderRadius: '50%', background: '#fff', transition: 'left 0.15s',
                          left: f.pii_masking_enabled ? 18 : 2,
                          boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
                        }} />
                      </button>
                    </td>
                    <td style={tdStyle}>
                      {f.feature_name ? (
                        <span style={{
                          fontSize: 'var(--sz-xs)', fontWeight: 500,
                          color: '#0F6E56',
                          background: 'rgba(15,110,86,0.12)',
                          padding: '2px 8px',
                          borderRadius: 4,
                        }}>Active</span>
                      ) : (
                        <span style={{ color: 'var(--color-text-tertiary)' }}>—</span>
                      )}
                    </td>
                    <td style={tdStyle}>
                      <button
                        type="button"
                        onClick={() => setConfiguring(configuring === i ? null : i)}
                        style={{
                          background: 'none',
                          border: `0.5px solid ${configuring === i ? '#185FA5' : 'var(--color-border-secondary)'}`,
                          borderRadius: 5,
                          cursor: 'pointer',
                          padding: '3px 10px',
                          fontSize: 'var(--sz-xs)',
                          fontWeight: 500,
                          color: configuring === i ? '#185FA5' : 'var(--color-text-tertiary)',
                          transition: 'all 0.12s',
                        }}
                      >
                        {f.cross_model_enabled ? '● On' : 'Configure'}
                      </button>
                    </td>
                    <td style={{ ...tdStyle, textAlign: 'center' }}>
                      <button
                        type="button"
                        onClick={() => {
                          setFeatures(prev => prev.filter((_, j) => j !== i))
                          if (configuring === i) setConfiguring(null)
                        }}
                        style={{
                          background: 'none', border: 'none', cursor: 'pointer', padding: '2px 4px',
                          fontSize: 'var(--sz-sm)', color: 'var(--color-text-tertiary)',
                          transition: 'color 0.12s',
                        }}
                        onMouseEnter={e => (e.currentTarget.style.color = '#A32D2D')}
                        onMouseLeave={e => (e.currentTarget.style.color = 'var(--color-text-tertiary)')}
                      >
                        Remove
                      </button>
                    </td>
                  </tr>

                  {/* ── Cross-model expand ── */}
                  {configuring === i && (
                    <tr>
                      <td colSpan={6} style={{
                        padding: '14px 16px 16px',
                        background: 'rgba(24,95,165,0.04)',
                        borderBottom: '0.5px solid var(--color-border-tertiary)',
                        borderLeft: '2px solid #185FA5',
                      }}>
                        <p style={{
                          fontSize: 'var(--sz-xs)', fontWeight: 600, color: '#185FA5',
                          textTransform: 'uppercase', letterSpacing: '0.06em',
                          margin: '0 0 12px',
                        }}>
                          Cross-model Verification
                        </p>

                        {/* Enable toggle row */}
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14 }}>
                          <button
                            type="button"
                            onClick={() => updateFeature(i, { cross_model_enabled: !f.cross_model_enabled })}
                            style={{
                              width: 36, height: 20, borderRadius: 10, border: 'none', cursor: 'pointer',
                              background: f.cross_model_enabled ? '#185FA5' : 'var(--color-background-primary)',
                              position: 'relative', transition: 'background 0.15s', flexShrink: 0,
                            }}
                          >
                            <span style={{
                              position: 'absolute', top: 2, width: 16, height: 16,
                              borderRadius: '50%', background: '#fff', transition: 'left 0.15s',
                              left: f.cross_model_enabled ? 18 : 2,
                              boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
                            }} />
                          </button>
                          <span style={{ fontSize: 'var(--sz-base)', color: 'var(--color-text-secondary)', fontWeight: f.cross_model_enabled ? 500 : 400 }}>
                            Enable cross-model verification
                          </span>
                          {f.cross_model_enabled && (
                            <span style={{
                              fontSize: 'var(--sz-xs)', fontWeight: 600,
                              color: '#0F6E56', background: 'rgba(15,110,86,0.12)',
                              padding: '1px 7px', borderRadius: 4,
                            }}>
                              Enabled
                            </span>
                          )}
                        </div>

                        {/* Config fields */}
                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 10 }}>
                          {/* Provider URL */}
                          <div>
                            <label style={labelStyle}>Secondary provider URL</label>
                            <input
                              type="text"
                              placeholder="https://api.groq.com/openai/v1"
                              value={f.cross_model_provider_url ?? ''}
                              onChange={e => updateFeature(i, { cross_model_provider_url: e.target.value })}
                              style={inputPlain}
                              disabled={!f.cross_model_enabled}
                            />
                          </div>

                          {/* API key */}
                          <div>
                            <label style={labelStyle}>Secondary API key</label>
                            <div style={{ position: 'relative' }}>
                              <input
                                type={visible[`cm_key_${i}`] ? 'text' : 'password'}
                                autoComplete="off"
                                placeholder="sk-..."
                                value={f.cross_model_api_key ?? ''}
                                onChange={e => updateFeature(i, { cross_model_api_key: e.target.value })}
                                style={inputStyle}
                                disabled={!f.cross_model_enabled}
                              />
                              <button
                                type="button"
                                onClick={() => toggleVisible(`cm_key_${i}`)}
                                style={{
                                  position: 'absolute', right: 8, top: '50%', transform: 'translateY(-50%)',
                                  background: 'none', border: 'none', cursor: 'pointer', padding: 2,
                                  color: 'var(--color-text-tertiary)',
                                  display: 'flex', alignItems: 'center',
                                }}
                              >
                                {visible[`cm_key_${i}`]
                                  ? <IconEyeOff size={14} strokeWidth={1.75} />
                                  : <IconEye size={14} strokeWidth={1.75} />}
                              </button>
                            </div>
                          </div>

                          {/* Model */}
                          <div>
                            <label style={labelStyle}>Secondary model</label>
                            <input
                              type="text"
                              placeholder="llama-3.3-70b-versatile"
                              value={f.cross_model_model ?? ''}
                              onChange={e => updateFeature(i, { cross_model_model: e.target.value })}
                              style={inputPlain}
                              disabled={!f.cross_model_enabled}
                            />
                          </div>
                        </div>

                        <div style={{ marginTop: 12, display: 'flex', gap: 8 }}>
                          <button
                            type="button"
                            onClick={save}
                            disabled={mutation.isPending}
                            style={{
                              background: '#185FA5', color: '#fff', border: 'none',
                              borderRadius: 6, padding: '6px 16px',
                              fontSize: 'var(--sz-sm)', fontWeight: 500,
                              cursor: mutation.isPending ? 'default' : 'pointer',
                              opacity: mutation.isPending ? 0.6 : 1,
                            }}
                          >
                            {mutation.isPending ? 'Saving…' : 'Save'}
                          </button>
                          <button
                            type="button"
                            onClick={() => setConfiguring(null)}
                            style={{
                              background: 'none', border: '0.5px solid var(--color-border-secondary)',
                              borderRadius: 6, padding: '6px 16px',
                              fontSize: 'var(--sz-sm)', color: 'var(--color-text-tertiary)',
                              cursor: 'pointer',
                            }}
                          >
                            Close
                          </button>
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}

              {features.length === 0 && (
                <tr>
                  <td colSpan={6} style={{ ...tdStyle, textAlign: 'center', color: 'var(--color-text-tertiary)', padding: '24px 14px' }}>
                    No features configured
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <button
          type="button"
          onClick={() => setFeatures(prev => [...prev, {
            feature_name: '',
            cost_alert_threshold_usd: 1.0,
            pii_masking_enabled: true,
            webhook_url: '',
            cross_model_enabled: false,
            cross_model_provider_url: '',
            cross_model_api_key: '',
            cross_model_model: '',
          }])}
          style={{
            background: 'none', border: 'none', cursor: 'pointer', padding: 0,
            fontSize: 'var(--sz-base)', color: '#185FA5',
          }}
        >
          + Add feature
        </button>
      </section>

      {/* ── Save ── */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <button
          type="button"
          onClick={save}
          disabled={mutation.isPending}
          style={{
            background: '#185FA5',
            color: '#fff',
            border: 'none',
            borderRadius: 7,
            padding: '9px 20px',
            fontSize: 'var(--sz-base)',
            fontWeight: 500,
            cursor: mutation.isPending ? 'default' : 'pointer',
            opacity: mutation.isPending ? 0.6 : 1,
            transition: 'opacity 0.15s',
          }}
        >
          {mutation.isPending ? 'Saving…' : 'Save Settings'}
        </button>
        {mutation.isSuccess && (
          <span style={{ fontSize: 'var(--sz-base)', color: '#0F6E56', fontWeight: 500 }}>Settings saved</span>
        )}
        {mutation.isError && (
          <span style={{ fontSize: 'var(--sz-base)', color: '#A32D2D' }}>Failed to save</span>
        )}
      </div>

    </div>
  )
}
