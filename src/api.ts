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
export type Athlete = {
  id: number
  name: string
  sex: string
  /** 主页资料卡头像；留空则显示首字母头像 */
  avatarUrl?: string
  /** 主页资料卡简介 */
  bio?: string
  /** 跑量目标（公里，0 = 未设置） */
  yearlyGoalKm?: number
  monthlyGoalKm?: number
  weeklyGoalKm?: number
}
/** 详情页的流派生统计（步频/步幅） */
export type StreamStats = {
  avgCadenceSpm: number
  maxCadenceSpm: number
  totalSteps: number
  avgStrideM: number
}

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
  /** 摘要平均心率（未知为 0） */
  avgHr: number
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
export type Cp3Fit = {
  cs: number
  dprime: number
  tau: number
  peak: number
  /** τ 未被数据约束住时 false —— 此时 peak 是外推值，不能当 Pmax 用 */
  peakReliable: boolean
  r2: number
  rmse: number
}

export type CsFitResult = {
  fitted: boolean
  reason?: string
  cs?: number
  dprime?: number
  r2?: number
  /** 参与拟合的活动数（每次活动最多贡献 1 个点） */
  activities?: number
  /** 三参数模型结果（与 2 参数并列，用于展示 Peak / Pmax） */
  cp3?: Cp3Fit | Record<string, never>
}
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
export type ActivityDetailT = Activity & {
  metrics: ActivityMetrics | null
  /** 步频/步幅等派生统计（无 cadence 数据时缺省） */
  stats?: StreamStats
}

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
  /** 保存凭证：字段随平台而异（见 platformMetas），统一按 JSON 键值对提交。 */
  save: (athleteId: number, platform: string, cred: Record<string, string>) =>
    authedFetch<PlatformConnection>(`/api/athletes/${athleteId}/platforms/${platform}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cred),
    }),
  upsert: (
    athleteId: number,
    platform: string,
    cred: Record<string, string>,
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
  /** 更新档案。avatarUrl / bio 只在传入时提交（不传则后端保持原值）。 */
  rename: (
    id: number,
    name: string,
    sex?: string,
    profile?: {
      avatarUrl?: string
      bio?: string
      yearlyGoalKm?: number
      monthlyGoalKm?: number
      weeklyGoalKm?: number
    },
  ) =>
    authedFetch<Athlete>(`/api/athletes/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, sex, ...profile }),
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

// ---------- coach 周校验（coach-engine 票 02/03）----------

export type CoachVerdict = {
  level: 'OK' | 'WARN' | 'FAIL'
  rule: string
  detail: string
  evidence?: string
}

export type CoachWeek = {
  plan_start: string
  week_role: string
  runs: number
  total_min: number
  total_km: number
  long_min: number
  history_sessions: number
}

export type CoachPlanUnit = {
  date: string
  type?: string
  minutes: number
  km?: number
}

export type CoachPaceZoneRow = {
  name: string
  paceSlow: number
  paceFast?: number
  pctLo: number
  pctHi?: number
  hrLo?: number
  hrHi?: number
  use: string
}

export type CoachDanielsRow = {
  name: string
  paceFast: number
  paceSlow: number
  use: string
}

export type CoachPaces = {
  error?: string
  threshold?: { secsPerKm: number; pace: string; source: string }
  lthr?: number
  vdot?: { value: number; race: { distanceM: number; timeSecs: number }; rows: CoachDanielsRow[] }
  zones8020?: CoachPaceZoneRow[]
}

export type CoachMarathonPrediction = {
  riegel: number
  vickers1?: number
  vickers2?: number
  tanda?: number
  recommended: number
  lowConfidence?: boolean
  notes?: string[]
}

export type CoachPredict = {
  error?: string
  base?: { distanceM: number; timeSecs: number; source: string }
  weekly_km?: number
  weeks_observed?: number
  training_pace?: number
  race2?: { distanceM: number; timeSecs: number }
  prediction?: CoachMarathonPrediction
}

export type CoachActivityAnalysis = {
  applicable: boolean
  reason?: string
  durationSecs: number
  decouplingPct?: number
  decouplingVerdict?: 'ok' | 'moderate' | 'large'
  firstHalfHr?: number
  secondHalfHr?: number
  efPerBpm?: number
  hrDriftPct?: number
  hrDriftVerdict?: 'ok' | 'high'
  earlyHr?: number
  lateHr?: number
}

