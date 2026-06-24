const BASE = 'http://localhost:8080'

function getToken(): string {
  return localStorage.getItem('ajah_token') ?? ''
}

function authHeaders(): HeadersInit {
  const token = getToken()
  return token ? { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` } : { 'Content-Type': 'application/json' }
}

export async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { headers: authHeaders() })
  if (res.status === 401) {
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
  if (res.status === 401) {
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
  return !!localStorage.getItem('ajah_token')
}
