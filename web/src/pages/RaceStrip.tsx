import type { RaceEvent } from '../api'
import { useLocale } from '../i18n'
import { raceCategoryKeys, raceState, sortByDateAsc } from '../race-ui'
import { shortName } from '../china-geo'
import RaceRegLine from './RaceRegLine'

/** 首屏「即将开赛」横滚条。
 *
 * 参考 nowrun 的「火热报名中」：首屏先回答「我现在该干什么」，再让人去逛地图。
 * 我们拿不到报名状态（上游没有这个字段），所以这条按**开赛日**排 ——
 * 30 天内开赛的排最前，剩下的用「N 天后」自己读。
 */
const WINDOW_DAYS = 90
const MAX_CARDS = 12

export default function RaceStrip({ races, today }: { races: RaceEvent[]; today: string }) {
  const { t } = useLocale()
  const soon = sortByDateAsc(
    races.filter((r) => {
      const d = raceState(r.date, today).days
      return d >= 0 && d <= WINDOW_DAYS
    }),
  ).slice(0, MAX_CARDS)
  if (soon.length === 0) return null

  return (
    <section aria-label={t('racesUpcomingTitle')} className="mb-4">
      <div className="mb-2 flex items-baseline gap-2">
        <h2 className="text-sm font-semibold text-foreground">{t('racesUpcomingTitle')}</h2>
        <span className="rounded-full bg-secondary/20 px-2 py-0.5 text-xs font-mono text-secondary">
          {soon.length}
        </span>
        <span className="text-xs text-muted-foreground">{t('racesUpcomingHint')}</span>
      </div>
      <ul className="flex snap-x gap-3 overflow-x-auto pb-2">
        {soon.map((r) => {
          const st = raceState(r.date, today)
          const cats = raceCategoryKeys(r.categories)
          return (
            <li
              key={r.raceId}
              className="flex w-[260px] shrink-0 snap-start flex-col gap-1.5 rounded-xl border border-border bg-card p-3"
            >
              <div className="flex items-start justify-between gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground">
                  {r.name}
                </span>
                {r.grade && (
                  <span className="shrink-0 rounded bg-destructive/10 px-1.5 text-[11px] font-medium text-destructive">
                    {r.grade}
                  </span>
                )}
              </div>
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <span className="font-mono text-foreground">{r.date}</span>
                <span className="text-border">|</span>
                <span className="truncate">{shortName(r.province)}</span>
              </div>
              <div className="mt-auto flex flex-col gap-1.5 pt-1">
                <div className="flex items-center justify-between gap-2">
                  <span className="flex flex-wrap gap-1">
                    {cats.map((key) => (
                      <span
                        key={key}
                        className="whitespace-nowrap rounded bg-secondary/15 px-1.5 text-[11px] text-secondary"
                      >
                        {t(key)}
                      </span>
                    ))}
                  </span>
                  <span
                    className={`shrink-0 whitespace-nowrap rounded-md px-2 py-0.5 text-[11px] font-medium ${
                      st.days <= 7
                        ? 'bg-secondary text-secondary-foreground'
                        : 'bg-muted text-muted-foreground'
                    }`}
                  >
                    {t('racesInDays', { n: st.days })}
                  </span>
                </div>
                {/* 报名那一行与时间轴行共用同一套判定，不再各写一份 */}
                <RaceRegLine race={r} />
              </div>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
