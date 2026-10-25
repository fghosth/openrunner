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
export type Athlete = { id: number; name: string; sex: string }
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
  /** 有可用 GPS 轨迹（可在地图上查看）；与 hasStream 区分 */
  hasGps: boolean
  source: string
}
export type ManualActivityInput = {
  date: string
  sport: string
  durationSecs: number
  distanceM: number
  avgHr: number
  isPlanned: boolean
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
  rtss: number
  np: number
  powerLoad: number
  powerTiZ: number[]
  paceTiZ: number[]
  isEstimated: boolean
}
export type Season = {
  id?: number
  name: string
  startDate: string
  endDate: string
  seed: number
}
export type CsZone = {
  id?: number
  domain: 'speed' | 'power'
  validFrom: string
  validTo: string
  cs: number
  dprime: number
  source: string
}
export type CsFitResult = { fitted: boolean; reason?: string; cs?: number; dprime?: number; r2?: number }
export type DBalSeries = { cs: number; dprime: number; dbal: number[]; intervalSecs: number }
export type WBalSeries = { cp: number; wprime: number; tau: number; wbal: number[]; intervalSecs: number }
export type PMCSeries = {
  dates: string[]
  stress: number[]
  lts: number[]
  sts: number[]
  sb: number[]
  rr: number[]
  plannedLts: number[]
  plannedSts: number[]
  plannedSb: number[]
  expectedLts: number[]
  expectedSts: number[]
  expectedSb: number[]
}
export type PaceZone = {
  id?: number
  validFrom: string
  validTo: string
  thresholdPaceSecsPerKm: number
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

export type ObservedHr = {
  /** 建议填入的最大心率（bpm）= 数据质量达标活动中的峰值；无数据时为 0 */
  maxHr: number
  /** 持续 30 秒的最高均值，比 maxHr 保守，用作可信度参照 */
  sustainedHr: number
  /** 参与统计的有效样本数 */
  samples: number
  /** 数据质量达标、被采信的活动数 */
  activities: number
  /** 数据质量不足（多为力量训练/球类的光电伪影）而被剔除的活动数 */
  excluded: number
  peakActivityId?: number
  peakActivityAt?: string
}

export const hrZones = {
  list: (athleteId: number) => authedFetch<HrZone[]>(`/api/athletes/${athleteId}/hr-zones`),
  upsert: (athleteId: number, z: HrZone) =>
    authedFetch<HrZone>(`/api/athletes/${athleteId}/hr-zones`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(z),
    }),
  /** 扫描历史活动心率，反推观测 MaxHR（用于给出建议值）。 */
  observed: (athleteId: number) =>
    authedFetch<ObservedHr>(`/api/athletes/${athleteId}/hr-observed`),
}

export const paceZones = {
  list: (athleteId: number) => authedFetch<PaceZone[]>(`/api/athletes/${athleteId}/pace-zones`),
  upsert: (athleteId: number, z: PaceZone) =>
    authedFetch<PaceZone>(`/api/athletes/${athleteId}/pace-zones`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(z),
    }),
}

export const seasons = {
  list: (athleteId: number) => authedFetch<Season[]>(`/api/athletes/${athleteId}/seasons`),
  upsert: (athleteId: number, s: Season) =>
    authedFetch<Season>(`/api/athletes/${athleteId}/seasons`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(s),
    }),
  remove: (athleteId: number, id: number) =>
    authedFetch<void>(`/api/athletes/${athleteId}/seasons/${id}`, { method: 'DELETE' }),
}

export const csZones = {
  list: (athleteId: number) => authedFetch<CsZone[]>(`/api/athletes/${athleteId}/cs-zones`),
  upsert: (athleteId: number, z: CsZone) =>
    authedFetch<CsZone>(`/api/athletes/${athleteId}/cs-zones`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(z),
    }),
}
export const fitCS = (athleteId: number, domain: 'speed' | 'power' = 'speed') =>
  authedFetch<CsFitResult>(`/api/athletes/${athleteId}/fit-cs?domain=${domain}`, { method: 'POST' })
export const dbal = (athleteId: number, activityId: number) =>
  authedFetch<DBalSeries>(`/api/athletes/${athleteId}/activities/${activityId}/dbal`)
export const wbal = (athleteId: number, activityId: number) =>
  authedFetch<WBalSeries>(`/api/athletes/${athleteId}/activities/${activityId}/wbal`)

