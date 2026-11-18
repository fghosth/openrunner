import { useCallback, useEffect, useMemo, useState } from 'react'
import { races as racesApi, type RaceCityCount, type RaceEvent, type RaceSummary } from '../api'
import { loadProvinces, shortName, type GeoFeature } from '../china-geo'
import { useLocale, type TKey } from '../i18n'
import { GT } from '../glossary-term'
import { STATUS_OPTIONS, defaultMonth, groupByMonth, isoToday, matchStatus, raceState } from '../race-ui'
import RaceDayGrid from './RaceDayGrid'
import RaceMapView from './RaceMapView'
import RaceMonthList from './RaceMonthList'
import RaceRow from './RaceRow'
import RaceStrip from './RaceStrip'

/** 分档阈值按**当前筛选下的最大场次**算，不写死绝对数。
 *  写死的话一筛到某一年（最多 38 场）整张图会挤在最浅档，看不出任何差异。 */
function raceThresholds(max: number): number[] {
  if (max <= 0) return [1, 2, 3]
  return [
    Math.max(1, Math.round(max * 0.1)),
    Math.max(2, Math.round(max * 0.3)),
    Math.max(3, Math.round(max * 0.6)),
  ]
}

/** 一次把当年全部赛事取回来（上游过滤后约 400 场），省/市/区、月份、月历
 *  都在前端切 —— 换来的是切视图与切省份零延迟，也不用发六个请求。
 *  服务端硬上限 2000 行，超了会走 racesTruncated 提示。 */
const LIST_LIMIT = 2000

/** 稳定空数组：给「数据还没到」用，避免每次渲染造新引用。 */
const EMPTY_LIST: RaceEvent[] = []

type Tab = 'month' | 'days' | 'map'

const TABS: { key: Tab; labelKey: TKey }[] = [
  { key: 'month', labelKey: 'racesViewMonth' },
  { key: 'days', labelKey: 'racesViewDays' },
  { key: 'map', labelKey: 'racesViewMap' },
]

const CATEGORY_OPTIONS: { key: string; labelKey: TKey }[] = [
  { key: '', labelKey: 'racesCatAll' },
  { key: 'full', labelKey: 'racesCatFull' },
  { key: 'half', labelKey: 'racesCatHalf' },
  { key: '10k', labelKey: 'racesCatTen' },
]


/** 赛事日历：三栏（筛选 / 视图 / 地区下钻）+ 三个并列视图。
 *
 * 布局参考 nowrun.cn 的骨架：首屏先给「即将开赛」这条可行动的横滚，左边是第一人称
 * 分组的筛选，中间在月份 / 赛历 / 跑马地图之间切，右边是地区下钻。
 * 数据口径没跟着抄 —— 报名状态与价格上游都没有（见 DEVLOG Round 44），
 * 所以横滚条按开赛日排，不假装知道报名情况。
 *
 * 地图只到省级是数据决定的：上游给的是行政区**名称**而不是经纬度，
 * 仓库里也只有省级边界，所以市/区用列表下钻表达，不假装有矢量边界。
 *
 * 分级图没有标签就读不出来（官方制图指南也是这条），所以省名 + 场次直接画在图上，
 * 并按「场次多的先落位、撞了就不画」避让；没标签的省靠悬停读数与右侧榜单兜底。
 */
