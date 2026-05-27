import { useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  IconLayoutDashboard,
  IconActivity,
  IconBell,
  IconSettings,
  IconGitBranch,
} from '@tabler/icons-react'
import { format } from 'date-fns'
import { fetchJSON } from '../api/client'
import type { SessionsResponse } from '../api/types'

const nav = [
  { path: '/overview', label: 'Overview', Icon: IconLayoutDashboard },
  { path: '/traces',   label: 'Traces',   Icon: IconActivity },
  { path: '/sessions', label: 'Sessions', Icon: IconGitBranch },
  { path: '/alerts',   label: 'Alerts',   Icon: IconBell },
  { path: '/settings', label: 'Settings', Icon: IconSettings },
]

const PAGE_META: Record<string, { title: string; badge?: string }> = {
  '/overview': { title: 'Overview',  badge: 'Today' },
  '/traces':   { title: 'Traces',    badge: 'Live feed' },
  '/sessions': { title: 'Sessions' },
  '/alerts':   { title: 'Alerts' },
  '/settings': { title: 'Settings' },
}

export default function Layout() {
  const location = useLocation()
  const [hovered, setHovered] = useState<string | null>(null)
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

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden', background: 'var(--color-background-tertiary)' }}>

      {/* ── Sidebar ── */}
      <aside style={{
        width: 196,
        flexShrink: 0,
        background: 'var(--color-background-secondary)',
        borderRight: '0.5px solid var(--color-border-tertiary)',
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
            }}>A</div>
            <div>
              <div style={{ fontSize: 14, fontWeight: 500, color: 'var(--color-text-primary)', lineHeight: 1.2 }}>ajah</div>
              <div style={{ fontSize: 10, color: 'var(--color-text-tertiary)', marginTop: 2 }}>LLM observability</div>
            </div>
          </div>
        </div>

        {/* Nav */}
        <nav style={{ flex: 1, padding: 8, display: 'flex', flexDirection: 'column', gap: 1 }}>
          {nav.map(({ path, label, Icon }) => (
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
                color: isActive ? '#185FA5' : hovered === path ? 'var(--color-text-primary)' : 'var(--color-text-secondary)',
                background: isActive
                  ? 'rgba(24,95,165,0.13)'
                  : hovered === path
                  ? 'var(--color-background-primary)'
                  : 'transparent',
                textDecoration: 'none',
                transition: 'background 0.12s, color 0.12s',
              })}
            >
              <Icon size={15} strokeWidth={1.75} />
              {label}
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
            </NavLink>
          ))}
        </nav>

        {/* Footer */}
        <div style={{ padding: '12px 14px', borderTop: '0.5px solid var(--color-border-tertiary)' }}>
          <span style={{ fontSize: 10, color: 'var(--color-text-tertiary)' }}>v0.1.0 · MIT license</span>
        </div>
      </aside>

      {/* ── Right column ── */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>

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
          <span style={{ fontSize: 12, color: 'var(--color-text-tertiary)' }}>
            {format(new Date(), 'MMMM d, yyyy')}
          </span>
        </div>

        {/* Scrollable content */}
        <main style={{ flex: 1, overflowY: 'auto' }}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
