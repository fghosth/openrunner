import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  dashboard as fetchDashboard,
  personalBest as fetchPersonalBest,
  trackCenters as fetchTrackCenters,
  type Dashboard,
  type DaySummary,
  type PeriodTotals,
  type PersonalBest,
  type TrackCenter,
  type Athlete,
} from '../api'
import ChinaMap from './ChinaMap'
import ProfileCard from './ProfileCard'
import CalendarWidget from './CalendarWidget'
import { readActiveAthlete } from '../App'
import { sportNames } from '../format'
import { formatDistance, formatElevation, formatPace, paceUnit, useUnit } from '../units'
import { useLocale } from '../i18n'
import { exportCardPng } from '../exportCard'

/** 首页概览三件套：统计卡片 + 年度贡献热力图 + 个人最佳。
 *
 * 对齐 running_page 的主页信息密度（它的 StatsCards / ContributionHeatmap /
 * PersonalBest）。区别在于个人最佳是在活动**内部**找最快分段，而不是挑一条
 * 总距离正好 5km 的活动取总时长。
 */

// ---------- 工具 ----------

function fmtHours(secs: number): string {
  const h = secs / 3600
  return h >= 100 ? h.toFixed(0) : h.toFixed(1)
}

function fmtClock(secs: number): string {
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  const s = Math.round(secs % 60)
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`
}

// ---------- 统计卡片 ----------

function StatTile({
  label,
  sub,
  value,
  unit,
}: {
  label: string
  sub?: string
  value: string
  unit: string
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      <div className="text-xs text-muted-foreground">
        {label}
        {sub && <span className="ml-1 opacity-70">{sub}</span>}
      </div>
      <div className="mt-1 font-mono text-2xl font-semibold text-foreground">
        {value}
        <span className="ml-1 text-sm font-normal text-muted-foreground">{unit}</span>
      </div>
    </div>
  )
}

function TotalsRow({ totals }: { totals: PeriodTotals }) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const dist = formatDistance(totals.distanceM, unit)
  const [dv, du] = dist.split(' ')
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      <StatTile label={t('distance')} value={dv} unit={du} />
      <StatTile label={t('count')} value={String(totals.count)} unit={t('times')} />
      <StatTile label={t('duration')} value={fmtHours(totals.durationSecs)} unit="h" />
      <StatTile
        label={t('elevation')}
        value={formatElevation(totals.elevationGainM, unit).split(' ')[0]}
        unit={formatElevation(totals.elevationGainM, unit).split(' ')[1]}
      />
    </div>
  )
}

// ---------- 贡献热力图 ----------

const HEAT_LEVELS = [
  'bg-muted', // 0
  'bg-secondary/25',
  'bg-secondary/45',
  'bg-secondary/70',
  'bg-secondary',
]

/** 按当日距离分档（0 / 小 / 中 / 大 / 很大）。阈值按「日常跑量」的量级定，
 *  不是为了跟别人的图比较，只是让本图自身有层次。 */
function heatLevel(meters: number): number {
  if (meters <= 0) return 0
  if (meters < 5000) return 1
  if (meters < 10000) return 2
  if (meters < 20000) return 3
  return 4
}

function ContributionHeatmap({
  year,
  daily,
  years,
  onYearChange,
}: {
  year: number
  daily: DaySummary[]
  years: number[]
  onYearChange: (y: number) => void
}) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const byDate = useMemo(() => {
    const m = new Map<string, DaySummary>()
    for (const d of daily) m.set(d.date, d)
    return m
  }, [daily])

  // 从该年 1 月 1 日所在周（周一起）到 12 月 31 日所在周，按列铺 7 天
  const { weeks, monthLabels } = useMemo(() => {
    const first = new Date(year, 0, 1)
    const last = new Date(year, 11, 31)
    // 回退到所在周的周一
    const start = new Date(first)
    const dow = (start.getDay() + 6) % 7 // 周一=0
    start.setDate(start.getDate() - dow)

    const cols: (string | null)[][] = []
    const labels: { col: number; text: string }[] = []
    const cursor = new Date(start)
    let col = 0
    while (cursor <= last) {
      const week: (string | null)[] = []
      for (let d = 0; d < 7; d++) {
        const cur = new Date(cursor)
        if (cur.getFullYear() !== year) {
          week.push(null) // 该年之外的日子留空
        } else {
          const key = `${cur.getFullYear()}-${String(cur.getMonth() + 1).padStart(2, '0')}-${String(cur.getDate()).padStart(2, '0')}`
          week.push(key)
          // 每月 1 号所在列打月份标签
          if (cur.getDate() === 1) {
            labels.push({ col, text: `${cur.getMonth() + 1}月` })
          }
        }
        cursor.setDate(cursor.getDate() + 1)
      }
      cols.push(week)
      col++
    }
    return { weeks: cols, monthLabels: labels }
  }, [year])

  const stats = useMemo(() => {
    let days = 0
    let total = 0
    for (const d of daily) {
      if (d.distanceM > 0) days++
      total += d.distanceM
    }
    return { days, total }
  }, [daily])

  return (
    <section
      aria-label="年度贡献热力图"
      className="rounded-lg border border-border bg-card p-4"
    >
      <div className="mb-3 flex flex-wrap items-baseline gap-2">
        <h2 className="text-sm font-medium text-muted-foreground">{t('trainingCalendar')}</h2>
        <span className="text-xs text-muted-foreground">
          {stats.days} {t('daysWithActivity')} · {t('total')} {formatDistance(stats.total, unit)}
        </span>
        {years.length > 1 && (
          <select
            value={year}
            onChange={(e) => onYearChange(Number(e.target.value))}
            className="ml-auto rounded border border-border bg-muted px-2 py-1 text-xs"
          >
            {years.map((y) => (
              <option key={y} value={y}>
                {y} 年
              </option>
            ))}
          </select>
        )}
      </div>

      <div className="overflow-x-auto">
        <div className="inline-block">
          <div className="mb-1 flex gap-[3px] pl-[26px] text-[10px] text-muted-foreground">
            {weeks.map((_, i) => {
              const label = monthLabels.find((m) => m.col === i)
              return (
                <span key={i} className="w-3 shrink-0">
                  {label ? label.text : ''}
                </span>
              )
            })}
          </div>
          <div className="flex gap-[3px]">
            <div className="mr-1 flex w-5 flex-col gap-[3px] pt-[1px] text-[10px] leading-3 text-muted-foreground">
              {['一', '', '三', '', '五', '', '日'].map((d, i) => (
                <span key={i} className="h-3">
                  {d}
                </span>
              ))}
            </div>
            {weeks.map((week, wi) => (
              <div key={wi} className="flex flex-col gap-[3px]">
                {week.map((date, di) => {
                  if (!date) {
                    return <span key={di} className="h-3 w-3" />
                  }
                  const day = byDate.get(date)
                  const lvl = heatLevel(day?.distanceM ?? 0)
                  const title = day
                    ? `${date} · ${formatDistance(day.distanceM, unit)} · ${day.count} ${t('times')}`
                    : `${date} · ${t('noActivity')}`
                  return (
                    <span
                      key={di}
                      title={title}
                      className={`h-3 w-3 rounded-sm ${HEAT_LEVELS[lvl]} ${
                        lvl > 0 ? 'hover:ring-1 hover:ring-ring' : ''
                      }`}
                    />
                  )
                })}
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-2 flex items-center gap-1 text-[10px] text-muted-foreground">
        <span className="mr-1">{t('less')}</span>
        {HEAT_LEVELS.map((c, i) => (
          <span key={i} className={`h-3 w-3 rounded-sm ${c}`} />
        ))}
        <span className="ml-1">{t('more')}</span>
      </div>
    </section>
  )
}

// ---------- 个人最佳 ----------

function PersonalBestCard({ bests }: { bests: PersonalBest[] | null }) {
  const { unit } = useUnit()
  const { t } = useLocale()
  return (
    <section aria-label="个人最佳" className="rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">{t('personalBest')}</h2>
      {!bests && <p className="text-xs text-muted-foreground">{t('computing')}</p>}
      {bests && bests.length === 0 && (
        <p className="text-xs text-muted-foreground">还没有带速度数据的跑步活动</p>
      )}
      {bests && bests.length > 0 && (
        <ul className="space-y-2">
          {bests.map((b) => (
            <li key={b.label} className="flex items-baseline gap-2 text-sm">
              <span className="w-10 shrink-0 text-muted-foreground">{b.label}</span>
              <span className="font-mono font-semibold text-foreground">
                {fmtClock(b.seconds)}
              </span>
              <span className="text-xs text-muted-foreground">
                {formatPace(b.paceSecsPerKm, unit)}
                {paceUnit(unit)}
              </span>
              <Link
                to={`/activities/${b.activityId}`}
                className="ml-auto shrink-0 text-xs text-secondary hover:underline"
              >
                {new Date(b.activityStart * 1000).toLocaleDateString('zh-CN', {
                  month: '2-digit',
                  day: '2-digit',
                })}
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-3 text-[10px] leading-relaxed text-muted-foreground">{t('pbNote')}</p>
    </section>
  )
}

// ---------- 组合 ----------

/** 首页概览区：统计卡片 + 热力图 + 个人最佳。数据独立加载，失败不阻塞活动列表。 */
export default function DashboardPanel({ athlete }: { athlete: Athlete | null }) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const [data, setData] = useState<Dashboard | null>(null)
  const [bests, setBests] = useState<PersonalBest[] | null>(null)
  const [centers, setCenters] = useState<TrackCenter[] | null>(null)
  const [year, setYear] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    fetchDashboard(id, year ?? undefined)
      .then((d) => {
        if (cancelled) return
        setData(d)
        if (year === null) setYear(d.year)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [year])

  // 个人最佳要扫全部速度流，比较慢，单独加载且只加载一次
  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    fetchPersonalBest(id)
      .then((b) => {
        if (!cancelled) setBests(b)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  // 轨迹代表点：用于省份归属（中国地图）
  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    fetchTrackCenters(id)
      .then((c) => {
        if (!cancelled) setCenters(c)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return (
      <p role="alert" className="mb-6 rounded border border-destructive bg-card p-3 text-sm text-destructive">
        概览加载失败：{error}
      </p>
    )
  }
  if (!data) {
    return (
      <p role="status" className="mb-6 rounded border border-border bg-card p-3 text-sm text-muted-foreground">
        概览加载中…
      </p>
    )
  }

  return (
    <div className="mb-6 space-y-4">
      <div className="flex flex-wrap items-baseline gap-3">
        <h2 className="text-lg font-semibold">
          {data.year} {t('overviewTitle')}
        </h2>
        <span className="text-xs text-muted-foreground">
          {t('cumulative')} {formatDistance(data.totals.all.distanceM, unit)} ·{' '}
          {data.totals.all.count} {t('times')}
        </span>
        <button
          onClick={async () => {
            try {
              await exportCardPng({
                title: `${data.year} 年度总结`,
                athleteName: athlete?.name ?? 'openrunner',
                year: data.year,
                distanceLabel: formatDistance(data.totals.year.distanceM, unit),
                countLabel: String(data.totals.year.count),
                durationLabel: `${(data.totals.year.durationSecs / 3600).toFixed(0)} h`,
                elevationLabel: formatElevation(data.totals.year.elevationGainM, unit),
                daily: new Map(data.daily.map((d) => [d.date, d.distanceM])),
              })
            } catch {
              // 导出失败通常是浏览器限制，静默即可（不影响主流程）
            }
          }}
          className="ml-auto cursor-pointer rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
          title="导出 1080×1080 分享卡片（PNG）"
        >
          导出分享图
        </button>
        {data.bySport.length > 1 && (
          <span className="text-xs text-muted-foreground">
            {data.bySport
              .map((s) => `${sportNames[s.sport] ?? s.sport} ${formatDistance(s.distanceM, unit)}`)
              .join(' · ')}
          </span>
        )}
      </div>

      <TotalsRow totals={data.totals.year} />

      <div className="grid gap-4 lg:grid-cols-[1fr_300px]">
        <ContributionHeatmap
          year={year ?? data.year}
          daily={data.daily}
          years={data.years}
          onYearChange={setYear}
        />
        <div className="flex flex-col gap-4">
          <ProfileCard athlete={athlete} data={data} centers={centers} />
          <PersonalBestCard bests={bests} />
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
        <ChinaMap centers={centers} />
        <CalendarWidget daily={data.daily} />
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        <div>
          <h3 className="mb-1 text-xs text-muted-foreground">{t('thisMonth')}</h3>
          <TotalsRow totals={data.totals.month} />
        </div>
        <div>
          <h3 className="mb-1 text-xs text-muted-foreground">{t('thisWeek')}</h3>
          <TotalsRow totals={data.totals.week} />
        </div>
      </div>
    </div>
  )
}
