import type { RaceEvent } from '../api'
import { useLocale } from '../i18n'
import { dayNum, monthLabel, raceCategoryKeys, raceState } from '../race-ui'

/** 时间轴上的一场比赛：左侧渐变大日期块 + 右侧信息卡。
 *
 * 月份视图与月历视图共用，所以日期块只在月份视图里画连接线（rail 由父级给）。
 * 已结束的整行降透明度 —— 赛历是拿来找下一场的，过去的该退到背景里。
 */
export default function RaceRow({
  race,
  today,
  showDateBlock = true,
}: {
  race: RaceEvent
  today: string
  showDateBlock?: boolean
}) {
  const { t, locale } = useLocale()
  const st = raceState(race.date, today)
  const past = st.key === 'racesPast'
  const cats = raceCategoryKeys(race.categories)

  return (
    <li className={`relative flex items-center gap-3 py-1.5 ${past ? 'opacity-55' : ''}`}>
      {showDateBlock && (
        <span className="relative z-10 flex h-14 w-14 shrink-0 flex-col items-center justify-center rounded-lg bg-gradient-to-b from-secondary to-primary text-primary-foreground shadow-sm">
          <span className="text-lg leading-none font-bold">{dayNum(race.date)}</span>
          <span className="mt-0.5 text-[10px] leading-none opacity-90">
            {monthLabel(race.date, locale)}
          </span>
        </span>
      )}
      <div className="min-w-0 flex-1 rounded-lg border border-border bg-card px-3 py-2">
        <div className="flex items-baseline gap-2">
          <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">
            {race.name}
          </span>
          {race.grade && (
            <span className="shrink-0 rounded bg-destructive/10 px-1.5 py-0.5 text-[11px] font-medium text-destructive">
              {race.grade}
            </span>
          )}
          {cats.map((key) => (
            <span
              key={key}
              className="shrink-0 rounded bg-secondary/15 px-1.5 py-0.5 text-[11px] text-secondary"
            >
              {t(key)}
            </span>
          ))}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span
            className={`rounded px-1.5 py-0.5 font-medium ${
              st.key === 'racesSoon'
                ? 'bg-secondary text-secondary-foreground'
                : past
                  ? 'bg-muted text-muted-foreground'
                  : 'bg-secondary/15 text-secondary'
            }`}
          >
            {t(st.key)}
            {!past && ` · ${t('racesInDays', { n: st.days })}`}
          </span>
          <span className="font-mono">{race.date}</span>
          {(race.city || race.district) && (
            <span className="truncate">
              {[race.city, race.district].filter(Boolean).join(' · ')}
            </span>
          )}
        </div>
      </div>
    </li>
  )
}
