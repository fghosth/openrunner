import { useMemo, useState } from 'react'
import type { DaySummary } from '../api'

/** 月历组件（对齐 running_page 的 CalendarWidget）。
 *
 * 一个月一张网格，有活动的日子按距离深浅着色并显示公里数，可翻月。
 * 数据来自 dashboard 的逐日摘要，不需要额外请求。
 */

const WEEKDAY_LABELS = ['一', '二', '三', '四', '五', '六', '日']

function daysInMonth(year: number, month: number): number {
  return new Date(year, month, 0).getDate() // month 1-based
}

/** 该月 1 号是周几（周一=0）。 */
function firstWeekdayOffset(year: number, month: number): number {
  return (new Date(year, month - 1, 1).getDay() + 6) % 7
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

const CELL_LEVELS = [
  '',
  'bg-secondary/20',
  'bg-secondary/40',
  'bg-secondary/65',
  'bg-secondary/90 text-background',
]

function levelOf(meters: number): number {
  if (meters <= 0) return 0
  if (meters < 5000) return 1
  if (meters < 10000) return 2
  if (meters < 20000) return 3
  return 4
}

export default function CalendarWidget({ daily }: { daily: DaySummary[] }) {
  const byDate = useMemo(() => {
    const m = new Map<string, DaySummary>()
    for (const d of daily) m.set(d.date, d)
    return m
  }, [daily])

  // 默认显示「最近一次有活动的月份」
  const [ym, setYm] = useState(() => {
    const latest = daily.reduce<DaySummary | null>(
      (acc, d) => (!acc || d.date > acc.date ? d : acc),
      null,
    )
    if (latest) {
      const [y, m] = latest.date.split('-').map(Number)
      return { year: y, month: m }
    }
    const now = new Date()
    return { year: now.getFullYear(), month: now.getMonth() + 1 }
  })

  const [picked, setPicked] = useState<string | null>(null)

  const total = daysInMonth(ym.year, ym.month)
  const offset = firstWeekdayOffset(ym.year, ym.month)
  const cells: (number | null)[] = [
    ...Array.from({ length: offset }, () => null),
    ...Array.from({ length: total }, (_, i) => i + 1),
  ]
  // 补齐尾部，凑成整周
  while (cells.length % 7 !== 0) cells.push(null)

  let monthDistance = 0
  let monthDays = 0
  for (let d = 1; d <= total; d++) {
    const key = `${ym.year}-${pad2(ym.month)}-${pad2(d)}`
    const v = byDate.get(key)
    if (v && v.distanceM > 0) {
      monthDistance += v.distanceM
      monthDays++
    }
  }

  const shift = (delta: number) => {
    setPicked(null)
    setYm(({ year, month }) => {
      const m = month + delta
      if (m < 1) return { year: year - 1, month: 12 }
      if (m > 12) return { year: year + 1, month: 1 }
      return { year, month: m }
    })
  }

  const pickedDay = picked ? byDate.get(picked) : null

  return (
    <section aria-label="月历" className="rounded-lg border border-border bg-card p-4">
      <div className="mb-3 flex items-baseline gap-2">
        <h2 className="text-sm font-medium text-muted-foreground">月历</h2>
        <button
          onClick={() => shift(-1)}
          className="ml-auto cursor-pointer rounded border border-border px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground"
          aria-label="上个月"
        >
          ←
        </button>
        <span className="font-mono text-xs text-foreground">
          {ym.year}-{pad2(ym.month)}
        </span>
        <button
          onClick={() => shift(1)}
          className="cursor-pointer rounded border border-border px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground"
          aria-label="下个月"
        >
          →
        </button>
      </div>

      <div className="grid grid-cols-7 gap-1 text-center text-[10px] text-muted-foreground">
        {WEEKDAY_LABELS.map((w) => (
          <span key={w}>{w}</span>
        ))}
      </div>

      <div className="mt-1 grid grid-cols-7 gap-1">
        {cells.map((day, i) => {
          if (day === null) return <span key={i} />
          const key = `${ym.year}-${pad2(ym.month)}-${pad2(day)}`
          const stat = byDate.get(key)
          const lvl = levelOf(stat?.distanceM ?? 0)
          const isPicked = picked === key
          return (
            <button
              key={i}
              onClick={() => setPicked(isPicked ? null : key)}
              title={
                stat
                  ? `${key} · ${(stat.distanceM / 1000).toFixed(1)} km · ${stat.count} 次`
                  : `${key} · 无活动`
              }
              className={`flex aspect-square cursor-pointer flex-col items-center justify-center rounded text-[11px] leading-none transition-colors hover:ring-1 hover:ring-ring ${CELL_LEVELS[lvl]} ${
                isPicked ? 'ring-1 ring-secondary' : ''
              } ${lvl === 0 ? 'text-muted-foreground' : 'text-foreground'}`}
            >
              <span>{day}</span>
              {stat && stat.distanceM > 0 && (
                <span className="mt-0.5 text-[9px] opacity-80">
                  {(stat.distanceM / 1000).toFixed(0)}
                </span>
              )}
            </button>
          )
        })}
      </div>

      <div className="mt-3 text-xs text-muted-foreground">
        {monthDays > 0 ? (
          <>
            本月 <span className="font-mono text-foreground">{monthDays}</span> 天有活动 ·{' '}
            <span className="font-mono text-foreground">
              {(monthDistance / 1000).toFixed(1)}
            </span>{' '}
            km
          </>
        ) : (
          '本月没有活动'
        )}
      </div>

      {pickedDay && pickedDay.distanceM > 0 && (
        <div className="mt-2 rounded border border-border bg-muted/50 p-2 text-xs">
          <div className="font-mono text-foreground">{pickedDay.date}</div>
          <div className="text-muted-foreground">
            {(pickedDay.distanceM / 1000).toFixed(2)} km · {pickedDay.count} 次 ·{' '}
            {Math.round(pickedDay.durationSecs / 60)} 分钟
          </div>
        </div>
      )}
    </section>
  )
}
