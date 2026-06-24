import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { setAuthToken } from '../api/client'

const BASE = 'http://localhost:8080'

export default function Login() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch(`${BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
      const data = await res.json()
      if (!res.ok) {
        setError(data.error ?? 'Login failed')
        return
      }
      setAuthToken(data.token, data.user)
      navigate('/overview')
    } catch {
      setError('Network error — is the gateway running?')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{
      minHeight: '100vh',
      background: 'var(--color-background-primary)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
    }}>
      <div style={{
        width: 360,
        background: 'var(--color-background-secondary)',
        border: '0.5px solid var(--color-border-tertiary)',
        borderRadius: 12,
        padding: '32px 28px',
      }}>
        {/* Logo */}
        <div style={{ marginBottom: 28, textAlign: 'center' }}>
          <div style={{ fontSize: 22, fontWeight: 700, color: 'var(--color-text-primary)', letterSpacing: '-0.02em' }}>
            ajah
          </div>
          <div style={{ fontSize: 13, color: 'var(--color-text-tertiary)', marginTop: 4 }}>
            LLM observability
          </div>
        </div>

        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          <div>
            <label style={{ display: 'block', fontSize: 12, color: 'var(--color-text-tertiary)', marginBottom: 5, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              Email
            </label>
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              required
              autoFocus
              style={{
                width: '100%',
                background: 'var(--color-background-primary)',
                border: '0.5px solid var(--color-border-secondary)',
                borderRadius: 6,
                padding: '9px 12px',
                color: 'var(--color-text-primary)',
                fontSize: 14,
                outline: 'none',
                boxSizing: 'border-box',
              }}
              placeholder="you@company.com"
            />
          </div>

          <div>
            <label style={{ display: 'block', fontSize: 12, color: 'var(--color-text-tertiary)', marginBottom: 5, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              Password
            </label>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              required
              style={{
                width: '100%',
                background: 'var(--color-background-primary)',
                border: '0.5px solid var(--color-border-secondary)',
                borderRadius: 6,
                padding: '9px 12px',
                color: 'var(--color-text-primary)',
                fontSize: 14,
                outline: 'none',
                boxSizing: 'border-box',
              }}
              placeholder="••••••••"
            />
          </div>

          {error && (
            <div style={{
              fontSize: 13,
              color: 'var(--color-danger)',
              background: 'var(--color-danger-glow)',
              border: '0.5px solid var(--color-danger)',
              borderRadius: 6,
              padding: '8px 12px',
            }}>
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            style={{
              marginTop: 4,
              padding: '10px',
              background: loading ? 'var(--color-border-secondary)' : 'var(--color-accent)',
              border: 'none',
              borderRadius: 6,
              color: '#fff',
              fontSize: 14,
              fontWeight: 600,
              cursor: loading ? 'not-allowed' : 'pointer',
              transition: 'background 0.15s',
            }}
          >
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>

        <div style={{ marginTop: 20, textAlign: 'center', fontSize: 13, color: 'var(--color-text-tertiary)' }}>
          No account?{' '}
          <Link to="/register" style={{ color: 'var(--color-accent)', textDecoration: 'none', fontWeight: 500 }}>
            Create one
          </Link>
        </div>
      </div>
    </div>
  )
}
