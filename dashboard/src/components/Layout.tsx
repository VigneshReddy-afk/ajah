import { NavLink, Outlet } from 'react-router-dom'

const nav = [
  { path: '/overview', label: 'Overview' },
  { path: '/traces',   label: 'Traces' },
  { path: '/alerts',   label: 'Alerts' },
  { path: '/settings', label: 'Settings' },
]

export default function Layout() {
  return (
    <div className="flex h-screen bg-gray-950 text-gray-100 overflow-hidden">
      <aside className="w-52 shrink-0 bg-gray-900 border-r border-gray-800 flex flex-col">
        <div className="px-5 py-4 border-b border-gray-800">
          <div className="text-base font-bold text-white tracking-tight">ajah</div>
          <div className="text-xs text-gray-500 mt-0.5">LLM Observability</div>
        </div>
        <nav className="flex-1 p-2 space-y-0.5">
          {nav.map(({ path, label }) => (
            <NavLink
              key={path}
              to={path}
              className={({ isActive }) =>
                `block px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
                  isActive
                    ? 'bg-indigo-600 text-white'
                    : 'text-gray-400 hover:text-white hover:bg-gray-800'
                }`
              }
            >
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="px-5 py-3 border-t border-gray-800">
          <p className="text-xs text-gray-600">v0.1.0</p>
        </div>
      </aside>
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
