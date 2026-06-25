const BASE = 'http://localhost:8080'

// AUTH_DISABLED is set to true when the gateway reports auth is not required.
// Checked once on startup via /auth/me — cached for the session.
let authDisabled = false

export function setAuthDisabled(v: boolean) {
  authDisabled = v
}

export function isAuthDisabled(): boolean {
  return authDisabled
}

function getToken(): string {
  return localStorage.getItem('ajah_token') ?? ''
}

function authHeaders(): HeadersInit {
  const token = getToken()
  return token
    ? { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` }
    : { 'Content-Type': 'application/json' }
}

export async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { headers: authHeaders() })
  if (res.status === 401 && !authDisabled) {
    localStorage.removeItem('ajah_token')
    localStorage.removeItem('ajah_user')
    window.location.href = '/login'
    throw new Error('Unauthorized')
  }
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json() as Promise<T>
}

export async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(body),
  })
  if (res.status === 401 && !authDisabled) {
    localStorage.removeItem('ajah_token')
    localStorage.removeItem('ajah_user')
    window.location.href = '/login'
    throw new Error('Unauthorized')
  }
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json() as Promise<T>
}

export async function deleteJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json() as Promise<T>
}

export function setAuthToken(token: string, user: { email: string; role: string; id: string }) {
  localStorage.setItem('ajah_token', token)
  localStorage.setItem('ajah_user', JSON.stringify(user))
}

export function clearAuth() {
  localStorage.removeItem('ajah_token')
  localStorage.removeItem('ajah_user')
}

export function getStoredUser(): { email: string; role: string; id: string } | null {
  try {
    const raw = localStorage.getItem('ajah_user')
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

export function isAuthenticated(): boolean {
  if (authDisabled) return true
  return !!localStorage.getItem('ajah_token')
}

// checkAuthMode calls /auth/me to determine if auth is required.
// Call this once on app startup before rendering any protected routes.
export async function checkAuthMode(): Promise<boolean> {
  try {
    const res = await fetch(`${BASE}/auth/me`, { headers: authHeaders() })
    if (!res.ok) return false
    const data = await res.json()
    if (data.auth_enabled === false) {
      authDisabled = true
      return true // auth disabled — let user in
    }
    // Auth enabled — check if we have a valid token
    return !!localStorage.getItem('ajah_token')
  } catch {
    return false
  }
}
