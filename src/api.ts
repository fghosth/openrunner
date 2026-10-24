// 与后端 API 的交互封装：token 存 localStorage，401 时清凭证并抛 ApiError(401)。
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

async function authedFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(path, { ...init, headers })
  if (res.status === 401) {
    clearToken()
    throw new ApiError(401, '登录已过期')
  }
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new ApiError(res.status, data?.error ?? `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export type AuthStatus = { needsSetup: boolean }
export type Athlete = { id: number; name: string }
export type Activity = {
  id: number
  athleteId: number
  sport: string
  startTime: number // unix 秒
  durationSecs: number
  timerSecs: number
  distanceM: number
  elevationGainM: number
  isPlanned: boolean
  hasStream: boolean
  source: string
}
export type HrZone = {
  id?: number
  validFrom: string
  validTo: string
  maxHr: number
  restHr: number
  ltHr: number
}
export type ActivityMetrics = {
  avgHr: number
  trimp: number
  trimp100: number
  trimpZonal: number
  hrTiZ: number[]
  isEstimated: boolean
}
export type ActivityDetailT = Activity & { metrics: ActivityMetrics | null }

export const auth = {
  status: () => authedFetch<AuthStatus>('/api/auth/status'),
  setup: (username: string, password: string) =>
    authedFetch<{ token: string }>('/api/auth/setup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    }),
  login: (username: string, password: string) =>
    authedFetch<{ token: string }>('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    }),
}

export const hrZones = {
  list: (athleteId: number) => authedFetch<HrZone[]>(`/api/athletes/${athleteId}/hr-zones`),
  upsert: (athleteId: number, z: HrZone) =>
    authedFetch<HrZone>(`/api/athletes/${athleteId}/hr-zones`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(z),
    }),
}

export const athletes = {
  list: () => authedFetch<Athlete[]>('/api/athletes'),
  create: (name: string) =>
    authedFetch<Athlete>('/api/athletes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  rename: (id: number, name: string) =>
    authedFetch<Athlete>(`/api/athletes/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  remove: (id: number) => authedFetch<void>(`/api/athletes/${id}`, { method: 'DELETE' }),
}

export const activities = {
  importFIT: (athleteId: number, file: File) => {
    const body = new FormData()
    body.append('file', file)
    // 注意：不要手动设 Content-Type，让浏览器带 boundary
    return authedFetch<Activity>(`/api/athletes/${athleteId}/activities/import`, {
      method: 'POST',
      body,
    })
  },
  list: (athleteId: number) => authedFetch<Activity[]>(`/api/athletes/${athleteId}/activities`),
  detail: (athleteId: number, id: number) =>
    authedFetch<ActivityDetailT>(`/api/athletes/${athleteId}/activities/${id}`),
  recompute: (athleteId: number, id: number) =>
    authedFetch<ActivityMetrics>(`/api/athletes/${athleteId}/activities/${id}/recompute`, {
      method: 'POST',
    }),
}
