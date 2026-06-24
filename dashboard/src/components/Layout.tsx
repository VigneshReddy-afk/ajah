import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  IconLayoutDashboard,
  IconActivity,
  IconRadar2,
  IconAlertHexagon,
  IconBell,
  IconSettings,
  IconGitBranch,
  IconTestPipe,
} from '@tabler/icons-react'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { SessionsResponse, WarningsResponse } from '../api/types'

const nav = [
  { path: '/overview',  label: 'Overview',  Icon: IconLayoutDashboard },
  { path: '/traces',    label: 'Traces',    Icon: IconActivity },
  { path: '/pulsar',    label: 'Pulsar',    Icon: IconRadar2, live: true },
  { path: '/sessions',  label: 'Sessions',  Icon: IconGitBranch },
  { path: '/warnings',  label: 'Warnings',  Icon: IconAlertHexagon },
  { path: '/alerts',    label: 'Alerts',    Icon: IconBell },
  { path: '/evals',     label: 'Evals',     Icon: IconTestPipe },
  { path: '/settings',  label: 'Settings',  Icon: IconSettings },
]

const PAGE_META: Record<string, { title: string; badge?: string }> = {
  '/overview': { title: 'Overview',  badge: 'Today' },
  '/traces':   { title: 'Traces',    badge: 'Live feed' },
  '/pulsar':   { title: 'Pulsar' },
  '/sessions': { title: 'Sessions' },
  '/warnings': { title: 'Warnings' },
  '/alerts':   { title: 'Alerts' },
  '/evals':    { title: 'Evals',     badge: 'Beta' },
  '/settings': { title: 'Settings' },
}

type TextSize = 'small' | 'medium' | 'large'

