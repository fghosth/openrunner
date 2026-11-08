import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import * as echarts from 'echarts'
import { activities, dbal, wbal, type ActivityDetailT, type DBalSeries, type WBalSeries } from '../api'
import { formatDistance, formatElevation, formatPace, paceUnit, useUnit } from '../units'
import { readActiveAthlete } from '../App'
import { fmtDateTime, sportNames } from '../format'
import CoachSessionAnalysisBlock from './CoachSessionAnalysis'

const sportNamesLocal: Record<string, string> = sportNames

/** 活动详情页：summary（票 03）+ 指标（票 05/06）+ D'bal 曲线（票 08）。 */
/** 指标说明浮标：hover 显示解释（纯 CSS，无状态）。 */
function MetricInfo({ text }: { text: string }) {
  return (
    <span className="group relative ml-1 inline-flex align-middle">
      <span
        aria-label={text}
        className="flex h-3.5 w-3.5 cursor-help items-center justify-center rounded-full border border-border text-[9px] leading-none text-muted-foreground"
      >
        i
      </span>
      <span className="pointer-events-none absolute bottom-full left-1/2 z-20 mb-1.5 w-64 -translate-x-1/2 rounded border border-border bg-card p-2 text-[11px] leading-relaxed text-foreground opacity-0 shadow-lg transition-opacity group-hover:opacity-100">
        {text}
      </span>
    </span>
  )
}

