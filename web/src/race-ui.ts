import type { RaceEvent } from './api'
import type { TKey } from './i18n'

/** 赛历三个视图（横滚条 / 月份时间轴 / 月历）共用的口径。
 *
 * 单独成模块而不是留在页面文件里，是因为 RaceStrip / RaceMonthList / RaceDayGrid
 * 都要用同一套「相对今天的位置」判定 —— 三处各写一遍的话，「即将开始」的
 * 30 天阈值迟早会在某个视图里飘掉。
 */

/** 本地日期的 YYYY-MM-DD。别用 toISOString() —— 那是 UTC，
 *  东八区傍晚之后会把「今天」算成昨天，当天比赛会被标成已结束。 */
export function isoToday(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** 比赛相对今天的位置。ISO 日期字典序即时间序，直接比。 */
export function raceState(date: string, today: string): { key: TKey; days: number } {
  const days = Math.round((Date.parse(`${date}T00:00:00`) - Date.parse(`${today}T00:00:00`)) / 86400000)
  if (days < 0) return { key: 'racesPast', days }
  if (days <= 30) return { key: 'racesSoon', days }
  return { key: 'racesUpcoming', days }
}

/** 状态筛选（月份 / 赛历视图）。空串 = 不限。 */
export const STATUS_OPTIONS: { key: string; labelKey: TKey }[] = [
  { key: '', labelKey: 'racesStatusAll' },
  { key: 'soon', labelKey: 'racesSoon' },
  { key: 'upcoming', labelKey: 'racesUpcoming' },
  { key: 'past', labelKey: 'racesPast' },
]

export function matchStatus(stateKey: TKey, picked: string): boolean {
  if (!picked) return true
  if (picked === 'soon') return stateKey === 'racesSoon'
  if (picked === 'upcoming') return stateKey === 'racesUpcoming'
  return stateKey === 'racesPast'
}

export const CATEGORY_BADGE: Record<string, TKey> = {
  full: 'racesCatFull',
  half: 'racesCatHalf',
  '10k': 'racesCatTen',
}

/** YYYY-MM —— 月份手风琴与月历的分组键。 */
export function monthKey(date: string): string {
  return date.slice(0, 7)
}

export function monthNum(date: string): number {
  return Number(date.slice(5, 7))
}

export function dayNum(date: string): number {
  return Number(date.slice(8, 10))
}

/** 按日期升序（赛历是拿来顺着往下读的，不是倒着考古）。 */
export function sortByDateAsc(list: RaceEvent[]): RaceEvent[] {
  return [...list].sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : a.raceId < b.raceId ? -1 : 1))
}

/** 按 YYYY-MM 分组，键升序。 */
export function groupByMonth(list: RaceEvent[]): Map<string, RaceEvent[]> {
  const out = new Map<string, RaceEvent[]>()
  for (const r of sortByDateAsc(list)) {
    const k = monthKey(r.date)
    const arr = out.get(k)
    if (arr) arr.push(r)
    else out.set(k, [r])
  }
  return out
}

/** 默认展开/停在哪个月：最近一场还没开始的比赛所在月，否则今天所在月。 */
export function defaultMonth(list: RaceEvent[], today: string): string {
  const next = sortByDateAsc(list).find((r) => r.date >= today)
  return (next ? monthKey(next.date) : monthKey(today))
}

/** 日期格式走 Intl，不进 i18n 字典（仓库约定：数字/日期格式不归 i18n 管）。 */
export function monthLabel(date: string, locale: string, style: 'short' | 'long' = 'short'): string {
  return new Date(`${date}T00:00:00`).toLocaleDateString(locale, { month: style })
}

export function yearMonthLabel(month: string, locale: string): string {
  return new Date(`${month}-01T00:00:00`).toLocaleDateString(locale, { year: 'numeric', month: 'long' })
}

/** 周一开头的七天短名。2024-01-01 是周一，拿它当基准就不用算偏移。 */
export function weekdayLabels(locale: string): string[] {
  return Array.from({ length: 7 }, (_, i) =>
    new Date(2024, 0, 1 + i).toLocaleDateString(locale, { weekday: 'short' }),
  )
}

/** 报名时间的显示串：'2026-11-18T12:00:00' → '11-18 12:00'。
 *  小标签里年份是噪音 —— 比赛日期已经带了年份，报名截止不会跨年差一年。 */
export function fmtRegWhen(iso: string): string {
  return iso.length >= 16 ? `${iso.slice(5, 10)} ${iso.slice(11, 16)}` : iso
}

/** 起价显示：整数不带小数（¥170 而不是 ¥170.00）。 */
/** 起价显示。
 *
 * 不能无脑 toFixed(0)：那会把 169.5 显示成 ¥170 —— 价格是给用户做决策用的数字，
 * 四舍五入出一个上游没有的价码就是造假。整数不带小数，非整数保留一位。 */
export function fmtPrice(n: number): string {
  return Number.isInteger(n) ? String(n) : n.toFixed(1)
}

/** 项目的显示标签键列表（横滚条与时间轴行都要，之前各写一份 split+filter）。 */
export function raceCategoryKeys(categories: string): TKey[] {
  return categories.split(',').filter((c) => CATEGORY_BADGE[c]).map((c) => CATEGORY_BADGE[c])
}

/** 北京时间的当前时刻 YYYY-MM-DDTHH:MM:SS。
 *
 * 报名窗口是从中国站点抓来的**北京墙上时钟**字符串（无时区偏移），
 * 拿浏览器本地时钟去比，非 Asia/Shanghai 的客户端会整批误判「已截止」/「未截止」。
 * isoToday 比的是「用户眼里的今天」，那个留本地时区是对的；这条比的是对方的截止时刻，
 * 必须钉死同一个时区。 */
export function nowInBeijing(): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(new Date())
  const g = (t: string) => parts.find((p) => p.type === t)?.value ?? '00'
  return `${g('year')}-${g('month')}-${g('day')}T${g('hour')}:${g('minute')}:${g('second')}`
}

/** 报名窗口是否已经关掉。
 *
 * 最酷的目录里大量比赛的报名**几个月前就截止了**（实测上海马拉松比赛 12-06、
 * 截止 05-29）。照原样显示「报名截止 05-29」会被读成还能报，
 * 所以过期的必须换成「报名已截止」，而不是继续显示一个过去的日期。
 * 两边都必须是北京墙上时钟（见 nowInBeijing）。 */
export function regClosed(regEnd: string, now: string): boolean {
  return !!regEnd && regEnd < now
}

/** 报名还没开放（列表页给的是「报名开始」而非「报名截止」）。 */
export function regNotOpenYet(reg: { regStart: string; regEnd: string }, now: string): boolean {
  return !reg.regEnd && !!reg.regStart && reg.regStart > now
}
