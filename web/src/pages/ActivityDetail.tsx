import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import * as echarts from 'echarts'
import { activities, dbal, wbal, type ActivityDetailT, type DBalSeries, type WBalSeries } from '../api'
import { formatDistance, formatElevation, formatPace, paceUnit, useUnit } from '../units'
import { readActiveAthlete } from '../athlete'
import { useLocale } from '../i18n'
import { fmtDateTime } from '../format'
import type { TKey } from '../i18n'
import CoachActivityAnalysisBlock from './CoachActivityAnalysis'
import { GT, InfoTip } from '../glossary-term'

const SPORT_KEYS: Record<string, TKey> = { run: 'sportRun', ride: 'sportRide', swim: 'sportSwim', other: 'sportOther' }

// 守卫哨兵：渲染时经 t('detailGuardError') 出文案（初始 state 拿不到 hook）
const GUARD_MISSING = '__guard_missing__'

/** 活动详情页：summary（票 03）+ 指标（票 05/06）+ D'bal 曲线（票 08）。 */
/** 指标说明浮标：hover 显示解释（纯 CSS，无状态）。 */
export default function ActivityDetail() {
  const { unit } = useUnit()
  const { t } = useLocale()
  const { id } = useParams()
  const [activity, setActivity] = useState<ActivityDetailT | null>(null)
  // 守卫错误在初始 state 给出（effect 调用栈内同步 setState 命中 set-state-in-effect）
  const [error, setError] = useState<string | null>(() =>
    !readActiveAthlete() || !id ? GUARD_MISSING : null,
  )
  const [dbalSeries, setDbalSeries] = useState<DBalSeries | null>(null)
  const [wbalSeries, setWbalSeries] = useState<WBalSeries | null>(null)
  const dbalRef = useRef<HTMLDivElement>(null)
  const dbalChart = useRef<echarts.ECharts | null>(null)
  const wbalRef = useRef<HTMLDivElement>(null)
  const wbalChart = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    const athleteId = readActiveAthlete()
    if (!athleteId || !id) return
    activities
      .detail(athleteId, Number(id))
      .then((a) => {
        setActivity(a)
        if (a.hasStream) {
          dbal(athleteId, Number(id))
            .then(setDbalSeries)
            .catch(() => setDbalSeries(null)) // 未配 CS 等情况静默隐藏
          wbal(athleteId, Number(id))
            .then(setWbalSeries)
            .catch(() => setWbalSeries(null)) // 无功率流静默隐藏
        }
      })
      .catch((err: Error) => setError(err.message))
  }, [id])

  useEffect(() => {
    if (!dbalRef.current || !dbalSeries) return
    if (!dbalChart.current) {
      dbalChart.current = echarts.init(dbalRef.current)
    }
    const xs = dbalSeries.dbal.map((_, i) => Math.round((i * dbalSeries.intervalSecs) / 60))
    dbalChart.current.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'axis',
          backgroundColor: '#111827',
          borderColor: '#334155',
          textStyle: { color: '#f8fafc' },
          valueFormatter: (v: number | string) => `${Number(v).toFixed(0)} m`,
        },
        grid: { left: 50, right: 20, top: 20, bottom: 30 },
        xAxis: { type: 'category', data: xs, name: 'min', axisLabel: { color: '#94a3b8' } },
        yAxis: {
          type: 'value',
          name: "D'bal (m)",
          axisLabel: { color: '#94a3b8' },
          splitLine: { lineStyle: { color: '#1e293b' } },
        },
        series: [
          {
            type: 'line',
            data: dbalSeries.dbal,
            symbol: 'none',
            lineStyle: { width: 1.5, color: '#d97706' },
            areaStyle: { color: 'rgba(217,119,6,0.15)' },
          },
        ],
      },
      true,
    )
  }, [dbalSeries])

  // W'bal 图渲染
  useEffect(() => {
    if (!wbalRef.current || !wbalSeries) return
    if (!wbalChart.current) {
      wbalChart.current = echarts.init(wbalRef.current)
    }
    const xs = wbalSeries.wbal.map((_, i) => Math.round((i * wbalSeries.intervalSecs) / 60))
    wbalChart.current.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'axis',
          backgroundColor: '#111827',
          borderColor: '#334155',
          textStyle: { color: '#f8fafc' },
          valueFormatter: (v: number | string) => `${Number(v).toFixed(0)} J`,
        },
        grid: { left: 60, right: 20, top: 20, bottom: 30 },
        xAxis: { type: 'category', data: xs, name: 'min', axisLabel: { color: '#94a3b8' } },
        yAxis: {
          type: 'value',
          name: "W'bal (J)",
          axisLabel: { color: '#94a3b8' },
          splitLine: { lineStyle: { color: '#1e293b' } },
        },
        series: [
          {
            type: 'line',
            data: wbalSeries.wbal,
            symbol: 'none',
            lineStyle: { width: 1.5, color: '#3b82f6' },
            areaStyle: { color: 'rgba(59,130,246,0.15)' },
          },
        ],
      },
      true,
    )
  }, [wbalSeries])

  useEffect(
    () => () => {
      dbalChart.current?.dispose()
      dbalChart.current = null
      wbalChart.current?.dispose()
      wbalChart.current = null
    },
    [],
  )

  if (error) {
    return (
      <main className="mx-auto max-w-4xl px-6 py-10">
        <p role="alert" className="rounded border border-destructive bg-card p-4 text-sm text-destructive">
          {error === GUARD_MISSING ? t('detailGuardError') : error}
        </p>
        <Link to="/" className="mt-4 inline-block text-sm text-secondary hover:underline">
          {t('detailBack')}
        </Link>
      </main>
    )
  }
  if (!activity) {
    return <div className="grid min-h-screen place-items-center bg-background text-muted-foreground">{t('detailLoading')}</div>
  }

  const rows: [string, string, string?][] = [
    ['detailType', SPORT_KEYS[activity.sport] ? t(SPORT_KEYS[activity.sport]) : activity.sport],
    ['detailStart', fmtDateTime(activity.startTime)],
    ['detailDuration', t('detailDurationFmt', { m: Math.floor(activity.durationSecs / 60), s: activity.durationSecs % 60 })],
    ['detailDistance', formatDistance(activity.distanceM, unit, 2)],
    ['detailElevation', formatElevation(activity.elevationGainM, unit)],
    [
      'detailAvgPace',
      activity.distanceM > 0
        ? `${formatPace(activity.durationSecs / (activity.distanceM / 1000), unit)}${paceUnit(unit)}`
        : '—',
    ],
    [
      'detailAvgCadence',
      activity.stats && activity.stats.avgCadenceSpm > 0
        ? `${activity.stats.avgCadenceSpm.toFixed(0)} spm`
        : '—',
    ],
    [
      'detailAvgStride',
      activity.stats && activity.stats.avgStrideM > 0
        ? `${(unit === 'imperial' ? activity.stats.avgStrideM * 3.280839895 : activity.stats.avgStrideM).toFixed(2)} ${unit === 'imperial' ? 'ft' : 'm'}`
        : '—',
    ],
    [
      'detailTotalSteps',
      activity.stats && activity.stats.totalSteps > 0
        ? activity.stats.totalSteps.toLocaleString('zh-CN')
        : '—',
    ],
    [
      'detailMaxCadence',
      activity.stats && activity.stats.maxCadenceSpm > 0
        ? `${activity.stats.maxCadenceSpm.toFixed(0)} spm`
        : '—',
    ],
    ['detailSource', activity.source],
    ['detailStream', activity.hasStream ? t('detailYes') : t('detailNo')],
    ['detailAvgHr', activity.metrics ? activity.metrics.avgHr.toFixed(0) + ' bpm' : '—'],
    [
      'TRIMP',
      activity.metrics ? activity.metrics.trimp.toFixed(0) : '—',
      'tipTrimp',
    ],
    [
      'TRIMP(100)',
      activity.metrics ? activity.metrics.trimp100.toFixed(0) : '—',
      'tipTrimp100',
    ],
    [
      'TRIMP Zonal',
      activity.metrics ? activity.metrics.trimpZonal.toFixed(0) : '—',
      'tipTrimpZonal',
    ],
    [
      'rTSS',
      activity.metrics ? activity.metrics.rtss.toFixed(0) : '—',
      'tipRtss',
    ],
    [
      'NP',
      activity.metrics && activity.metrics.np > 0
        ? activity.metrics.np.toFixed(0) + ' W'
        : t('detailNoPower'),
      'tipNp',
    ],
    [
      'detailPowerLoad',
      activity.metrics && activity.metrics.powerLoad > 0
        ? activity.metrics.powerLoad.toFixed(0)
        : activity.metrics && activity.metrics.np > 0
          ? t('detailNeedCp')
          : t('detailNoPower'),
      'tipPowerLoad',
    ],
  ]

  return (
    <main className="mx-auto max-w-4xl px-6 py-10">
      <Link to="/" className="text-sm text-secondary hover:underline">
        {t('detailBack')}
      </Link>
      <h1 className="my-4 flex flex-wrap items-center gap-3 text-2xl font-semibold">
        <span>
          {t('detailActivityTitle', { id: activity.id, sport: SPORT_KEYS[activity.sport] ? t(SPORT_KEYS[activity.sport]) : activity.sport })}
          {activity.isPlanned && (
            <span className="ml-2 rounded bg-muted px-2 py-1 text-sm text-accent">{t('detailPlannedBadge')}</span>
          )}
        </span>
        {activity.hasGps && (
          <Link
            to={`/map?activity=${activity.id}`}
            className="rounded border border-border px-3 py-1 text-sm font-normal text-secondary hover:underline"
          >
            {t('detailViewOnMap')}
          </Link>
        )}
      </h1>

      <dl className="grid grid-cols-1 gap-px rounded-lg border border-border bg-border sm:grid-cols-2">
        {rows.map(([k, v, info]) => (
          <div key={k} className="bg-card px-4 py-3">
            <dt className="flex items-center text-xs text-muted-foreground">
              {info ? (
                <>
                  {t(k as never)}
                  <InfoTip label={t(k as never)} text={t(info as never)} />
                </>
              ) : (
                // 没有富文案 ⓘ 的指标：交给术语表，TRIMP / rTSS / NP / W′ 等自动可查
                <GT text={t(k as never)} />
              )}
            </dt>
            <dd className="mt-1 font-mono text-sm">{v}</dd>
          </div>
        ))}
      </dl>

      {activity.metrics && (
        <section aria-label={t('detailHrZonesAria')} className="mt-6 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground"><GT text={t('detailHrZonesTitle')} /></h2>
          <div className="flex gap-2">
            {activity.metrics.hrTiZ.map((secs, i) => {
              const total = activity.metrics!.hrTiZ.reduce((a, b) => a + b, 0)
              const pct = total > 0 ? (secs / total) * 100 : 0
              return (
                <div key={i} className="flex-1">
                  <div className="h-2 rounded bg-muted">
                    <div className="h-2 rounded bg-secondary" style={{ width: `${pct}%` }} />
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    Z{i + 1} · {Math.round(secs / 60)}min
                  </div>
                </div>
              )
            })}
          </div>
        </section>
      )}
      <CoachActivityAnalysisBlock activityId={Number(id)} />

      {activity.metrics && activity.metrics.paceTiZ.some((x) => x > 0) && (
        <section aria-label={t('detailPaceZonesAria')} className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">{t('detailPaceZonesTitle')}</h2>
          <div className="flex gap-2">
            {activity.metrics.paceTiZ.map((secs, i) => {
              const total = activity.metrics!.paceTiZ.reduce((a, b) => a + b, 0)
              const pct = total > 0 ? (secs / total) * 100 : 0
              return (
                <div key={i} className="flex-1">
                  <div className="h-2 rounded bg-muted">
                    <div className="h-2 rounded bg-accent" style={{ width: `${pct}%` }} />
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    P{i + 1} · {Math.round(secs / 60)}min
                  </div>
                </div>
              )
            })}
          </div>
        </section>
      )}
      {dbalSeries && (
        <section aria-label={t('detailDbalAria')} className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">
            <GT text={t('detailDbalTitle', { cs: dbalSeries.cs.toFixed(2), d: dbalSeries.dprime.toFixed(0) })} />
          </h2>
          <div ref={dbalRef} className="h-56 w-full" role="img" aria-label={t('detailDbalAria')} />
          <p className="mt-1 text-xs text-muted-foreground">
            <GT text={t('detailDbalHint')} />
          </p>
        </section>
      )}
      {activity.metrics && activity.metrics.powerTiZ.some((x) => x > 0) && (
        <section aria-label={t('detailPowerZonesAria')} className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground"><GT text={t('detailPowerZonesTitle')} /></h2>
          <div className="flex gap-2">
            {activity.metrics.powerTiZ.map((secs, i) => {
              const total = activity.metrics!.powerTiZ.reduce((a, b) => a + b, 0)
              const pct = total > 0 ? (secs / total) * 100 : 0
              return (
                <div key={i} className="flex-1">
                  <div className="h-2 rounded bg-muted">
                    <div className="h-2 rounded bg-secondary" style={{ width: `${pct}%` }} />
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    W{i + 1} · {Math.round(secs / 60)}min
                  </div>
                </div>
              )
            })}
          </div>
        </section>
      )}
      {wbalSeries && (
        <section aria-label={t('detailWbalAria')} className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">
            <GT text={t('detailWbalTitle', { cp: wbalSeries.cp.toFixed(0), wp: wbalSeries.wprime.toFixed(0), tau: wbalSeries.tau.toFixed(0) })} />
          </h2>
          <div ref={wbalRef} className="h-56 w-full" role="img" aria-label={t('detailWbalAria')} />
          <p className="mt-1 text-xs text-muted-foreground">
            <GT text={t('detailWbalHint')} />
          </p>
        </section>
      )}
      {activity.metrics?.isEstimated && (
        <p className="mt-2 text-xs text-accent">{t('detailEstimated')}</p>
      )}

    </main>
  )
}
