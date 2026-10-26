import { useMemo } from 'react'
import type { Activity, Athlete } from '../api'
import { formatDistance, useUnit } from '../units'
import { useLocale } from '../i18n'

/** 跑量目标卡（对齐 running_page 的 StatsCards）。
 *
 * 四张卡：年度目标 / 月度目标 / 周目标 / 连续记录。
 * 每张目标卡都有「已达成 / 目标」+ 进度条 + 次数与时长 + 同期对比
 * （去年同期、上月同期、上周同期）；连续记录卡有连续天数/周数、本周 7 天圆点
 * 和史上最长纪录。
 *
 * 数据都在前端从活动列表算 —— 和 running_page 一样，它也是拿全量 activities 现算的。
 */

type Totals = { distanceM: number; count: number; seconds: number }

function sum(list: Activity[]): Totals {
  let distanceM = 0
  let seconds = 0
  for (const a of list) {
    distanceM += a.distanceM
    seconds += a.timerSecs > 0 ? a.timerSecs : a.durationSecs
  }
  return { distanceM, count: list.length, seconds }
}

const pad2 = (n: number) => String(n).padStart(2, '0')
const dateKey = (d: Date) => `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`

/** 周序号（周一为起点），用于「连续周」统计。 */
function weekKey(d: Date): string {
  const t = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  t.setDate(t.getDate() - ((t.getDay() + 6) % 7))
  return dateKey(t)
}

function GoalCard({
  title,
  icon,
  done,
  goalKm,
  totals,
  diffM,
  diffLabel,
}: {
  title: string
  icon: React.ReactNode
  done: number
  goalKm: number
  totals: Totals
  diffM: number
  diffLabel: string
}) {
  const { unit } = useUnit()
  const pct = goalKm > 0 ? Math.min(100, (done / (goalKm * 1000)) * 100) : 0
  const positive = diffM >= 0
  return (
    <div className="rounded-xl border border-border bg-card p-5 transition-colors hover:border-secondary/40 hover:bg-secondary/5">
      <p className="mb-2 flex items-center gap-1.5 text-xs tracking-wider text-muted-foreground uppercase">
        {icon}
        {title}
      </p>
      <p className="font-mono text-3xl font-bold whitespace-nowrap text-foreground">
        {formatDistance(done, unit)}
        <span className="ml-1 text-base font-normal text-muted-foreground">
          / {formatDistance(goalKm * 1000, unit, 0)}
        </span>
      </p>
      <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-border">
        <div
          className="h-full rounded-full bg-accent transition-all"
          style={{ width: `${pct}%` }}
        />
      </div>
      <div className="mt-3 flex items-center justify-between text-sm text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <span className="text-accent">⚡</span>
          {totals.count} 次
        </span>
        <span className="font-mono">
          {Math.floor(totals.seconds / 3600)}h {Math.floor((totals.seconds % 3600) / 60)}m
        </span>
      </div>
      <p className={`mt-1.5 text-xs ${positive ? 'text-emerald-500' : 'text-red-400'}`}>
        {positive ? '↗' : '↘'} {formatDistance(Math.abs(diffM), unit)} {diffLabel}
      </p>
    </div>
  )
}

