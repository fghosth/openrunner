// 与后端 API 的交互封装：token 存 localStorage，401 时清凭证回登录页。
const TOKEN_KEY = 'openrunner.token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body) headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(`/api${path}`, { ...init, headers })
  if (res.status === 401) {
    clearToken()
    throw new ApiError(401, '登录已过期')
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `HTTP ${res.status}`)
  }
  return data as T
}

export type AuthStatus = { needsSetup: boolean }
export type Athlete = { id: number; name: string }

export const auth = {
  status: () => api<AuthStatus>('/auth/status'),
  setup: (username: string, password: string) =>
    api<{ token: string }>('/auth/setup', { method: 'POST', body: JSON.stringify({ username, password }) }),
  login: (username: string, password: string) =>
    api<{ token: string }>('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
}

export const athletes = {
  list: () => api<Athlete[]>('/athletes'),
  create: (name: string) => api<Athlete>('/athletes', { method: 'POST', body: JSON.stringify({ name }) }),
  rename: (id: number, name: string) =>
    api<Athlete>(`/athletes/${id}`, { method: 'PUT', body: JSON.stringify({ name }) }),
  remove: (id: number) => api<void>(`/athletes/${id}`, { method: 'DELETE' }),
}