export const coach = {
  weekCheck: (
    athleteId: number,
    body: { week_role?: string; cautious?: boolean; from?: string; plan?: CoachPlanUnit[] },
  ) =>
    authedFetch<{ verdicts: CoachVerdict[]; week: CoachWeek }>(
      `/api/athletes/${athleteId}/coach/week-check`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  analysis: (athleteId: number, activityId: number) =>
    authedFetch<CoachActivityAnalysis>(`/api/athletes/${athleteId}/activities/${activityId}/coach/analysis`),
  paces: (athleteId: number) =>
    authedFetch<CoachPaces>(`/api/athletes/${athleteId}/coach/paces`),
  predict: (athleteId: number, opts?: { distance?: number; time?: string }) => {
    const q = opts?.distance ? `?distance=${opts.distance}&time=${encodeURIComponent(opts.time ?? '')}` : ''
    return authedFetch<CoachPredict>(`/api/athletes/${athleteId}/coach/predict${q}`)
  },
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
  /** 当日跑步距离（热力图按运动类型配色用） */
  runMeters: number
  /** 当日骑行距离 */
  rideMeters: number
}

export type SportTotals = { sport: string; distanceM: number; count: number }

export type Dashboard = {
  year: number
  /** 有数据的年份，倒序 */
  years: number[]
  /** **全部年份**的逐日摘要（热力图自行按年分组） */
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

/** 一条 GPS 轨迹的代表点（用于判断活动落在哪个省份）。 */
export type TrackCenter = {
  id: number
  sport: string
  startTime: number
  distanceM: number
  lat: number
  lng: number
}

export const trackCenters = (athleteId: number) =>
  authedFetch<TrackCenter[]>(`/api/athletes/${athleteId}/track-centers`)

// ---------- 比赛成绩 + Banister 表现预测 ----------

export type RaceResult = {
  id?: number
  athleteId?: number
  date: string
  distanceM: number
  timeSecs: number
  name?: string
  note?: string
}

export type BanisterPoint = {
  date: string
  /** 等价参考距离（10km）下的速度 m/s */
  actual: number
  predicted: number
  paceSecsPerKm: number
}

export type BanisterForecast = {
  date: string
  predicted: number
  paceSecsPerKm: number
}

export type RaceProjection = {
  label: string
  distanceM: number
  timeSecs: number
  paceSecsPerKm: number
  /** 目标距离 / 基准距离（10km） */
  extrapolationRatio: number
  reliable: boolean
  note?: string
}

export type RiegelInfo = {
  /** 距离指数：1.06 = Riegel 教科书值；个人拟合通常 1.08–1.15 */
  alpha: number
  source: 'personal' | 'default'
  n: number
  ok: boolean
  r2?: number
}

export type BanisterFit = {
  fitted: boolean
  reason?: string
  count?: number
  p0?: number
  k1?: number
  k2?: number
  tau1?: number
  tau2?: number
  r2?: number
  rmse?: number
  n?: number
  /** n ≤ 3 时为 true：3 个参数拟合 ≤3 个点，R² 必然≈1，无统计意义 */
  overfit?: boolean
  refDistanceM?: number
  points?: BanisterPoint[]
  forecast?: BanisterForecast[]
  /** 5K / 10K / 半马 / 全马 预测 */
  projections?: RaceProjection[]
  rieger?: RiegelInfo
  maxKnownDistanceM?: number
}

export const raceResults = {
  list: (athleteId: number) =>
    authedFetch<RaceResult[]>(`/api/athletes/${athleteId}/race-results`),
  create: (athleteId: number, r: RaceResult) =>
    authedFetch<RaceResult>(`/api/athletes/${athleteId}/race-results`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(r),
    }),
  update: (athleteId: number, id: number, r: RaceResult) =>
    authedFetch<RaceResult>(`/api/athletes/${athleteId}/race-results/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(r),
    }),
  remove: (athleteId: number, id: number) =>
    authedFetch<void>(`/api/athletes/${athleteId}/race-results/${id}`, { method: 'DELETE' }),
}

export const banister = (athleteId: number, metric: 'trimp' | 'rtss' = 'trimp') =>
  authedFetch<BanisterFit>(`/api/athletes/${athleteId}/banister?metric=${metric}`)

// ---------- AI 分析提示词 ----------

export type AiPrompt = {
  range: string
  goal: string
  from: string
  to: string
  activities: number
  markdown: string
}

export const aiPrompt = (
  athleteId: number,
  params: { range: string; goal: string; activityId?: number },
) => {
  const q = new URLSearchParams({ range: params.range, goal: params.goal })
  if (params.activityId) q.set('activityId', String(params.activityId))
  return authedFetch<AiPrompt>(`/api/athletes/${athleteId}/ai-prompt?${q.toString()}`)
}

// ---------- 平台元信息（多平台凭证表单） ----------

export type PlatformField = {
  key: string
  label: string
  type: 'text' | 'password' | 'select'
  placeholder?: string
  help?: string
  options?: { value: string; label: string }[]
}

export type PlatformMeta = {
  name: string
  label: string
  help: string
  fields: PlatformField[]
  setupUrl?: string
}

export const platformMetas = () => authedFetch<PlatformMeta[]>('/api/platforms')
