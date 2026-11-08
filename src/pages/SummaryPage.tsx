import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { activities as fetchActivities, type Activity } from '../api'
import { readActiveAthlete } from '../athlete'
import { sportNames } from '../format'
import { formatDistance, formatPace, paceUnit, useUnit } from '../units'
import { useLocale } from '../i18n'
import { NOW } from '../clock'

/** 汇总页（对齐 running_page 的 SummaryPage）。 // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
 *
 * 按「年 / 月 / 周 / 日 / Life」把活动切成周期，每个周期一张卡片： // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
 * 8 项指标 + 一张按周期粒度分桶的柱状图。 // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
 * 与 running_page 的差异：柱状图用自绘 SVG（项目里 ECharts 6 在最小配置外 // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
 * 会崩，一个 12~31 根柱子的图不值得再引入图表库）。 // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
 */

type Period = 'year' | 'month' | 'week' | 'day' | 'life'

const pad2 = (n: number) => String(n).padStart(2, '0')

/** 活动所属周期键（按浏览器本地时区）。 */ // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
function keyOf(t: number, period: Period): string {
  if (period === 'life') return 'Life'
  const d = new Date(t * 1000)
  const y = d.getFullYear()
  const m = pad2(d.getMonth() + 1)
  const day = pad2(d.getDate())
  if (period === 'year') return `${y}`
  if (period === 'month') return `${y}-${m}`
  if (period === 'day') return `${y}-${m}-${day}`
  // ISO 周（跨年时归属 ISO week-year）
  const u = new Date(Date.UTC(y, d.getMonth(), d.getDate()))
  u.setUTCDate(u.getUTCDate() + 4 - (u.getUTCDay() || 7))
  const wy = u.getUTCFullYear()
  const w = Math.ceil(((u.getTime() - Date.UTC(wy, 0, 1)) / 86400000 + 1) / 7)
  return `${wy}-W${pad2(w)}`
}

type Stats = {
  count: number
  distanceM: number
  seconds: number
  paceSecsPerKm: number
  avgHr: number
  maxDistanceM: number
  fastestPaceSecsPerKm: number
  avgDistanceM: number
  elevationGainM: number
}

function summarize(list: Activity[]): Stats {
  let distanceM = 0
  let seconds = 0
  let elevation = 0
  let maxDistance = 0
  let fastest = 0
  let hrSum = 0
  let hrCount = 0
  for (const a of list) {
    // 优先用 timerSecs（不含暂停），与 running_page 的 moving_time 同义
    const secs = a.timerSecs > 0 ? a.timerSecs : a.durationSecs
    distanceM += a.distanceM
    seconds += secs
    elevation += a.elevationGainM
    if (a.distanceM > maxDistance) maxDistance = a.distanceM
    // 「最快配速」只统计跑步：骑行的 min/km 会低到 3:xx，混进来会误导。
    if (a.sport === 'run' && a.distanceM > 100 && secs > 0) {
      const pace = secs / (a.distanceM / 1000)
      if (fastest === 0 || pace < fastest) fastest = pace
    }
    if (a.avgHr > 0) {
      hrSum += a.avgHr
      hrCount++
    }
  }
  return {
    count: list.length,
    distanceM,
    seconds,
    paceSecsPerKm: distanceM > 0 ? seconds / (distanceM / 1000) : 0,
    avgHr: hrCount > 0 ? hrSum / hrCount : 0,
    maxDistanceM: maxDistance,
    fastestPaceSecsPerKm: fastest,
    avgDistanceM: list.length > 0 ? distanceM / list.length : 0,
    elevationGainM: elevation,
  }
}

/** en 月份缩写（日期桶标签用；与 toLocaleString 解耦，保证测试可断言）。 */ // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
const MONTHS_EN = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** 柱状图的一个桶：短标签给坐标轴，完整标签给 tooltip。 */ // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
type Bucket = {
  label: string
  fullLabel: string
  km: number
  count: number
  seconds: number
  runMeters: number
}

