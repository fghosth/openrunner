import type { RaceEvent } from '../api'
import { useLocale } from '../i18n'
import { monthKey, monthLabel } from '../race-ui'
import RaceRow from './RaceRow'

/** 月份手风琴 + 时间轴列表。
 *
 * 分组、展开状态都由父级给：展开/收起全部要在视图工具栏上，
 * 状态放这里就得再往上冒一层，不如统一由页面持有。
 */
export default function RaceMonthList({
  months,
  open,
  races,
  today,
  onToggle,
}: {
  /** 升序的 YYYY-MM 列表 */
  months: string[]
  open: Set<string>
  races: RaceEvent[]
  today: string
  onToggle: (m: string) => void
}) {
  const { t, locale } = useLocale()
  if (months.length === 0) {
    return <p className="py-6 text-sm text-muted-foreground">{t('racesEmpty')}</p>
  }

  return (
    <div className="flex flex-col gap-2">
      {months.map((m) => {
        const rows = races.filter((r) => monthKey(r.date) === m)
        const isOpen = open.has(m)
        return (
          <div key={m} className="rounded-lg border border-border bg-muted/20">
            <button
              type="button"
              onClick={() => onToggle(m)}
              aria-expanded={isOpen}
              className="flex w-full cursor-pointer items-center gap-3 rounded-lg px-4 py-3 text-left hover:bg-muted/50"
            >
              <span className="h-5 w-1 shrink-0 rounded-full bg-secondary" />
              <span className="text-base font-medium text-foreground">
                {t('racesMonthLabel', { m: monthLabel(`${m}-01`, locale, 'long') })}
              </span>
              <span className="ml-auto rounded-full bg-secondary/20 px-2.5 py-0.5 font-mono text-xs text-secondary">
                {rows.length}
              </span>
              <Chevron open={isOpen} />
            </button>
            {isOpen && (
              <div className="relative px-4 pb-3">
                {/* 时间轴竖线：串起这一月的日期块 */}
                {/* 竖线要落在日期块的水平中心：容器 px-4(16) + 块宽 w-14(56)/2 = 44 */}
                <span aria-hidden className="absolute top-4 bottom-5 left-[43px] w-[2px] rounded bg-secondary/30" />
                <ul>
                  {rows.map((r) => (
                    <RaceRow key={r.raceId} race={r} today={today} />
                  ))}
                </ul>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

function Chevron({ open }: { open: boolean }) {
  return (
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
      className={`shrink-0 text-muted-foreground transition-transform ${open ? 'rotate-180' : ''}`}
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  )
}
