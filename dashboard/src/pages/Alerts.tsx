import type { CSSProperties } from 'react'
import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import {
  IconAlertTriangle,
  IconShieldOff,
  IconInfoCircle,
} from '@tabler/icons-react'
import { fetchJSON } from '../api/client'
import type { Alert } from '../api/types'

// ── Static demo alerts (shown when no real alerts exist) ───────────────────

interface DemoAlert {
  id: string
  icon: typeof IconAlertTriangle
  iconColor: string
  iconBg: string
  title: string
  meta: string
  chips: { label: string; value: string }[]
}

const DEMO_ALERTS: DemoAlert[] = [
  {
    id: 'a1',
    icon: IconAlertTriangle,
    iconColor: '#854F0B',
    iconBg: 'rgba(133,79,11,0.12)',
    title: 'Cost spike — summarize feature',
    meta: 'Today at 11:25 · Threshold exceeded',
    chips: [
      { label: 'threshold', value: '$0.002' },
      { label: 'actual',    value: '$0.00414' },
      { label: 'model',     value: 'gpt-4o' },
    ],
  },
  {
    id: 'a2',
    icon: IconShieldOff,
    iconColor: '#A32D2D',
    iconBg: 'rgba(163,45,45,0.12)',
    title: 'PII detected in translate output',
    meta: 'Today at 09:12 · Email address in response',
    chips: [
      { label: 'type',   value: 'EMAIL' },
      { label: 'user',   value: 'user_4' },
      { label: 'masked', value: '✓' },
    ],
  },
  {
    id: 'a3',
    icon: IconInfoCircle,
    iconColor: '#185FA5',
    iconBg: 'rgba(24,95,165,0.12)',
    title: 'Quality degradation — chat feature',
    meta: 'Today at 12:00 · Score dropped below threshold',
    chips: [
      { label: 'avg score',  value: '0.45' },
      { label: 'threshold',  value: '0.70' },
      { label: 'model',      value: 'llama-3.3-70b' },
    ],
  },
]

// ── Styles ─────────────────────────────────────────────────────────────────

const cardStyle: CSSProperties = {
  background: 'var(--color-background-secondary)',
  border: '0.5px solid var(--color-border-tertiary)',
  borderRadius: 10,
  padding: '16px 20px',
  display: 'flex',
  alignItems: 'flex-start',
  gap: 16,
}

function Chip({ label, value }: { label: string; value: string }) {
  return (
    <span style={{
      fontSize: 11,
      color: 'var(--color-text-secondary)',
      background: 'var(--color-background-primary)',
      border: '0.5px solid var(--color-border-tertiary)',
      borderRadius: 4,
      padding: '2px 8px',
    }}>
      <span style={{ color: 'var(--color-text-tertiary)' }}>{label} </span>
      {value}
    </span>
  )
}

// ── Page ───────────────────────────────────────────────────────────────────

export default function Alerts() {
  const { data = [], isLoading, error } = useQuery<Alert[]>({
    queryKey: ['alerts'],
    queryFn: () => fetchJSON('/metrics/alerts'),
  })

  if (isLoading) return <div style={{ padding: 24, color: 'var(--color-text-tertiary)', fontSize: 13 }}>Loading…</div>
  if (error)     return <div style={{ padding: 24, color: '#A32D2D', fontSize: 13 }}>Failed to load alerts</div>

  // Real alerts from API
  if (data.length > 0) {
    return (
      <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 12 }}>
        {data.map((alert, i) => (
          <div key={i} style={cardStyle}>
            <div style={{
              width: 36, height: 36, borderRadius: 8, flexShrink: 0,
              background: 'rgba(133,79,11,0.12)',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>
              <IconAlertTriangle size={18} color="#854F0B" strokeWidth={1.75} />
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <p style={{ margin: '0 0 4px', fontSize: 14, fontWeight: 500, color: 'var(--color-text-primary)' }}>
                Cost spike — {alert.feature} feature
              </p>
              <p style={{ margin: '0 0 10px', fontSize: 12, color: 'var(--color-text-tertiary)' }}>
                {format(new Date(alert.timestamp), 'MMM d · HH:mm')} · Threshold exceeded
              </p>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                <Chip label="threshold" value={`$${alert.threshold.toFixed(4)}`} />
                <Chip label="actual"    value={`$${alert.actual.toFixed(4)}`} />
              </div>
            </div>
          </div>
        ))}
      </div>
    )
  }

  // Demo alerts
  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 12 }}>
      {DEMO_ALERTS.map(alert => {
        const Icon = alert.icon
        return (
          <div key={alert.id} style={cardStyle}>
            <div style={{
              width: 36, height: 36, borderRadius: 8, flexShrink: 0,
              background: alert.iconBg,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>
              <Icon size={18} color={alert.iconColor} strokeWidth={1.75} />
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <p style={{ margin: '0 0 4px', fontSize: 14, fontWeight: 500, color: 'var(--color-text-primary)' }}>
                {alert.title}
              </p>
              <p style={{ margin: '0 0 10px', fontSize: 12, color: 'var(--color-text-tertiary)' }}>
                {alert.meta}
              </p>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {alert.chips.map(c => <Chip key={c.label} {...c} />)}
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}
