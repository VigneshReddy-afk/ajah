import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Layout from './components/Layout'
import Overview from './pages/Overview'
import Traces from './pages/Traces'
import Pulsar from './pages/Pulsar'
import Sessions from './pages/Sessions'
import Warnings from './pages/Warnings'
import Alerts from './pages/Alerts'
import Settings from './pages/Settings'
import Evals from './pages/Evals'
import Login from './pages/Login'
import Register from './pages/Register'
import { isAuthenticated } from './api/client'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchInterval: 30_000,
      staleTime: 5_000,
      retry: 1,
    },
  },
})

function RequireAuth({ children }: { children: React.ReactNode }) {
  if (!isAuthenticated()) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/register" element={<Register />} />
          <Route
            path="/"
            element={
              <RequireAuth>
                <Layout />
              </RequireAuth>
            }
          >
            <Route index element={<Navigate to="/overview" replace />} />
            <Route path="overview" element={<Overview />} />
            <Route path="traces" element={<Traces />} />
            <Route path="pulsar" element={<Pulsar />} />
            <Route path="sessions" element={<Sessions />} />
            <Route path="warnings" element={<Warnings />} />
            <Route path="alerts" element={<Alerts />} />
            <Route path="settings" element={<Settings />} />
            <Route path="evals" element={<Evals />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
