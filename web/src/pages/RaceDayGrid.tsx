import { useState } from 'react'
import type { RaceEvent } from '../api'
import { useLocale } from '../i18n'
import { dayNum, defaultMonth, monthKey, weekdayLabels, yearMonthLabel } from '../race-ui'
import RaceRow from './RaceRow'

/** 赛历月视图：一格一天，格子里挂当天场次数。
 *
 * 参考 nowrun 的「赛历」tab —— 地图回答「哪儿有比赛」，月历回答「我这周有空能跑哪儿」，
 * 两者问的不是一个问题，所以做成并列视图而不是塞进同一张图。
 *
 * 周一开头与首页跑量月历（CalendarWidget）保持一致，同一站两种日历不该长得不一样。
 */

function shiftMonth(m: string, delta: number): string {
  const y = Number(m.slice(0, 4))
  const mo = Number(m.slice(5, 7)) + delta
  const ny = y + Math.floor((mo - 1) / 12)
  const nyMo = ((mo - 1 + 12) % 12) + 1
  return `${ny}-${String(nyMo).padStart(2, '0')}`
}

function daysInMonth(m: string): number {
  return new Date(Number(m.slice(0, 4)), Number(m.slice(5, 7)), 0).getDate()
}

/** 该月 1 号是周几（周一=0）。 */
function firstOffset(m: string): number {
  return (new Date(`${m}-01T00:00:00`).getDay() + 6) % 7
}

export default function RaceDayGrid({ races, today }: { races: RaceEvent[]; today: string }) {
  const { t, locale } = useLocale()
  // null = 还没人工翻过月，跟着「最近一场未开始的比赛」走；数据晚到也能自愈，
  // 所以不用 effect 去同步（effect 里 setState 是这仓库的 lint 红线）。
  const [pickedMonth, setPickedMonth] = useState<string | null>(null)
  const [pickedDay, setPickedDay] = useState<string | null>(null)

  const month = pickedMonth ?? defaultMonth(races, today)
  const total = races.filter((r) => monthKey(r.date) === month).length
  const selDay = pickedDay && pickedDay.slice(0, 7) === month ? pickedDay : null

  const byDay = new Map<string, number>()
  for (const r of races) {
    if (monthKey(r.date) !== month) continue
    byDay.set(r.date, (byDay.get(r.date) ?? 0) + 1)
  }
  const dayRaces = selDay ? races.filter((r) => r.date === selDay) : []

  const dim = daysInMonth(month)
  const lead = firstOffset(month)
  const cells: (string | null)[] = []
  for (let i = 0; i < lead; i++) cells.push(null)
  for (let d = 1; d <= dim; d++) cells.push(`${month}-${String(d).padStart(2, '0')}`)
  while (cells.length % 7 !== 0) cells.push(null)
  const weekdays = weekdayLabels(locale)

  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="mb-3 flex items-center justify-center gap-3">
        <NavBtn label={t('racesPrevMonth')} onClick={() => setPickedMonth(shiftMonth(month, -1))}>
          <path d="m15 18-6-6 6-6" />
        </NavBtn>
        <span className="min-w-[150px] text-center text-lg font-semibold text-foreground">
          {yearMonthLabel(month, locale)}
        </span>
        <span className="rounded-full bg-secondary/20 px-2.5 py-0.5 font-mono text-xs text-secondary">
          {t('racesDayCount', { n: total })}
        </span>
        <NavBtn label={t('racesNextMonth')} onClick={() => setPickedMonth(shiftMonth(month, 1))}>
          <path d="m9 18 6-6-6-6" />
        </NavBtn>
      </div>

      <div className="grid grid-cols-7 gap-1 text-center text-xs text-muted-foreground">
        {weekdays.map((w, i) => (
          <span key={i}>{w}</span>
        ))}
      </div>
      <div className="mt-1 grid grid-cols-7 gap-1">
        {cells.map((date, i) => {
          if (!date) return <span key={`b${i}`} />
          const n = byDay.get(date) ?? 0
          const isToday = date === today
          const isSel = date === selDay
          return (
            <button
              key={date}
              type="button"
              disabled={n === 0}
              onClick={() => setPickedDay(date)}
              aria-label={`${date}${n ? ` · ${n}` : ''}`}
              className={`flex min-h-[62px] cursor-pointer flex-col items-center gap-1 rounded-md border px-1 py-1.5 text-sm transition-colors ${
                isSel
                  ? 'border-secondary bg-secondary/15'
                  : n > 0
                    ? 'border-border bg-muted/40 hover:border-secondary'
                    : 'border-transparent hover:bg-muted/30'
              } ${n === 0 ? 'cursor-default text-muted-foreground' : 'text-foreground'}`}
            >
              <span className={isToday ? 'font-bold text-secondary' : undefined}>{dayNum(date)}</span>
              {n > 0 && (
                <span className="rounded bg-secondary px-1.5 text-[11px] font-medium text-secondary-foreground">
                  {n}
                </span>
              )}
            </button>
          )
        })}
      </div>

      <div className="mt-3 border-t border-border pt-3">
        {selDay ? (
          <ul>
            {dayRaces.map((r) => (
              <RaceRow key={r.raceId} race={r} today={today} showDateBlock={false} />
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">{t('racesPickDay')}</p>
        )}
      </div>
    </div>
  )
}

function NavBtn({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className="flex h-8 w-8 cursor-pointer items-center justify-center rounded-full border border-border bg-card text-muted-foreground hover:border-secondary hover:text-foreground"
    >
      <svg
        aria-hidden
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        {children}
      </svg>
    </button>
  )
}
