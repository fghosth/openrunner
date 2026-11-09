import type { Athlete, Dashboard, TrackCenter } from '../api'
import { formatDistance, formatElevation, useUnit } from '../units'
import { useLocale } from '../i18n'

/** 个人资料卡（对齐 running_page 的 ProfileCard）。
 *
 * 展示：头像（或首字母兜底）、名字、简介、累计里程/次数/时长、足迹覆盖。
 * 数据都从 dashboard 聚合里取，不额外请求。
 */
export default function ProfileCard({
  athlete,
  data,
  centers,
}: {
  athlete: Athlete | null
  data: Dashboard
  centers: TrackCenter[] | null
}) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const name = athlete?.name || '未命名'
  const initial = name.trim().slice(0, 1).toUpperCase()
  const all = data.totals.all

  // 活跃天数：取 dashboard 的逐日数据（选定年份）；跨年累计用 all 的次数近似不够准，
  // 这里只报「本年内有活动的天数」，措辞上也这么写。
  const activeDays = data.daily.filter((d) => d.distanceM > 0).length

  return (
    <section aria-label="个人资料卡" className="rounded-lg border border-border bg-card p-4">
      <div className="flex items-center gap-3">
        <div className="h-14 w-14 shrink-0 overflow-hidden rounded-full border-2 border-border bg-muted">
          {athlete?.avatarUrl ? (
            <img
              src={athlete.avatarUrl}
              alt={name}
              className="h-full w-full object-cover"
              // 头像地址失效时退回首字母，不显示破图
              onError={(e) => {
                e.currentTarget.style.display = 'none'
              }}
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center text-xl font-semibold text-secondary">
              {initial}
            </div>
          )}
        </div>
        <div className="min-w-0">
          <div className="truncate text-base font-semibold text-foreground">{name}</div>
          {athlete?.bio ? (
            <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{athlete.bio}</p>
          ) : (
            <p className="mt-0.5 text-xs text-muted-foreground">
              {data.years.length > 1
                ? `从 ${data.years[data.years.length - 1]} 年跑到现在`
                : `${data.year} 年开始记录`}
            </p>
          )}
        </div>
      </div>

      <div className="mt-4 grid grid-cols-2 gap-2 text-sm">
        <div>
          <div className="text-xs text-muted-foreground">{t('totalDistance')}</div>
          <div className="font-mono text-lg font-semibold text-foreground">
            {formatDistance(all.distanceM, unit)}
          </div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground">{t('totalCount')}</div>
          <div className="font-mono text-lg font-semibold text-foreground">{all.count}</div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground">{t('totalTime')}</div>
          <div className="font-mono text-lg font-semibold text-foreground">
            {(all.durationSecs / 3600).toFixed(0)}
            <span className="ml-1 text-xs font-normal text-muted-foreground">h</span>
          </div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground">{data.year} {t('activeThisYear')}</div>
          <div className="font-mono text-lg font-semibold text-foreground">
            {activeDays}
            <span className="ml-1 text-xs font-normal text-muted-foreground">
              {t('daysWithActivity')}
            </span>
          </div>
        </div>
      </div>

      {centers && centers.length > 0 && (
        <p className="mt-3 text-xs text-muted-foreground">
          {centers.length} {t('gpsTracks')} · {t('totalElevation')}{' '}
          {formatElevation(all.elevationGainM, unit)}
        </p>
      )}
    </section>
  )
}