/** 按周期粒度分桶（年→12 月、月→逐日、周→周一至周日、日→逐小时、Life→按年）。 */ // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
function chartFor(list: Activity[], period: Period, year: number, zh: boolean): Bucket[] {
  type Acc = { km: number; count: number; seconds: number; runMeters: number }
  const mk = (): Acc => ({ km: 0, count: 0, seconds: 0, runMeters: 0 })
  const add = (a: Acc, act: Activity) => {
    const secs = act.timerSecs > 0 ? act.timerSecs : act.durationSecs
    a.km += act.distanceM / 1000
    a.count++
    a.seconds += secs
    if (act.sport === 'run') a.runMeters += act.distanceM
  }

  if (period === 'week') {
    // 星期/月份标签属日期形态：zh 中文、en 用最短英文缩写（不引 Intl，保持确定性）
    const names = zh
      ? ['一', '二', '三', '四', '五', '六', '日'] // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
      : ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
    const accs = names.map(mk)
    for (const a of list) {
      const d = new Date(a.startTime * 1000)
      add(accs[(d.getDay() + 6) % 7], a)
    }
    return accs.map((a, i) => ({
      label: names[i],
      fullLabel: zh ? `周${names[i]}` : names[i], // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
      km: Number(a.km.toFixed(2)),
      count: a.count,
      seconds: a.seconds,
      runMeters: a.runMeters,
    }))
  }

  const buckets = new Map<string, Bucket & Acc>()
  const ensure = (key: string, label: string, fullLabel: string) => {
    if (!buckets.has(key)) {
      buckets.set(key, { label, fullLabel, km: 0, count: 0, seconds: 0, runMeters: 0 })
    }
  }

  if (period === 'year') {
    for (let i = 1; i <= 12; i++) ensure(pad2(i), pad2(i), zh ? `${year} 年 ${i} 月` : `${MONTHS_EN[i - 1]} ${year}`) // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
    for (const a of list) {
      const d = new Date(a.startTime * 1000)
      const k = pad2(d.getMonth() + 1)
      const b = buckets.get(k)
      if (b) {
        const tmp = mk()
        add(tmp, a)
        b.km += tmp.km
        b.count += tmp.count
        b.seconds += tmp.seconds
        b.runMeters += tmp.runMeters
      }
    }
  } else if (period === 'month') {
    const first = list.length > 0 ? new Date(list[0].startTime * 1000) : new Date()
    const y = first.getFullYear()
    const m = first.getMonth() + 1
    const days = new Date(y, m, 0).getDate()
    for (let i = 1; i <= days; i++) ensure(pad2(i), pad2(i), zh ? `${m} 月 ${i} 日` : `${MONTHS_EN[m - 1]} ${i}`) // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
    for (const a of list) {
      const d = new Date(a.startTime * 1000)
      const k = pad2(d.getDate())
      const b = buckets.get(k)
      if (b) {
        const tmp = mk()
        add(tmp, a)
        b.km += tmp.km
        b.count += tmp.count
        b.seconds += tmp.seconds
        b.runMeters += tmp.runMeters
      }
    }
  } else if (period === 'day') {
    for (let i = 0; i < 24; i++) ensure(pad2(i), pad2(i), `${pad2(i)}:00–${pad2(i)}:59`)
    for (const a of list) {
      const d = new Date(a.startTime * 1000)
      const k = pad2(d.getHours())
      const b = buckets.get(k)
      if (b) {
        const tmp = mk()
        add(tmp, a)
        b.km += tmp.km
        b.count += tmp.count
        b.seconds += tmp.seconds
        b.runMeters += tmp.runMeters
      }
    }
  } else {
    // Life → 按年份
    for (const a of list) {
      const y = String(new Date(a.startTime * 1000).getFullYear())
      ensure(y, y, zh ? `${y} 年` : `${y}`) // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
      const b = buckets.get(y)!
      const tmp = mk()
      add(tmp, a)
      b.km += tmp.km
      b.count += tmp.count
      b.seconds += tmp.seconds
      b.runMeters += tmp.runMeters
    }
  }

  return [...buckets.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, b]) => ({ ...b, km: Number(b.km.toFixed(2)) }))
}