export default function RaceCalendar() {
  const { t } = useLocale()
  const [features, setFeatures] = useState<GeoFeature[] | null>(null)
  const [summary, setSummary] = useState<RaceSummary | null>(null)
  const [year, setYear] = useState('')
  const [category, setCategory] = useState('')
  const [grade, setGrade] = useState('')
  const [province, setProvince] = useState<string | null>(null)
  const [city, setCity] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)
  const [tab, setTab] = useState<Tab>('map')
  const [status, setStatus] = useState('')
  // null = 用户还没动过展开状态，跟着「最近一场未开始的比赛」所在月走
  const [openMonths, setOpenMonths] = useState<Set<string> | null>(null)

  useEffect(() => {
    let cancelled = false
    loadProvinces()
      .then((f) => {
        if (!cancelled) setFeatures(f)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  const filters = useMemo(
    () => ({
      year: year ? Number(year) : undefined,
      category: category || undefined,
      grade: grade || undefined,
    }),
    [year, category, grade],
  )

  // setState 推迟到微任务：effect 调用栈内同步 setState 命中 set-state-in-effect
  const refreshSummary = useCallback(
    () =>
      Promise.resolve().then(async () => {
        setBusy(true)
        setError(null)
        try {
          setSummary(await racesApi.summary(filters))
        } catch (e) {
          setError(e instanceof Error ? e.message : String(e))
        } finally {
          setBusy(false)
        }
      }),
    [filters],
  )

  useEffect(() => {
    void refreshSummary()
  }, [refreshSummary])

  // 明细一次拿全，键不匹配就当没数据 —— 切筛选时无需在 effect 里同步清空
  const listKey = `${year}|${category}|${grade}`
  const [bundle, setBundle] = useState<{ key: string; list: RaceEvent[] } | null>(null)
  useEffect(() => {
    let cancelled = false
    void Promise.resolve().then(async () => {
      try {
        const list = await racesApi.list({ ...filters, limit: LIST_LIMIT })
        if (!cancelled) setBundle({ key: listKey, list })
      } catch {
        if (!cancelled) setBundle({ key: listKey, list: [] })
      }
    })
    return () => {
      cancelled = true
    }
  }, [listKey, filters])

  // 必须 memo：下面 scoped 的 useMemo 依赖它，字面量 [] 每次渲染都是新引用，
  // exhaustive-deps 会直接报错（而且 scoped 会白算一遍）。
  const allEvents = useMemo(
    () => (bundle && bundle.key === listKey ? bundle.list : EMPTY_LIST),
    [bundle, listKey],
  )

  // 悬停/选中的省一定要有读数，哪怕它的标签被避让掉了
  // 每次渲染重算足够便宜；跨午夜时用户点一下任何控件就会刷新
  const today = isoToday()
  // 状态筛选是「时间视图」的控件，地图不受它影响（地图答的是哪儿有比赛，
  // 不是哪儿还有比赛），所以 map tab 上强制不限。
  const statusPick = tab === 'map' ? '' : status
  const scoped = useMemo(
    () =>
      allEvents.filter(
        (r) =>
          (!province || r.province === province) &&
          (!city || r.city === city) &&
          matchStatus(raceState(r.date, today).key, statusPick),
      ),
    [allEvents, province, city, statusPick, today],
  )

  const months = useMemo(() => [...groupByMonth(scoped).keys()], [scoped])
  const openSet = useMemo(
    () => openMonths ?? new Set([defaultMonth(scoped, today)]),
    [openMonths, scoped, today],
  )

  const counts = useMemo(() => {
    const m = new Map<string, number>()
    for (const p of summary?.provinces ?? []) m.set(p.name, p.count)
    return m
  }, [summary])

  // 阈值跟着当前筛选走：全量与「只看某一年」的场次差一个量级
  const maxCount = useMemo(() => Math.max(0, ...counts.values()), [counts])
  const thresholds = useMemo(() => raceThresholds(maxCount), [maxCount])

  const selProvince = useMemo(
    () => summary?.provinces.find((p) => p.name === province) ?? null,
    [summary, province],
  )
  const selCities: RaceCityCount[] = selProvince?.cities ?? []
  const selDistricts = selCities.find((c) => c.name === city)?.districts ?? []

  async function manualRefresh() {
    setBusy(true)
    setError(null)
    setNote(null)
    try {
      const res = await racesApi.refresh()
      // 报名那一步失败不能混在「已更新」里 —— 赛历新了、窗口还是旧的，
      // 说「已更新」就是在骗人，所以分开报。
      setNote(
        res.regNote
          ? t('racesUpdatedRegFailed', { n: res.imported, err: res.regNote })
          : t('racesUpdated', { n: res.imported, reg: res.registrations }),
      )
      await refreshSummary()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const lastSync = summary?.lastSyncAt
    ? new Date(summary.lastSyncAt * 1000).toLocaleString(undefined, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    : null

  function pickProvince(name: string) {
    const next = province === name ? null : name
    setProvince(next)
    setCity(null)
  }

  function clearFilters() {
    setYear('')
    setCategory('')
    setGrade('')
    setProvince(null)
    setCity(null)
    setStatus('')
  }

  const hasFilters = !!(year || category || grade || province || status)

  return (
    <section aria-label={t('racesAria')} className="mx-auto max-w-[1600px] px-6 py-5">
      <div className="mb-1 flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold text-foreground">{t('racesTitle')}</h1>
        {summary && (
          <span className="text-sm text-muted-foreground">
            <span className="font-mono text-base text-foreground">{summary.total}</span>{' '}
            {t('racesTotalUnit')}
            {lastSync && ` · ${t('racesLastSync', { time: lastSync })}`}
          </span>
        )}
        <button
          onClick={() => void manualRefresh()}
          disabled={busy}
          className="ml-auto cursor-pointer rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
        >
          {busy ? t('racesRefreshing') : t('racesRefresh')}
        </button>
      </div>
      <p className="mb-4 text-sm leading-relaxed text-muted-foreground">
        <GT text={t('racesHint')} />
      </p>

      {error && (
        <p role="alert" className="mb-2 text-sm text-destructive">
          {error}
        </p>
      )}
      {note && <p className="mb-2 text-sm text-muted-foreground">{note}</p>}
      {summary?.lastError && (
        <p className="mb-2 text-sm text-destructive">{t('racesLastError', { err: summary.lastError })}</p>
      )}

      {summary && summary.total > 0 && <RaceStrip races={allEvents} today={today} />}

      <div className="flex flex-col gap-4 xl:flex-row xl:items-start">
        {/* 左：第一人称分组的筛选（参考 nowrun 的「我想去哪儿 / 我想跑多远」） */}
        <aside className="shrink-0 rounded-lg border border-border bg-muted/20 p-4 xl:sticky xl:top-4 xl:w-[210px]">
          <div className="mb-3 flex items-baseline justify-between">
            <h2 className="text-base font-medium text-foreground">{t('racesFilterTitle')}</h2>
            {hasFilters && (
              <button
                onClick={clearFilters}
                className="cursor-pointer text-xs text-secondary hover:underline"
              >
                {t('racesFilterClear')}
              </button>
            )}
          </div>
          <ChipGroup
            label={t('racesDistance')}
            options={CATEGORY_OPTIONS.map((o) => ({ key: o.key, text: t(o.labelKey) }))}
            value={category}
            onChange={setCategory}
          />
          <ChipGroup
            label={t('racesCare')}
            options={[
              { key: '', text: t('racesGradeAll') },
              ...(summary?.grades ?? []).map((g) => ({ key: g.name, text: `${g.name} · ${g.count}` })),
            ]}
            value={grade}
            onChange={setGrade}
          />
          <div className="mb-3">
            <div className="mb-1.5 border-l-2 border-secondary pl-2 text-xs text-muted-foreground">
              {t('racesWhen')}
            </div>
            <div className="text-sm text-muted-foreground">
              <select
                aria-label={t('racesYear')}
                value={year}
                onChange={(e) => {
                  setYear(e.target.value)
                  setProvince(null)
                  setCity(null)
                }}
                className="w-full cursor-pointer rounded-md border border-border bg-card px-3 py-1.5 text-sm text-foreground"
              >
                <option value="">{t('racesYearAll')}</option>
                {(summary?.years ?? []).map((y) => (
                  <option key={y} value={y}>
                    {y}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </aside>

        {/* 中：视图切换 + 当前视图 */}
        <div className="min-w-0 flex-1">
          <div className="mb-3 flex flex-wrap items-center gap-3">
            <div className="flex rounded-lg border border-border bg-muted/30 p-1">
              {TABS.map((x) => (
                <button
                  key={x.key}
                  onClick={() => setTab(x.key)}
                  aria-pressed={tab === x.key}
                  className={`cursor-pointer rounded-md px-3 py-1.5 text-sm transition-colors ${
                    tab === x.key
                      ? 'bg-card font-medium text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {t(x.labelKey)}
                </button>
              ))}
            </div>
            {tab !== 'map' && (
              <>
                <div className="flex flex-wrap gap-1.5">
                  {STATUS_OPTIONS.map((o) => (
                    <button
                      key={o.key}
                      onClick={() => setStatus(o.key)}
                      aria-pressed={status === o.key}
                      className={`cursor-pointer rounded-md border px-2.5 py-1 text-xs ${
                        status === o.key
                          ? 'border-secondary bg-secondary/15 text-secondary'
                          : 'border-border text-muted-foreground hover:border-secondary'
                      }`}
                    >
                      {t(o.labelKey)}
                    </button>
                  ))}
                </div>
                {tab === 'month' && (
                  <button
                    onClick={() =>
                      setOpenMonths(
                        openSet.size === months.length ? new Set<string>() : new Set(months),
                      )
                    }
                    className="ml-auto cursor-pointer rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:border-secondary"
                  >
                    {openSet.size === months.length
                      ? t('racesCollapseAll')
                      : t('racesExpandAll')}
                  </button>
                )}
              </>
            )}
          </div>

          {!features && <p className="text-sm text-muted-foreground">{t('racesLoading')}</p>}
          {features && summary && summary.total === 0 && (
            <p className="mb-3 text-sm text-muted-foreground">{t('racesNoData')}</p>
          )}

          {tab === 'month' && (
            <RaceMonthList
              months={months}
              open={openSet}
              races={scoped}
              today={today}
              onToggle={(m) =>
                setOpenMonths(
                  new Set(
                    openSet.has(m)
                      ? [...openSet].filter((x) => x !== m)
                      : [...openSet, m],
                  ),
                )
              }
            />
          )}

          {tab === 'days' && <RaceDayGrid races={scoped} today={today} />}

          {tab === 'map' && features && (
            <RaceMapView
              features={features}
              counts={counts}
              thresholds={thresholds}
              maxCount={maxCount}
              province={province}
              onPickProvince={pickProvince}
            />
          )}
        </div>

        {/* 右：地区下钻（省 → 市 → 区），选中后同时收窄中间视图 */}
        <aside className="min-w-0 shrink-0 xl:w-[360px]">
          <div className="mb-2 border-l-2 border-secondary pl-2 text-xs text-muted-foreground">
            {t('racesWhere')}
          </div>
          {!province && (
            <div className="rounded-lg border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
              <div className="mb-3 text-base font-medium text-foreground">{t('racesTopTitle')}</div>
              {(summary?.provinces ?? []).slice(0, 12).map((p) => (
                <button
                  key={p.name}
                  onClick={() => pickProvince(p.name)}
                  className="flex w-full cursor-pointer items-baseline gap-3 rounded px-2 py-1 text-left hover:bg-muted"
                >
                  <span className="w-20 shrink-0 truncate">{shortName(p.name)}</span>
                  <span className="font-mono text-foreground">{p.count}</span>
                  <span
                    className="ml-auto h-2 rounded-sm bg-secondary/60"
                    style={{ width: `${Math.max(4, (p.count / (maxCount || 1)) * 100)}%` }}
                  />
                </button>
              ))}
              <p className="mt-3">{t('racesSelectProvince')}</p>
            </div>
          )}

          {province && selProvince && (
            <div className="rounded-lg border border-border bg-muted/30 p-4">
              <div className="mb-3 flex items-baseline gap-3">
                <span className="text-lg font-medium text-foreground">{selProvince.name}</span>
                <span className="text-sm text-muted-foreground">
                  <span className="font-mono text-base text-foreground">{selProvince.count}</span>{' '}
                  {t('racesTotalUnit')}
                </span>
                <button
                  onClick={() => pickProvince(province)}
                  className="ml-auto cursor-pointer text-sm text-secondary hover:underline"
                >
                  {t('racesClear')}
                </button>
              </div>

              {!city && (
                <div className="grid grid-cols-2 gap-1.5 text-sm">
                  {selCities.map((c) => (
                    <button
                      key={c.name}
                      onClick={() => setCity(c.name)}
                      className="flex cursor-pointer items-baseline justify-between gap-2 rounded-md border border-border bg-card px-3 py-2 hover:border-secondary"
                    >
                      <span className="truncate">{shortName(c.name)}</span>
                      <span className="font-mono text-muted-foreground">{c.count}</span>
                    </button>
                  ))}
                </div>
              )}

              {city && (
                <div className="mb-3 text-sm">
                  <button
                    onClick={() => setCity(null)}
                    className="mb-2 cursor-pointer text-secondary hover:underline"
                  >
                    {t('racesBackToCities')}
                  </button>
                  <div className="mb-1.5 text-base font-medium text-foreground">{city}</div>
                  <div className="flex flex-wrap gap-1.5">
                    {selDistricts.map((d) => (
                      <span key={d.name} className="rounded-md border border-border bg-card px-2.5 py-1">
                        {d.name} <span className="font-mono text-muted-foreground">{d.count}</span>
                      </span>
                    ))}
                    {selDistricts.length === 0 && (
                      <span className="text-muted-foreground">{t('racesNoDistrict')}</span>
                    )}
                  </div>
                </div>
              )}

              {tab === 'map' && (
                <ul className="mt-3 max-h-[560px] divide-y divide-border overflow-y-auto text-sm">
                  {scoped.map((e) => (
                    <RaceRow key={e.raceId} race={e} today={today} showDateBlock={false} />
                  ))}
                  {scoped.length === 0 && <li className="py-2 text-muted-foreground">{t('racesEmpty')}</li>}
                </ul>
              )}
              {allEvents.length >= LIST_LIMIT && (
                <p className="pt-1 text-xs text-muted-foreground">
                  {t('racesTruncated', { n: LIST_LIMIT })}
                </p>
              )}
            </div>
          )}
        </aside>
      </div>
    </section>
  )
}

/** 一组单选 chip。空 key 表示「不限」，与后端的「零值 = 不限」口径一致。 */
function ChipGroup({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: { key: string; text: string }[]
  value: string
  onChange: (v: string) => void
}) {
  return (
    <div className="mb-3">
      <div className="mb-1.5 border-l-2 border-secondary pl-2 text-xs text-muted-foreground">
        {label}
      </div>
      <div className="flex flex-wrap gap-1.5">
        {options.map((o) => (
          <button
            key={o.key || 'all'}
            onClick={() => onChange(o.key)}
            aria-pressed={value === o.key}
            className={`cursor-pointer rounded-md border px-2.5 py-1 text-xs ${
              value === o.key
                ? 'border-secondary bg-secondary/15 text-secondary'
                : 'border-border text-muted-foreground hover:border-secondary'
            }`}
          >
            {o.text}
          </button>
        ))}
      </div>
    </div>
  )
}
