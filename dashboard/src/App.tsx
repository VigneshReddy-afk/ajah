import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Layout from './components/Layout'
import Overview from './pages/Overview'
import Traces from './pages/Traces'
import Alerts from './pages/Alerts'
import Settings from './pages/Settings'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchInterval: 30_000,
      staleTime: 5_000,
      retry: 1,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<Navigate to="/overview" replace />} />
            <Route path="overview" element={<Overview />} />
            <Route path="traces" element={<Traces />} />
            <Route path="alerts" element={<Alerts />} />
            <Route path="settings" element={<Settings />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