/** 自绘 SVG 柱状图 + 自定义悬浮提示（原生 title 有延迟且样式不可控）。 */ // i18n-allow: 日期桶标签（票面：数字/日期格式不动；en 分支为英文）
function MiniBars({ data }: { data: Bucket[] }) {
  const { t } = useLocale()
  const { unit } = useUnit()
  const boxRef = useRef<HTMLDivElement>(null)
  const [tip, setTip] = useState<{ x: number; y: number; b: Bucket } | null>(null)

  const W = 520
  const H = 120
  const PAD_B = 18
  const max = Math.max(1, ...data.map((d) => d.km))
  const bw = data.length > 0 ? (W - 8) / data.length : W

  return (
    <div ref={boxRef} className="relative">
      <svg viewBox={`0 0 ${W} ${H}`} className="h-28 w-full" role="img">
        {data.map((d, i) => {
          const h = (d.km / max) * (H - PAD_B - 6)
          const x = 4 + i * bw
          return (
            <g key={d.label}>
              <rect
                x={x + bw * 0.12}
                y={H - PAD_B - h}
                width={Math.max(1, bw * 0.76)}
                height={h}
                rx={1.5}
                className={`fill-accent ${d.km > 0 ? 'cursor-pointer hover:fill-secondary' : ''}`}
                onMouseEnter={() => {
                  if (d.count === 0) return
                  const r = boxRef.current?.getBoundingClientRect()
                  if (!r) return
                  // 用柱子的水平中心作为浮层锚点，避免跟随鼠标抖动
                  const cx = ((x + bw / 2) / W) * r.width
                  const cy = ((H - PAD_B - h) / H) * r.height
                  setTip({ x: cx, y: cy, b: d })
                }}
                onMouseLeave={() => setTip(null)}
              />
              {(data.length <= 13 || i % 3 === 0) && (
                <text
                  x={x + bw / 2}
                  y={H - 5}
                  textAnchor="middle"
                  className="fill-muted-foreground"
                  fontSize="8"
                >
                  {d.label}
                </text>
              )}
            </g>
          )
        })}
      </svg>

      {tip && (
        <div
          className="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded border border-border bg-card px-2 py-1.5 text-[11px] whitespace-nowrap shadow-lg"
          style={{ left: tip.x, top: Math.max(0, tip.y - 6) }}
          role="tooltip"
        >
          <div className="font-medium text-foreground">{tip.b.fullLabel}</div>
          <div className="mt-0.5 font-mono text-accent">
            {formatDistance(tip.b.km * 1000, unit)}
            <span className="text-muted-foreground">{t('summaryTimes', { n: tip.b.count })}</span>
          </div>
          <div className="font-mono text-muted-foreground">
            {Math.floor(tip.b.seconds / 3600)}h {Math.floor((tip.b.seconds % 3600) / 60)}m
            {tip.b.runMeters > 0 && (
              <span>
                {' · '}
                {formatPace(tip.b.seconds / (tip.b.km || 1), unit)}
                {paceUnit(unit)}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function SummaryCard({
  periodKey,
  list,
  period,
  onPickDay,
}: {
  periodKey: string
  list: Activity[]
  period: Period
  onPickDay: (a: Activity) => void
}) {
  const { unit } = useUnit()
  const { locale, t } = useLocale()
  const stats = summarize(list)
  const chart = chartFor(list, period, (list[0] ? new Date(list[0].startTime * 1000) : NOW).getFullYear(), locale === 'zh')

  const metrics: [string, string][] = [
    [t('summaryCount'), String(stats.count)],
    [
      t('summaryMovingTime'),
      `${Math.floor(stats.seconds / 3600)}h ${Math.floor((stats.seconds % 3600) / 60)}m`,
    ],
    [
      t('summaryAvgPace'),
      stats.paceSecsPerKm > 0 ? `${formatPace(stats.paceSecsPerKm, unit)}${paceUnit(unit)}` : '—',
    ],
    [
      t('summaryAvgHr'),
      stats.avgHr > 0 ? `${Math.round(stats.avgHr)} bpm` : '—',
    ],
    [t('summaryLongest'), formatDistance(stats.maxDistanceM, unit, 2)],
    [
      t('summaryFastest'),
      stats.fastestPaceSecsPerKm > 0
        ? `${formatPace(stats.fastestPaceSecsPerKm, unit)}${paceUnit(unit)}`
        : '—',
    ],
    [t('summaryAvgDistance'), formatDistance(stats.avgDistanceM, unit, 2)],
    [t('summaryElevation'), `${stats.elevationGainM.toFixed(0)} m`],
  ]

  return (
    <article className="rounded-xl border border-border bg-card p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <h2 className="text-base font-semibold">
          {periodKey === 'Life' ? t('summaryAllTime') : periodKey}
        </h2>
        <p className="font-mono text-xl font-semibold text-accent">
          {formatDistance(stats.distanceM, unit)}
        </p>
      </div>

      <dl className="my-4 grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
        {metrics.map(([name, value]) => (
          <div key={name}>
            <dt className="text-[11px] text-muted-foreground">{name}</dt>
            <dd className="mt-0.5 font-mono text-sm font-medium text-foreground">{value}</dd>
          </div>
        ))}
      </dl>

      <MiniBars data={chart} />
      <p className="mt-1 text-right text-[10px] text-muted-foreground">
        {period === 'life'
          ? t('summaryDimYear')
          : period === 'year'
            ? t('summaryDimMonth')
            : period === 'week'
              ? t('summaryDimWeekdayRange')
              : period === 'day'
                ? t('summaryDimStart')
                : t('summaryDimDay')}{' '}
        · {unit === 'imperial' ? 'mi' : 'km'}
      </p>

      {period === 'day' && (
        <div className="mt-3 space-y-1 border-t border-border pt-3">
          {list.map((a) => (
            <button
              key={a.id}
              onClick={() => onPickDay(a)}
              className="flex w-full cursor-pointer items-center justify-between gap-3 rounded p-1.5 text-left text-sm hover:bg-muted"
            >
              <span className="truncate">
                {new Date(a.startTime * 1000).toLocaleTimeString('zh-CN', {
                  hour: '2-digit',
                  minute: '2-digit',
                  hour12: false,
                })}{' '}
                · {sportNames[a.sport] ?? a.sport}
              </span>
              <span className="shrink-0 text-accent">
                {formatDistance(a.distanceM, unit)}
              </span>
            </button>
          ))}
        </div>
      )}
    </article>
  )
}

export default function SummaryPage() {
  const { t } = useLocale()

  const [list, setList] = useState<Activity[]>([])
  const [error, setError] = useState<string | null>(null)
  const [period, setPeriod] = useState<Period>('month')
  const [sport, setSport] = useState('all')
  const [year, setYear] = useState('all')
  const [limit, setLimit] = useState(12)

  const load = useCallback(() =>
      // setState 推迟到微任务：effect 调用栈内同步 setState 命中 set-state-in-effect
      Promise.resolve().then(async () => {
      const id = readActiveAthlete()
      if (!id) return
      try {
        setList(await fetchActivities.list(id))
      } catch (err) {
        setError(err instanceof Error ? err.message : t('commonLoadFailed'))
      }
        }), [t])

  useEffect(() => {
    void load()
  }, [load])

  const sports = useMemo(
    () => [...new Set(list.map((a) => a.sport))].sort(),
    [list],
  )
  const years = useMemo(
    () =>
      [...new Set(list.map((a) => String(new Date(a.startTime * 1000).getFullYear())))].sort().reverse(),
    [list],
  )

  const groups = useMemo(() => {
    const filtered = list.filter(
      (a) =>
        (sport === 'all' || a.sport === sport) &&
        (year === 'all' || keyOf(a.startTime, 'year') === year),
    )
    const map = new Map<string, Activity[]>()
    for (const a of filtered) {
      const k = keyOf(a.startTime, period)
      const g = map.get(k)
      if (g) g.push(a)
      else map.set(k, [a])
    }
    return [...map.entries()].sort(([a], [b]) => b.localeCompare(a))
  }, [list, sport, year, period])

  const periods: [Period, string][] = [
    ['year', t('summaryDimYear')],
    ['month', t('summaryDimMonth')],
    ['week', t('summaryDimWeek')],
    ['day', t('summaryDimDay')],
    ['life', 'Life'],
  ]

  const control = 'rounded border border-border bg-muted px-2 py-1.5 text-sm'

  return (
    <main className="mx-auto max-w-[1400px] px-4 py-6 sm:px-6">
      <div className="mb-5 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">{t('summaryTitle')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {t('summarySubtitle')}
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <label className="text-xs text-muted-foreground">
            {t('summarySport')}
            <select
              aria-label={t('summarySport')}
              className={`${control} ml-2`}
              value={sport}
              onChange={(e) => {
                setSport(e.target.value)
                setLimit(12)
              }}
            >
              <option value="all">{t('summaryAllSports')}</option>
              {sports.map((s) => (
                <option key={s} value={s}>
                  {sportNames[s] ?? s}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs text-muted-foreground">
            {t('summaryYear')}
            <select
              aria-label={t('summaryYearAria')}
              className={`${control} ml-2`}
              value={year}
              onChange={(e) => {
                setYear(e.target.value)
                setLimit(12)
              }}
            >
              <option value="all">{t('summaryAllYears')}</option>
              {years.map((y) => (
                <option key={y}>{y}</option>
              ))}
            </select>
          </label>
        </div>
      </div>

      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div
          role="group"
          aria-label={t('summaryPeriod')}
          className="flex gap-1 rounded-lg border border-border bg-card p-1"
        >
          {periods.map(([value, label]) => (
            <button
              key={value}
              onClick={() => {
                setPeriod(value)
                setLimit(12)
              }}
              aria-pressed={period === value}
              className={`cursor-pointer rounded px-3 py-1.5 text-sm ${
                period === value
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
        <span className="text-sm text-muted-foreground">
          {groups.length} {t('summaryPeriods')}
        </span>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {groups.length === 0 ? (
        <p role="status" className="rounded-xl border border-border bg-card py-16 text-center text-muted-foreground">
          {t('summaryEmpty')}
        </p>
      ) : (
        <div className={`grid min-w-0 gap-5 ${period === 'life' ? '' : 'xl:grid-cols-2'}`}>
          {groups.slice(0, limit).map(([key, items]) => (
            <SummaryCard
              key={key}
              periodKey={key}
              list={items}
              period={period}
              onPickDay={(a) => {
                window.location.href = `/activities/${a.id}`
              }}
            />
          ))}
        </div>
      )}

      {groups.length > limit && (
        <div className="mt-6 text-center">
          <button
            onClick={() => setLimit((n) => n + 12)}
            className="cursor-pointer rounded border border-border bg-card px-4 py-2 text-sm hover:text-foreground"
          >
            {t('summaryLoadMore')} ({limit}/{groups.length})
          </button>
        </div>
      )}

      <p className="mt-6 text-center text-xs text-muted-foreground">
        <Link to="/" className="text-secondary hover:underline">
          {t('summaryBack')}
        </Link>
      </p>
    </main>
  )
}
