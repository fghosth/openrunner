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
  type Activity,
} from '../api'
import ChinaMap from './ChinaMap'
import ProfileCard from './ProfileCard'
import CalendarWidget from './CalendarWidget'
import CoachWeekPanel from './CoachWeekPanel'
import { readActiveAthlete } from '../App'
import { sportNames } from '../format'
import { formatDistance, formatElevation, formatPace, paceUnit, useUnit } from '../units'
import { useLocale } from '../i18n'
import { CURRENT_YEAR } from '../clock'
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

/** 运动类型配色（对齐 running_page：跑步橙、骑行蓝，四档深浅）。 */
const TYPE_PALETTES: Record<string, string[]> = {
  run: ['#fed7aa', '#fb923c', '#f97316', '#ea580c'],
  ride: ['#bfdbfe', '#60a5fa', '#3b82f6', '#2563eb'],
}

const EMPTY_CELL = 'bg-border/60'

/** 当日主导运动类型（按距离），决定用哪套配色。 */
function dominantSport(d: DaySummary | undefined): 'run' | 'ride' {
  if (!d) return 'run'
  return d.rideMeters > d.runMeters ? 'ride' : 'run'
}

/** 该日在其主导类型下的强度比（0~1）。 */
function intensity(d: DaySummary | undefined, typeMax: Record<string, number>): number {
  if (!d) return 0
  const type = dominantSport(d)
  const v = type === 'ride' ? d.rideMeters : d.runMeters
  const m = typeMax[type] || 1
  return v > 0 ? Math.min(1, v / m) : 0
}

type YearGrid = {
  year: number
  weeks: (string | null)[][]
  monthLabels: { col: number; text: string }[]
  days: number
  distanceM: number
  count: number
}

/** 生成某一年的周列网格（周一起）。 */
function buildYearGrid(year: number, byDate: Map<string, DaySummary>): YearGrid {
  const first = new Date(year, 0, 1)
  const last = new Date(year, 11, 31)
  const start = new Date(first)
  start.setDate(start.getDate() - ((start.getDay() + 6) % 7))

  const weeks: (string | null)[][] = []
  const monthLabels: { col: number; text: string }[] = []
  const cursor = new Date(start)
  let col = 0
  let days = 0
  let distanceM = 0
  let count = 0
  while (cursor <= last) {
    const week: (string | null)[] = []
    for (let i = 0; i < 7; i++) {
      const cur = new Date(cursor)
      if (cur.getFullYear() !== year) {
        week.push(null)
      } else {
        const key = `${cur.getFullYear()}-${String(cur.getMonth() + 1).padStart(2, '0')}-${String(cur.getDate()).padStart(2, '0')}`
        week.push(key)
        const d = byDate.get(key)
        if (d && d.distanceM > 0) {
          days++
          distanceM += d.distanceM
          count += d.count
        }
        if (cur.getDate() === 1) monthLabels.push({ col, text: `${cur.getMonth() + 1}月` })
      }
      cursor.setDate(cursor.getDate() + 1)
    }
    weeks.push(week)
    col++
  }
  return { year, weeks, monthLabels, days, distanceM, count }
}