/** 下载年度 SVG 海报（需带 Authorization，走 blob）。 */
export async function downloadPoster(athleteId: number, year: number): Promise<void> {
  const token = getToken()
  const res = await fetch(`/api/athletes/${athleteId}/poster.svg?year=${year}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok) throw new ApiError(res.status, '生成海报失败')
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `openrunner-${year}-poster.svg`
  a.click()
  URL.revokeObjectURL(url)
}

/** 导出全量 JSON：直接走浏览器下载（带 Authorization 需 blob 方式）。 */
export async function downloadExport(athleteId: number): Promise<void> {
  const token = getToken()
  const res = await fetch(`/api/athletes/${athleteId}/export`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok) throw new ApiError(res.status, '导出失败')
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `openrunner-athlete-${athleteId}.json`
  a.click()
  URL.revokeObjectURL(url)
}

export const platforms = {
  list: (athleteId: number) => authedFetch<PlatformConnection[]>(`/api/athletes/${athleteId}/platforms`),
  upsert: (
    athleteId: number,
    platform: string,
    cred: { apiKey?: string; oauth1Token?: string; oauth1Secret?: string; region?: 'global' | 'cn' },
  ) =>
    authedFetch<PlatformConnection>(`/api/athletes/${athleteId}/platforms/${platform}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cred),
    }),
  remove: (athleteId: number, platform: string) =>
    authedFetch<void>(`/api/athletes/${athleteId}/platforms/${platform}`, { method: 'DELETE' }),
  /** full=true 时忽略游标，从最早日期重新拉全部历史（首次回填/补历史用）。 */
  sync: (athleteId: number, platform: string, full = false) =>
    authedFetch<SyncResult>(
      `/api/athletes/${athleteId}/platforms/${platform}/sync${full ? '?full=1' : ''}`,
      { method: 'POST' },
    ),
}

export const tracks = (athleteId: number, max = 300) =>
  authedFetch<Track[]>(`/api/athletes/${athleteId}/activities/tracks?max=${max}`)
export const annualStats = (athleteId: number, year: number) =>
  authedFetch<AnnualStat>(`/api/athletes/${athleteId}/stats/annual?year=${year}`)

export const pmc = (athleteId: number, metric: 'trimp' | 'rtss') =>
  authedFetch<PMCSeries>(`/api/athletes/${athleteId}/pmc?metric=${metric}`)

export const athletes = {
  list: () => authedFetch<Athlete[]>('/api/athletes'),
  create: (name: string) =>
    authedFetch<Athlete>('/api/athletes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  rename: (id: number, name: string, sex?: string) =>
    authedFetch<Athlete>(`/api/athletes/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, sex }),
    }),
  remove: (id: number) => authedFetch<void>(`/api/athletes/${id}`, { method: 'DELETE' }),
}

export type Track = {
  id: number
  sport: string
  startTime: number
  distanceM: number
  durationSecs: number
  avgHr: number
  points: [number, number][]
}
export type MonthStat = {
  month: number
  distanceM: number
  durationSecs: number
  count: number
  activeDays: number
}
export type AnnualStat = {
  year: number
  totalDistanceM: number
  totalDurationSecs: number
  activityCount: number
  activeDays: number
  monthly: MonthStat[]
  streaks: { longest: number; current: number }
}
export type PlatformConnection = {
  id: number
  platform: string
  region: string
  credentialKind: string
  syncCursor: string
  lastSyncAt: number
  lastError: string
  enabled: boolean
  hasCredential: boolean
}
export type SyncResult = {
  imported: number
  skipped: number
  failed: number
  status: string
  message: string
  cursor: string
}
export type BatchResult = {
  imported: number
  skipped: number
  failed: number
  results: { file: string; status: string; reason?: string; activityId?: number }[]
}

export const activities = {
  importBatch: (athleteId: number, files: File[]) => {
    const body = new FormData()
    for (const f of files) body.append('file', f)
    return authedFetch<BatchResult>(`/api/athletes/${athleteId}/activities/import-batch`, {
      method: 'POST',
      body,
    })
  },
  createManual: (athleteId: number, input: ManualActivityInput) =>
    authedFetch<Activity>(`/api/athletes/${athleteId}/activities`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    }),
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
  recomputeAll: (athleteId: number) =>
    authedFetch<{ total: number; computed: number; skipped: number; failed: number }>(
      `/api/athletes/${athleteId}/recompute-all`,
      { method: 'POST' },
    ),
  recompute: (athleteId: number, id: number) =>
    authedFetch<ActivityMetrics>(`/api/athletes/${athleteId}/activities/${id}/recompute`, {
      method: 'POST',
    }),
}

// ---------- 首页概览（统计卡片 / 贡献热力图 / 个人最佳）----------

export type PeriodTotals = {
  distanceM: number
  count: number
  durationSecs: number
  elevationGainM: number
}

export type DaySummary = {
  /** YYYY-MM-DD（本地日期） */
  date: string
  distanceM: number
  durationSecs: number
  count: number
}

export type SportTotals = { sport: string; distanceM: number; count: number }

export type Dashboard = {
  year: number
  /** 有数据的年份，倒序 */
  years: number[]
  /** 选定年份的逐日摘要（热力图数据源） */
  daily: DaySummary[]
  totals: { all: PeriodTotals; year: PeriodTotals; month: PeriodTotals; week: PeriodTotals }
  bySport: SportTotals[]
}

export type PersonalBest = {
  label: string
  distanceM: number
  seconds: number
  paceSecsPerKm: number
  activityId: number
  activityStart: number
}

export const dashboard = (athleteId: number, year?: number) =>
  authedFetch<Dashboard>(
    `/api/athletes/${athleteId}/dashboard${year ? `?year=${year}` : ''}`,
  )

/** 各目标距离的最快分段（扫描速度流，可能较慢 → 独立加载） */
export const personalBest = (athleteId: number) =>
  authedFetch<PersonalBest[]>(`/api/athletes/${athleteId}/personal-best`)