/** 目标卡组。goals 全为 0 时（未设置）仍显示，进度条为空；由设置页配置。 */
export default function GoalCards({
  activities,
  athlete,
}: {
  activities: Activity[]
  athlete: Athlete | null
}) {
  const { unit } = useUnit()
  const { locale } = useLocale()
  const zh = locale === 'zh'

  const now = useMemo(() => new Date(), [])

  // 计划活动不是「已跑完的跑量」，不能计入目标完成度
  const done = useMemo(() => activities.filter((a) => !a.isPlanned), [activities])

  const calc = useMemo(() => {
    const y = now.getFullYear()
    const m = now.getMonth()
    const todayStart = new Date(y, m, now.getDate())
    const dayOfYear = Math.floor(
      (now.getTime() - new Date(y, 0, 1).getTime()) / 86400000,
    )
    // 本周起点（周一）
    const weekStart = new Date(todayStart)
    weekStart.setDate(weekStart.getDate() - ((weekStart.getDay() + 6) % 7))
    const lastWeekStart = new Date(weekStart)
    lastWeekStart.setDate(lastWeekStart.getDate() - 7)
    const lastWeekSamePoint = new Date(now.getTime() - 7 * 86400000)
    // 上月同期
    const lastMonthYear = m === 0 ? y - 1 : y
    const lastMonth = m === 0 ? 11 : m - 1

    const inRange = (a: Activity, from: Date, to: Date) => {
      const d = new Date(a.startTime * 1000)
      return d >= from && d <= to
    }

    const yearList = done.filter(
      (a) => new Date(a.startTime * 1000).getFullYear() === y,
    )
    const lastYearList = done.filter((a) => {
      const d = new Date(a.startTime * 1000)
      if (d.getFullYear() !== y - 1) return false
      const aDay = Math.floor((d.getTime() - new Date(y - 1, 0, 1).getTime()) / 86400000)
      return aDay <= dayOfYear
    })
    const monthList = done.filter((a) => {
      const d = new Date(a.startTime * 1000)
      return d.getFullYear() === y && d.getMonth() === m
    })
    const lastMonthList = done.filter((a) => {
      const d = new Date(a.startTime * 1000)
      return (
        d.getFullYear() === lastMonthYear &&
        d.getMonth() === lastMonth &&
        d.getDate() <= now.getDate()
      )
    })
    const weekList = done.filter((a) => inRange(a, weekStart, now))
    const lastWeekList = done.filter((a) =>
      inRange(a, lastWeekStart, lastWeekSamePoint),
    )

    // 连续天数
    const days = [...new Set(done.map((a) => dateKey(new Date(a.startTime * 1000))))].sort()
    let currentStreak = 0
    if (days.length > 0) {
      const today = dateKey(now)
      const yesterday = dateKey(new Date(now.getTime() - 86400000))
      const last = days[days.length - 1]
      if (last === today || last === yesterday) {
        let expected = last
        for (let i = days.length - 1; i >= 0; i--) {
          if (days[i] !== expected) break
          currentStreak++
          const prev = new Date(`${expected}T12:00:00`)
          prev.setDate(prev.getDate() - 1)
          expected = dateKey(prev)
        }
      }
    }
    // 史上最长连续天数
    let longestStreak = days.length > 0 ? 1 : 0
    let run = 1
    for (let i = 1; i < days.length; i++) {
      const prev = new Date(`${days[i - 1]}T00:00:00`)
      const cur = new Date(`${days[i]}T00:00:00`)
      if ((cur.getTime() - prev.getTime()) / 86400000 === 1) {
        run++
        if (run > longestStreak) longestStreak = run
      } else {
        run = 1
      }
    }

    // 连续周
    const weeks = [...new Set(done.map((a) => weekKey(new Date(a.startTime * 1000))))].sort()
    let currentWeekStreak = 0
    if (weeks.length > 0) {
      const thisWeek = weekKey(now)
      const lastWeek = weekKey(new Date(now.getTime() - 7 * 86400000))
      const tail = weeks[weeks.length - 1]
      if (tail === thisWeek || tail === lastWeek) {
        let expected = new Date(`${tail}T12:00:00`)
        for (let i = weeks.length - 1; i >= 0; i--) {
          if (weeks[i] !== dateKey(expected)) break
          currentWeekStreak++
          expected = new Date(expected.getTime() - 7 * 86400000)
        }
      }
    }
    let longestWeekStreak = weeks.length > 0 ? 1 : 0
    let wrun = 1
    for (let i = 1; i < weeks.length; i++) {
      const prev = new Date(`${weeks[i - 1]}T00:00:00`)
      const cur = new Date(`${weeks[i]}T00:00:00`)
      if ((cur.getTime() - prev.getTime()) / (7 * 86400000) === 1) {
        wrun++
        if (wrun > longestWeekStreak) longestWeekStreak = wrun
      } else {
        wrun = 1
      }
    }

    // 本周 7 天圆点
    const weekDays = Array.from({ length: 7 }, (_, i) => {
      const d = new Date(weekStart.getTime() + i * 86400000)
      const key = dateKey(d)
      const acts = done.filter((a) => dateKey(new Date(a.startTime * 1000)) === key)
      return { key, day: d.getDate(), has: acts.length > 0, isToday: key === dateKey(now), acts }
    })

    const yearT = sum(yearList)
    const lastYearT = sum(lastYearList)
    const monthT = sum(monthList)
    const lastMonthT = sum(lastMonthList)
    const weekT = sum(weekList)
    const lastWeekT = sum(lastWeekList)

    return {
      yearT,
      yearDiff: yearT.distanceM - lastYearT.distanceM,
      monthT,
      monthDiff: monthT.distanceM - lastMonthT.distanceM,
      weekT,
      weekDiff: weekT.distanceM - lastWeekT.distanceM,
      currentStreak,
      longestStreak,
      currentWeekStreak,
      longestWeekStreak,
      weekDays,
    }
  }, [done, now])

  const yearly = athlete?.yearlyGoalKm ?? 0
  const monthly = athlete?.monthlyGoalKm ?? 0
  const weekly = athlete?.weeklyGoalKm ?? 0

  const icon = (d: string) => (
    <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d={d} />
    </svg>
  )
  const weekLabels = zh ? ['一', '二', '三', '四', '五', '六', '日'] : ['M', 'T', 'W', 'T', 'F', 'S', 'S']

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-[1fr_1fr_1fr_1.4fr]">
      <GoalCard
        title={zh ? '年度目标' : 'Yearly goal'}
        icon={icon('M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z')}
        done={calc.yearT.distanceM}
        goalKm={yearly}
        totals={calc.yearT}
        diffM={calc.yearDiff}
        diffLabel={zh ? 'vs 去年同期' : 'vs same period last year'}
      />
      <GoalCard
        title={zh ? '月度目标' : 'Monthly goal'}
        icon={icon('M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z')}
        done={calc.monthT.distanceM}
        goalKm={monthly}
        totals={calc.monthT}
        diffM={calc.monthDiff}
        diffLabel={zh ? 'vs 上月同期' : 'vs last month'}
      />
      <GoalCard
        title={zh ? '周目标' : 'Weekly goal'}
        icon={icon('M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z')}
        done={calc.weekT.distanceM}
        goalKm={weekly}
        totals={calc.weekT}
        diffM={calc.weekDiff}
        diffLabel={zh ? 'vs 上周同期' : 'vs last week'}
      />

      {/* 连续记录 */}
      <div className="rounded-xl border border-border bg-card p-5 transition-colors hover:border-secondary/40 hover:bg-secondary/5">
        <p className="mb-2 flex items-center gap-1.5 text-xs tracking-wider text-muted-foreground uppercase">
          🔥 {zh ? '连续记录' : 'Streak'}
        </p>
        <div className="flex items-baseline gap-3">
          <p className="font-mono text-3xl font-bold text-foreground">
            {calc.currentStreak}
            <span className="ml-1 text-base font-normal text-muted-foreground">
              {zh ? '天' : 'days'}
            </span>
          </p>
          <p className="font-mono text-lg font-semibold text-accent">
            {calc.currentWeekStreak}
            <span className="ml-0.5 text-xs font-normal text-muted-foreground">
              {zh ? '周' : 'wk'}
            </span>
          </p>
        </div>

        <div className="mt-3 flex items-center gap-1.5">
          {calc.weekDays.map((wd, i) => (
            <div key={wd.key} className="flex flex-col items-center gap-0.5">
              <span className="text-[9px] text-muted-foreground">{weekLabels[i]}</span>
              <div
                className={`flex h-6 w-6 items-center justify-center rounded-full text-[10px] font-medium ${
                  wd.has
                    ? 'bg-accent text-accent-foreground'
                    : wd.isToday
                      ? 'text-foreground ring-1 ring-foreground'
                      : 'bg-border text-muted-foreground'
                }`}
                title={
                  wd.has
                    ? `${wd.key} · ${wd.acts.length} 次 · ${formatDistance(
                        wd.acts.reduce((s, a) => s + a.distanceM, 0),
                        unit,
                      )}`
                    : wd.key
                }
              >
                {wd.day}
              </div>
            </div>
          ))}
        </div>

        <div className="mt-3 flex items-center gap-2 text-sm text-muted-foreground">
          <span className="text-accent">✦</span>
          {zh ? '最长' : 'Longest'}: {calc.longestStreak} {zh ? '天' : 'd'} /{' '}
          {calc.longestWeekStreak} {zh ? '周' : 'wk'}
        </div>
      </div>
    </div>
  )
}