function YearHeatmap({
  grid,
  byDate,
  typeMax,
  unit,
  showYearLabel,
  onPickDay,
  pickedDate,
}: {
  grid: YearGrid
  byDate: Map<string, DaySummary>
  typeMax: Record<string, number>
  unit: 'metric' | 'imperial'
  /** 「全部」模式下每年上方显示年份 + 汇总 */
  showYearLabel: boolean
  onPickDay: (date: string) => void
  pickedDate: string | null
}) {
  const weekLabels = ['一', '', '三', '', '五', '', '日']
  return (
    <div>
      {showYearLabel && (
        <div className="mb-1 flex flex-wrap items-baseline gap-2 text-xs">
          <span className="font-mono font-semibold text-accent">{grid.year}</span>
          <span className="text-muted-foreground">
            {grid.count} 次 · {formatDistance(grid.distanceM, unit)}
          </span>
        </div>
      )}
      <div className="overflow-x-auto">
        <div className="inline-block">
          <div className="mb-1 flex gap-[3px] pl-[22px] text-[10px] text-muted-foreground">
            {grid.weeks.map((_, i) => {
              const label = grid.monthLabels.find((m) => m.col === i)
              return (
                <span key={i} className="w-3 shrink-0">
                  {label ? label.text : ''}
                </span>
              )
            })}
          </div>
          <div className="flex gap-[3px]">
            <div className="mr-1 flex w-4 flex-col gap-[3px] text-[9px] leading-3 text-muted-foreground">
              {weekLabels.map((d, i) => (
                <span key={i} className="h-3">
                  {d}
                </span>
              ))}
            </div>
            {grid.weeks.map((week, wi) => (
              <div key={wi} className="flex flex-col gap-[3px]">
                {week.map((date, di) => {
                  if (!date) return <span key={di} className="h-3 w-3" />
                  const day = byDate.get(date)
                  const ratio = intensity(day, typeMax)
                  const type = dominantSport(day)
                  const level = ratio > 0 ? Math.ceil(ratio * 4) : 0
                  const color =
                    level > 0 ? TYPE_PALETTES[type][level - 1] : undefined
                  const title = day
                    ? `${date} · ${formatDistance(day.distanceM, unit)} · ${day.count} 次`
                    : `${date} · 无活动`
                  // 无活动的格子不做成按钮：否则一年 365 个空格子全是 tab 停靠点，
                  // 键盘用户要按 365 次才能穿过这张图，屏幕阅读器也会挨个念「无活动」。
                  const Cell = day ? 'button' : 'div'
                  return (
                    <Cell
                      key={di}
                      {...(day
                        ? {
                            type: 'button' as const,
                            onClick: () => onPickDay(date),
                            'aria-label': title,
                          }
                        : {})}
                      title={title}
                      className={`h-3 w-3 shrink-0 rounded-sm transition-colors ${
                        day ? 'cursor-pointer hover:ring-1 hover:ring-muted-foreground' : ''
                      } ${level === 0 ? EMPTY_CELL : ''} ${
                        pickedDate === date ? 'ring-1 ring-accent' : ''
                      }`}
                      style={color ? { backgroundColor: color } : undefined}
                    />
                  )
                })}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

function ContributionHeatmap({
  daily,
  years,
  onYearChange,
  activities,
}: {
  daily: DaySummary[]
  years: number[]
  onYearChange?: (y: number) => void
  /** 活动明细：点击某天时列出当天活动（与 running_page 的日活动面板一致） */
  activities?: Activity[]
}) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const [selected, setSelected] = useState<number | 'all'>(years[0] ?? CURRENT_YEAR)
  const [pickedDate, setPickedDate] = useState<string | null>(null)

  // 当天活动（点击格子后展示）
  const dayActs = useMemo(() => {
    if (!pickedDate || !activities) return []
    return activities
      .filter((a) => {
        const d = new Date(a.startTime * 1000)
        return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` === pickedDate
      })
      .sort((a, b) => a.startTime - b.startTime)
  }, [pickedDate, activities])

  const byDate = useMemo(() => {
    const m = new Map<string, DaySummary>()
    for (const d of daily) m.set(d.date, d)
    return m
  }, [daily])

  // 各运动类型的单日最大距离（强度分档的分母）
  const typeMax = useMemo(() => {
    const mx: Record<string, number> = { run: 1, ride: 1 }
    for (const d of daily) {
      if (d.runMeters > mx.run) mx.run = d.runMeters
      if (d.rideMeters > mx.ride) mx.ride = d.rideMeters
    }
    return mx
  }, [daily])

  // 「全部」渲染所有年份（纵向堆叠，靠页面滚动浏览），不做数量截断 ——
  // 截断会让更早的年份静默消失。running_page 是横向 10 年一窗口翻页，
  // 这里纵向排布没有宽度限制，无需分页。
  const grids = useMemo(() => {
    if (selected === 'all') {
      return years.map((y) => buildYearGrid(y, byDate))
    }
    return [buildYearGrid(selected, byDate)]
  }, [selected, years, byDate])

  const total = grids.reduce(
    (acc, g) => ({ days: acc.days + g.days, distanceM: acc.distanceM + g.distanceM }),
    { days: 0, distanceM: 0 },
  )

  return (
    <section aria-label="年度贡献热力图" className="rounded-lg border border-border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-medium text-muted-foreground">{t('trainingCalendar')}</h2>
        <span className="text-xs text-muted-foreground">
          {total.days} {t('daysWithActivity')} · {formatDistance(total.distanceM, unit)}
        </span>

        <div className="ml-auto flex max-w-full items-center gap-1 overflow-x-auto">
          <button
            onClick={() => setSelected('all')}
            aria-pressed={selected === 'all'}
            className={`cursor-pointer rounded-full px-2.5 py-0.5 text-xs ${
              selected === 'all'
                ? 'bg-accent text-accent-foreground'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {years.length > 1 ? '全部' : '全部年份'}
          </button>
          {years.map((y) => (
            <button
              key={y}
              onClick={() => {
                setSelected(y)
                onYearChange?.(y)
              }}
              aria-pressed={selected === y}
              className={`cursor-pointer rounded-full px-2.5 py-0.5 text-xs ${
                selected === y
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {y}
            </button>
          ))}
        </div>
      </div>

      <div className="space-y-4">
        {grids.map((g) => (
          <YearHeatmap
            key={g.year}
            grid={g}
            byDate={byDate}
            typeMax={typeMax}
            unit={unit}
            showYearLabel={selected === 'all'}
            onPickDay={(d) => setPickedDate((cur) => (cur === d ? null : d))}
            pickedDate={pickedDate}
          />
        ))}
      </div>

      {/* 当天活动面板（对齐 running_page 的日活动列表） */}
      {pickedDate && dayActs.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-border pt-3">
          <span className="text-xs text-muted-foreground">{pickedDate}</span>
          {dayActs.map((a) => (
            <Link
              key={a.id}
              to={`/activities/${a.id}`}
              className="rounded border border-border px-2 py-0.5 text-xs hover:bg-muted"
            >
              {new Date(a.startTime * 1000).toLocaleTimeString('zh-CN', {
                hour: '2-digit',
                minute: '2-digit',
                hour12: false,
              })}{' '}
              · {sportNames[a.sport] ?? a.sport} · {formatDistance(a.distanceM, unit)}
            </Link>
          ))}
          <button
            onClick={() => setPickedDate(null)}
            aria-label="关闭当日活动"
            className="cursor-pointer px-2 text-sm text-muted-foreground hover:text-foreground"
          >
            ×
          </button>
        </div>
      )}

      {/* 统计行：单年模式显示该年完整统计（次数/距离/时间/配速） */}
      {selected !== 'all' && (
        <div className="mt-3 flex flex-wrap items-center justify-end gap-4 border-t border-border pt-3 text-xs text-muted-foreground">
          <span className="mr-auto">{selected} 年汇总</span>
          <span className="font-mono">{grids[0].count} 次</span>
          <span className="font-mono">{formatDistance(grids[0].distanceM, unit)}</span>
          <span className="font-mono">
            {Math.floor(
              daily
                .filter((d) => d.date.startsWith(`${selected}-`))
                .reduce((s2, d) => s2 + d.durationSecs, 0) / 3600,
            )}
            h
          </span>
        </div>
      )}

      <div className="mt-3 flex flex-wrap items-center gap-3 text-[10px] text-muted-foreground">
        <span className="flex items-center gap-1">
          {t('less')}
          <span className={`h-3 w-3 rounded-sm ${EMPTY_CELL}`} />
          {TYPE_PALETTES.run.map((c, i) => (
            <span key={i} className="h-3 w-3 rounded-sm" style={{ backgroundColor: c }} />
          ))}
          {t('more')}
        </span>
        <span className="flex items-center gap-1">
          <span className="h-3 w-3 rounded-sm" style={{ backgroundColor: TYPE_PALETTES.run[2] }} />
          跑步
          <span className="h-3 w-3 rounded-sm" style={{ backgroundColor: TYPE_PALETTES.ride[2] }} />
          骑行
        </span>
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
export default function DashboardPanel({
  athlete,
  activities,
}: {
  athlete: Athlete | null
  /** 活动明细（传给月历，点击某天时列出具体活动） */
  activities?: Activity[]
}) {
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

      {/* 左右两列各自纵向排列：
          之前是两行 grid，第一行被右栏（资料卡+个人最佳）撑高，导致第二行的
          足迹地图上方空出一大片。改成两列后每张卡片紧跟上一张，不再悬空。 */}
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_320px]">
        {/* 左列：热力图 → 足迹地图 */}
        <div className="flex min-w-0 flex-col gap-4">
          <ContributionHeatmap
            daily={data.daily}
            years={data.years}
            onYearChange={setYear}
            activities={activities}
          />
          <ChinaMap centers={centers} />
        </div>

        {/* 右列：资料卡 → 个人最佳 → 月历 */}
        <div className="flex min-w-0 flex-col gap-4">
          <ProfileCard athlete={athlete} data={data} centers={centers} />
          <PersonalBestCard bests={bests} />
          <CalendarWidget daily={data.daily} activities={activities} />
        </div>
      </div>
      <CoachWeekPanel activities={activities} />

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