export default function Layout() {
  const location = useLocation()
  const [hovered, setHovered] = useState<string | null>(null)

  const [textSize, setTextSize] = useState<TextSize>(() =>
    (localStorage.getItem('ajah-text-size') as TextSize) ?? 'medium'
  )

  useEffect(() => {
    document.documentElement.classList.remove('sz-sm', 'sz-lg')
    if (textSize === 'small')  document.documentElement.classList.add('sz-sm')
    if (textSize === 'large')  document.documentElement.classList.add('sz-lg')
    localStorage.setItem('ajah-text-size', textSize)
  }, [textSize])
  const meta = PAGE_META[location.pathname] ?? { title: 'Dashboard' }

  const { data: sessionsData } = useQuery<SessionsResponse>({
    queryKey: ['sessions'],
    queryFn: () => fetchJSON<SessionsResponse>('/sessions'),
    staleTime: 30_000,
  })
  const todayUTC = new Date().toISOString().slice(0, 10)
  const sessionsToday = (sessionsData?.sessions ?? []).filter(s =>
    s.start_time.startsWith(todayUTC)
  ).length

  const { data: warningsData } = useQuery<WarningsResponse>({
    queryKey: ['warnings'],
    queryFn: () => fetchJSON<WarningsResponse>('/warnings'),
    refetchInterval: 10_000,
  })
  const highRiskCount = (warningsData?.warnings ?? []).length

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden', background: 'var(--color-background-tertiary)' }}>

      {/* ── Sidebar ── */}
      <aside style={{
        width: 196,
        flexShrink: 0,
        background: 'var(--color-background-secondary)',
        borderRight: '1px solid var(--color-border-tertiary)',
        display: 'flex',
        flexDirection: 'column',
      }}>

        {/* Logo */}
        <div style={{ padding: '16px 14px 14px', borderBottom: '0.5px solid var(--color-border-tertiary)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div style={{
              width: 26, height: 26, borderRadius: 6,
              background: '#185FA5',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              fontSize: 12, fontWeight: 700, color: '#fff',
              flexShrink: 0, letterSpacing: '-0.5px',
              boxShadow: '0 0 16px rgba(45,125,210,0.3)',
              transition: 'box-shadow 0.3s ease',
            }}>A</div>
            <div>
              <div style={{ fontSize: 14, fontWeight: 500, color: 'var(--color-text-primary)', lineHeight: 1.2 }}>ajah</div>
              <div style={{ fontSize: 10, color: 'var(--color-text-tertiary)', marginTop: 2 }}>LLM observability</div>
            </div>
          </div>
        </div>

        {/* Nav */}
        <nav style={{ flex: 1, padding: 8, display: 'flex', flexDirection: 'column', gap: 1 }}>
          {nav.map(({ path, label, Icon, live }) => (
            <NavLink
              key={path}
              to={path}
              onMouseEnter={() => setHovered(path)}
              onMouseLeave={() => setHovered(null)}
              style={({ isActive }) => ({
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                padding: '7px 10px',
                borderRadius: 6,
                fontSize: 13,
                fontWeight: isActive ? 500 : 400,
                color: isActive ? 'var(--color-accent)' : hovered === path ? 'var(--color-text-primary)' : 'var(--color-text-secondary)',
                background: isActive
                  ? 'var(--color-accent-hover)'
                  : hovered === path
                  ? 'var(--color-background-primary)'
                  : 'transparent',
                textDecoration: 'none',
                transition: 'background 0.12s, color 0.12s',
              })}
            >
              <Icon size={15} strokeWidth={1.75} />
              {label}
              {live && (
                <span style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center' }}>
                  <span style={{
                    width: 6, height: 6, borderRadius: '50%', flexShrink: 0,
                    background: location.pathname === path ? '#185FA5' : 'var(--color-text-tertiary)',
                    animation: 'pulsarDot 1.4s ease-in-out infinite',
                  }} />
                </span>
              )}
              {path === '/sessions' && sessionsToday > 0 && (
                <span style={{
                  marginLeft: 'auto',
                  fontSize: 10, fontWeight: 600,
                  color: '#185FA5',
                  background: 'rgba(24,95,165,0.15)',
                  borderRadius: 10,
                  padding: '1px 6px',
                  lineHeight: 1.5,
                }}>
                  {sessionsToday}
                </span>
              )}
              {path === '/warnings' && highRiskCount > 0 && (
                <span style={{
                  marginLeft: 'auto',
                  fontSize: 10, fontWeight: 600,
                  color: '#A32D2D',
                  background: 'rgba(163,45,45,0.15)',
                  borderRadius: 10,
                  padding: '1px 6px',
                  lineHeight: 1.5,
                }}>
                  {highRiskCount}
                </span>
              )}
            </NavLink>
          ))}
        </nav>

        {/* Footer */}
        <div style={{ padding: '12px 14px', borderTop: '0.5px solid var(--color-border-tertiary)' }}>
          <span style={{ fontSize: 10, color: 'var(--color-text-tertiary)' }}>v0.1.0 · MIT license</span>
        </div>
      </aside>

      {/* ── Right column ── */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', animation: 'fadeIn 0.25s ease forwards' }}>

        {/* Top bar */}
        <div style={{
          height: 52,
          flexShrink: 0,
          background: 'var(--color-background-secondary)',
          borderBottom: '0.5px solid var(--color-border-tertiary)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 24px',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--color-text-primary)' }}>
              {meta.title}
            </span>
            {meta.badge && (
              <span style={{
                fontSize: 11, fontWeight: 500,
                color: '#185FA5',
                background: 'rgba(24,95,165,0.13)',
                padding: '2px 8px',
                borderRadius: 4,
              }}>
                {meta.badge}
              </span>
            )}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {/* Text-size picker: A A A (small / medium / large) */}
            <div style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
              {(['small', 'medium', 'large'] as const).map((s) => {
                const letterPx = s === 'small' ? 10 : s === 'large' ? 15 : 12
                const active   = textSize === s
                return (
                  <button
                    key={s}
                    title={`${s.charAt(0).toUpperCase() + s.slice(1)} text`}
                    onClick={() => setTextSize(s)}
                    style={{
                      width: 28, height: 28, borderRadius: 6,
                      border: `1px solid ${active ? '#185FA5' : 'var(--color-border-secondary)'}`,
                      background: active ? 'rgba(24,95,165,0.15)' : 'transparent',
                      color: active ? '#185FA5' : 'var(--color-text-tertiary)',
                      fontSize: letterPx,
                      fontWeight: 700,
                      cursor: 'pointer',
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      transition: 'border-color 0.12s, background 0.12s, color 0.12s',
                      lineHeight: 1,
                      padding: 0,
                    }}
                  >
                    A
                  </button>
                )
              })}
            </div>

            <span style={{ fontSize: 12, color: 'var(--color-text-tertiary)' }}>
              {format(new Date(), 'MMMM d, yyyy')}
            </span>
          </div>
        </div>

        {/* Scrollable content */}
        <main style={{ flex: 1, overflowY: 'auto' }}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
