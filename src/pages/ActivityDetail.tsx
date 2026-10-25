import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import * as echarts from 'echarts'
import { activities, dbal, type ActivityDetailT, type DBalSeries } from '../api'
import { readActiveAthlete } from '../App'
import { fmtDateTime, sportNames } from '../format'

const sportNamesLocal: Record<string, string> = sportNames

/** 活动详情页：summary（票 03）+ 指标（票 05/06）+ D'bal 曲线（票 08）。 */
export default function ActivityDetail() {
  const { id } = useParams()
  const [activity, setActivity] = useState<ActivityDetailT | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [dbalSeries, setDbalSeries] = useState<DBalSeries | null>(null)
  const dbalRef = useRef<HTMLDivElement>(null)
  const dbalChart = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    const athleteId = readActiveAthlete()
    if (!athleteId || !id) {
      setError('缺少档案或活动 id')
      return
    }
    activities
      .detail(athleteId, Number(id))
      .then((a) => {
        setActivity(a)
        if (a.hasStream) {
          dbal(athleteId, Number(id))
            .then(setDbalSeries)
            .catch(() => setDbalSeries(null)) // 未配 CS 等情况静默隐藏
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

  useEffect(
    () => () => {
      dbalChart.current?.dispose()
      dbalChart.current = null
    },
    [],
  )

  if (error) {
    return (
      <main className="mx-auto max-w-4xl px-6 py-10">
        <p role="alert" className="rounded border border-destructive bg-card p-4 text-sm text-destructive">
          {error}
        </p>
        <Link to="/" className="mt-4 inline-block text-sm text-secondary hover:underline">
          ← 返回列表
        </Link>
      </main>
    )
  }
  if (!activity) {
    return <div className="grid min-h-screen place-items-center bg-background text-muted-foreground">加载中…</div>
  }

  const rows: [string, string][] = [
    ['类型', sportNamesLocal[activity.sport] ?? activity.sport],
    ['开始时间', fmtDateTime(activity.startTime)],
    ['时长', `${Math.floor(activity.durationSecs / 60)} 分 ${activity.durationSecs % 60} 秒`],
    ['距离', `${(activity.distanceM / 1000).toFixed(2)} km`],
    ['爬升', `${activity.elevationGainM.toFixed(0)} m`],
    ['平均配速', activity.distanceM > 0 ? `${(activity.durationSecs / (activity.distanceM / 1000)).toFixed(0)} s/km` : '—'],
    ['来源', activity.source],
    ['Stream', activity.hasStream ? '有' : '无'],
    ['平均心率', activity.metrics ? activity.metrics.avgHr.toFixed(0) + ' bpm' : '—'],
    ['TRIMP', activity.metrics ? activity.metrics.trimp.toFixed(0) : '—'],
    ['TRIMP(100)', activity.metrics ? activity.metrics.trimp100.toFixed(0) : '—'],
    ['TRIMP Zonal', activity.metrics ? activity.metrics.trimpZonal.toFixed(0) : '—'],
    ['rTSS', activity.metrics ? activity.metrics.rtss.toFixed(0) : '—'],
  ]

  return (
    <main className="mx-auto max-w-4xl px-6 py-10">
      <Link to="/" className="text-sm text-secondary hover:underline">
        ← 返回列表
      </Link>
      <h1 className="my-4 text-2xl font-semibold">
        活动 #{activity.id} · {sportNamesLocal[activity.sport] ?? activity.sport}
        {activity.isPlanned && (
          <span className="ml-2 rounded bg-muted px-2 py-1 text-sm text-accent">计划</span>
        )}
      </h1>

      <dl className="grid grid-cols-1 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-2">
        {rows.map(([k, v]) => (
          <div key={k} className="bg-card px-4 py-3">
            <dt className="text-xs text-muted-foreground">{k}</dt>
            <dd className="mt-1 font-mono text-sm">{v}</dd>
          </div>
        ))}
      </dl>

      {activity.metrics && (
        <section aria-label="心率区间分布" className="mt-6 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">心率区间分布（%MaxHR）</h2>
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
      {activity.metrics && activity.metrics.paceTiZ.some((x) => x > 0) && (
        <section aria-label="配速区间分布" className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">配速区间分布（%阈值速度）</h2>
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
        <section aria-label="Dbalance 曲线" className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">
            D&apos;balance（CS {dbalSeries.cs.toFixed(2)} m/s · D&apos; {dbalSeries.dprime.toFixed(0)} m）
          </h2>
          <div ref={dbalRef} className="h-56 w-full" role="img" aria-label="Dbal 曲线" />
          <p className="mt-1 text-xs text-muted-foreground">
            超 CS 消耗、低于 CS 按剩余比例恢复——间歇课的「抽干-回血」画像
          </p>
        </section>
      )}
      {activity.metrics?.isEstimated && (
        <p className="mt-2 text-xs text-accent">估算指标：该活动无逐秒 Stream，由日均值近似。</p>
      )}

      <p className="mt-6 text-xs text-muted-foreground">
        W&apos;bal（功率链）随票 09 挂载。
      </p>
    </main>
  )
}