export default function ActivityDetail() {
  const { unit } = useUnit()
  const { id } = useParams()
  const [activity, setActivity] = useState<ActivityDetailT | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [dbalSeries, setDbalSeries] = useState<DBalSeries | null>(null)
  const [wbalSeries, setWbalSeries] = useState<WBalSeries | null>(null)
  const dbalRef = useRef<HTMLDivElement>(null)
  const dbalChart = useRef<echarts.ECharts | null>(null)
  const wbalRef = useRef<HTMLDivElement>(null)
  const wbalChart = useRef<echarts.ECharts | null>(null)

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

  const rows: [string, string, string?][] = [
    ['类型', sportNamesLocal[activity.sport] ?? activity.sport],
    ['开始时间', fmtDateTime(activity.startTime)],
    ['时长', `${Math.floor(activity.durationSecs / 60)} 分 ${activity.durationSecs % 60} 秒`],
    ['距离', formatDistance(activity.distanceM, unit, 2)],
    ['爬升', formatElevation(activity.elevationGainM, unit)],
    [
      '平均配速',
      activity.distanceM > 0
        ? `${formatPace(activity.durationSecs / (activity.distanceM / 1000), unit)}${paceUnit(unit)}`
        : '—',
    ],
    [
      '平均步频',
      activity.stats && activity.stats.avgCadenceSpm > 0
        ? `${activity.stats.avgCadenceSpm.toFixed(0)} spm`
        : '—',
    ],
    [
      '平均步幅',
      activity.stats && activity.stats.avgStrideM > 0
        ? `${(unit === 'imperial' ? activity.stats.avgStrideM * 3.280839895 : activity.stats.avgStrideM).toFixed(2)} ${unit === 'imperial' ? 'ft' : 'm'}`
        : '—',
    ],
    [
      '总步数',
      activity.stats && activity.stats.totalSteps > 0
        ? activity.stats.totalSteps.toLocaleString('zh-CN')
        : '—',
    ],
    [
      '最大步频',
      activity.stats && activity.stats.maxCadenceSpm > 0
        ? `${activity.stats.maxCadenceSpm.toFixed(0)} spm`
        : '—',
    ],
    ['来源', activity.source],
    ['Stream', activity.hasStream ? '有' : '无'],
    ['平均心率', activity.metrics ? activity.metrics.avgHr.toFixed(0) + ' bpm' : '—'],
    [
      'TRIMP',
      activity.metrics ? activity.metrics.trimp.toFixed(0) : '—',
      '【这堂课有多累】跑多久 × 心率有多高，加权算出来的一个「疲劳分」。心率越高，权重涨得越快（不是直线，是指数）。举例：轻松跑 1 小时约 50 分，全力比赛 1 小时约 200 分。数值越大，身体欠的越多。',
    ],
    [
      'TRIMP(100)',
      activity.metrics ? activity.metrics.trimp100.toFixed(0) : '—',
      '【换成统一尺子】把「用你的阈值心率跑满 1 小时」定为 100 分，好跟别人、跟别的运动比，也好按周/月累加成训练负荷。举例：60 分 ≈ 阈值强度跑了 36 分钟。',
    ],
    [
      'TRIMP Zonal',
      activity.metrics ? activity.metrics.trimpZonal.toFixed(0) : '—',
      '【另一种算法：按心率区间算】把每秒按心率落进 5 个区间，强度越高的区间权重越大（系数 0.84 / 1.65 / 2.57 / 4.01 / 5.91）。它和上面的 TRIMP 通常应当接近——差很多说明你的心率在区间边界来回跳。',
    ],
    [
      'rTSS',
      activity.metrics ? activity.metrics.rtss.toFixed(0) : '—',
      '【这次跑步的工作量】只看配速，不看心率：用阈值配速跑 1 小时 = 100 分。举例：阈值配速跑 30 分钟约 50 分。和 TRIMP(100) 对比能看出当天状态——心率负荷低于配速负荷，说明那次跑得比较省力。',
    ],
    [
      'NP',
      activity.metrics && activity.metrics.np > 0
        ? activity.metrics.np.toFixed(0) + ' W'
        : '—（无功率数据）',
      '【标准化功率】把起伏的功率「抹平」成一个等效恒定值：功率忽高忽低比恒定更累，所以先做 30 秒滑动平均，再按四次方平均（对冲刺特别敏感）。需要功率计，骑行最常用。',
    ],
    [
      '功率负荷',
      activity.metrics && activity.metrics.powerLoad > 0
        ? activity.metrics.powerLoad.toFixed(0)
        : activity.metrics && activity.metrics.np > 0
          ? '—（需配置功率 CP）'
          : '—（无功率数据）',
      '【功率版的训练负荷】和 rTSS 同一个公式，只是把「配速 ÷ 阈值配速」换成「NP ÷ CP」：100 × 时长 × (NP/CP)²。CP 是你的临界功率，要先在设置里配置「功率域 CP/W’」——没配置就算不出来，这栏会是空的。',
    ],
  ]

  return (
    <main className="mx-auto max-w-4xl px-6 py-10">
      <Link to="/" className="text-sm text-secondary hover:underline">
        ← 返回列表
      </Link>
      <h1 className="my-4 flex flex-wrap items-center gap-3 text-2xl font-semibold">
        <span>
          活动 #{activity.id} · {sportNamesLocal[activity.sport] ?? activity.sport}
          {activity.isPlanned && (
            <span className="ml-2 rounded bg-muted px-2 py-1 text-sm text-accent">计划</span>
          )}
        </span>
        {activity.hasGps && (
          <Link
            to={`/map?activity=${activity.id}`}
            className="rounded border border-border px-3 py-1 text-sm font-normal text-secondary hover:underline"
          >
            在地图上查看 ↗
          </Link>
        )}
      </h1>

      <dl className="grid grid-cols-1 gap-px rounded-lg border border-border bg-border sm:grid-cols-2">
        {rows.map(([k, v, info]) => (
          <div key={k} className="bg-card px-4 py-3">
            <dt className="flex items-center text-xs text-muted-foreground">
              {k}
              {info && <MetricInfo text={info} />}
            </dt>
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
      <CoachSessionAnalysisBlock activityId={Number(id)} />

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
      {activity.metrics && activity.metrics.powerTiZ.some((x) => x > 0) && (
        <section aria-label="功率区间分布" className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">功率区间分布（%CP）</h2>
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
        <section aria-label="Wbalance 曲线" className="mt-4 rounded-lg border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-medium text-muted-foreground">
            W&apos;balance（CP {wbalSeries.cp.toFixed(0)} W · W&apos; {wbalSeries.wprime.toFixed(0)} J · τ{' '}
            {wbalSeries.tau.toFixed(0)} s）
          </h2>
          <div ref={wbalRef} className="h-56 w-full" role="img" aria-label="Wbal 曲线" />
          <p className="mt-1 text-xs text-muted-foreground">
            Skiba 积分式：超 CP 消耗、低于 CP 按指数恢复——跑步功率用户的「电池」曲线
          </p>
        </section>
      )}
      {activity.metrics?.isEstimated && (
        <p className="mt-2 text-xs text-accent">估算指标：该活动无逐秒 Stream，由日均值近似。</p>
      )}

    </main>
  )
}
