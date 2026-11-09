import { useEffect, useMemo, useRef, useState } from 'react'
import * as echarts from 'echarts'
import { banister, type BanisterFit, type PMCMetric } from '../api'

import { readActiveAthlete } from '../athlete'
import { useLocale, useServerText } from '../i18n'
import { formatPace, paceUnit, useUnit } from '../units'

/** Banister 表现预测：历史成绩散点 + 模型拟合 + 未来 30 天外推。 */
const BANISTER_REASON_KEYS: Record<string, string> = {
  needThreeResults: 'banisterNeedThree',
  invalidResults: 'banisterInvalid',
}

/** Banister 拟合不可用的原因：code → 本地模板（带 {n} 插值），否则回落服务端原文。 */
function banisterReason(
  code: string | undefined,
  fallback: string | undefined,
  params: Record<string, number> | undefined,
  serverText: (key?: string, fallback?: string) => string,
  t: (k: never, vars?: Record<string, string | number>) => string,
): string {
  const key = code ? BANISTER_REASON_KEYS[code] : undefined
  if (!key) return serverText(undefined, fallback)
  const vars: Record<string, string | number> = {}
  for (const [k, v] of Object.entries(params ?? {})) vars[k] = v
  return t(key as never, vars)
}

export default function PerformanceForecast({ metric }: { metric: PMCMetric }) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const serverText = useServerText()
  const [data, setData] = useState<BanisterFit | null>(null)
  const [error, setError] = useState<string | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  const chart = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    banister(id, metric)
      .then((d) => {
        if (!cancelled) setData(d)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [metric])

  // 表现值用「等价 10K 配速」展示更直观（数值越大越快 → 取负号让曲线向上=越快）
  const chartData = useMemo(() => {
    if (!data?.fitted) return null
    // 后端按 date DESC 返回；ECharts 的 line 按数组顺序连线，
    // 倒序会让折线从最后一个点往第一个点连（锯齿）。这里显式升序。
    const pts = [...(data.points ?? [])].sort((a, b) => a.date.localeCompare(b.date))
    const fc = [...(data.forecast ?? [])].sort((a, b) => a.date.localeCompare(b.date))
    const actual = pts.map((p) => [p.date, 1000 / p.actual] as [string, number])
    const fitted = [...pts.map((p) => [p.date, 1000 / p.predicted] as [string, number])]
    const forecast = fc.map((f) => [f.date, 1000 / f.predicted] as [string, number])
    return { actual, fitted, forecast }
  }, [data])

  useEffect(() => {
    const el = ref.current
    if (!el || !chartData) return
    if (!chart.current) chart.current = echarts.init(el)
    chart.current.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'axis',
          valueFormatter: (v: unknown) => {
            const n = Number(v)
            if (!Number.isFinite(n) || n <= 0) return '—'
            return `${Math.floor(n / 60)}:${String(Math.round(n % 60)).padStart(2, '0')}${paceUnit(unit)}`
          },
        },
        legend: {
          data: [t('forecastActual'), t('forecastFitted'), t('forecastFuture')],
          textStyle: { color: '#cbd5e1' },
          top: 0,
        },
        grid: { left: 56, right: 24, top: 36, bottom: 32 },
        xAxis: { type: 'time', axisLabel: { color: '#94a3b8' } },
        yAxis: {
          type: 'value',
          name: t('forecastYName', { u: paceUnit(unit).slice(1) }),
          nameTextStyle: { color: '#94a3b8', fontSize: 10 },
          axisLabel: {
            color: '#94a3b8',
            formatter: (v: number) => `${Math.floor(v / 60)}:${String(Math.round(v % 60)).padStart(2, '0')}`,
          },
          splitLine: { lineStyle: { color: '#1e293b' } },
          inverse: true, // 配速越小越快 → 反向，让「向上 = 更快」
        },
        series: [
          {
            name: t('forecastActual'),
            type: 'scatter',
            data: chartData.actual,
            symbolSize: 10,
            itemStyle: { color: '#22c55e' },
          },
          {
            name: t('forecastFitted'),
            type: 'line',
            data: chartData.fitted,
            smooth: true,
            symbol: 'none',
            lineStyle: { width: 2, color: '#3b82f6' },
          },
          {
            name: t('forecastFuture'),
            type: 'line',
            data: chartData.forecast,
            smooth: true,
            symbol: 'none',
            lineStyle: { width: 2, color: '#f59e0b', type: 'dashed' },
          },
        ],
      },
      true,
    )
  }, [chartData, unit, t])

  if (error) {
    return (
      <p role="alert" className="mb-6 rounded border border-destructive bg-card p-3 text-sm text-destructive">
        {t('forecastLoadFailed', { error })}
      </p>
    )
  }
  if (!data) return null
  if (!data.fitted) {
    return (
      <section aria-label={t('forecastAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
        <h2 className="mb-2 text-sm font-medium text-muted-foreground">{t('forecastTitle')}</h2>
        <p className="text-xs text-muted-foreground">
          {banisterReason(data.reasonCode, data.reason, data.reasonCodeParams, serverText, t)}
        </p>
      </section>
    )
  }

  return (
    <section aria-label={t('forecastAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
      <div className="mb-1 flex flex-wrap items-baseline gap-2">
        <h2 className="text-sm font-medium text-muted-foreground">{t('forecastTitle')}</h2>
        <span className="text-xs text-muted-foreground">
          {t('forecastCount', { n: data.n ?? 0 })}
        </span>
      </div>

      <details className="mb-3 text-xs text-muted-foreground">
        <summary className="cursor-pointer text-foreground">{t('forecastHow')}</summary>
        <div className="mt-2 space-y-1.5 leading-relaxed">
          <p>
            {t('forecastP1a')}<b>{t('forecastP1Strong')}</b>
            {t('forecastP1StrongTail', { tau: data.tau1?.toFixed(0) ?? '—' })}
            <b>{t('forecastP1Fatigue')}</b>{t('forecastP1FatigueTail', { tau: data.tau2?.toFixed(0) ?? '—' })}
          </p>
          <p className="font-mono text-[11px]">
            {t('forecastFormula')}
          </p>
          <p>
            {t('forecastP2a')}<b>{t('forecastP2Race')}</b>{t('forecastP2Tail')}
          </p>
          <p className="rounded border border-border bg-muted/40 p-2">
            {t('forecastP3a')}<b>{t('forecastP3Bold')}</b>{t('forecastP3Mid')}<b>{t('forecastP3Warn')}</b>
          </p>
        </div>
      </details>

      {data.overfit && (
        <p role="status" className="mb-3 rounded border border-accent bg-card p-2 text-xs text-accent">
          {t('forecastOverfitA', { n: data.n ?? 0 })}<b>{t('forecastOverfitBold')}</b>{t('forecastOverfitTail')}
        </p>
      )}

      <div ref={ref} className="h-64 w-full" role="img" aria-label={t('forecastChartAria')} />

      {/* 各距离预测（5K / 10K / 半马 / 全马） */}
      {data.projections && data.projections.length > 0 && (
        <div className="mt-4 border-t border-border pt-4">
          <div className="mb-2 flex flex-wrap items-baseline gap-2">
            <h3 className="text-xs font-medium text-foreground">{t('forecastProjTitle')}</h3>
            <span className="text-[11px] text-muted-foreground">
              {t('forecastProjBase')}{data.rieger?.alpha.toFixed(3)}
              {data.rieger?.source === 'personal' ? (
                <span className="text-emerald-500">
                  {t('forecastProjPersonal', { n: data.rieger.n })}
                </span>
              ) : (
                <span>{t('forecastProjDefault')}</span>
              )}
            </span>
          </div>

          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
                <th className="py-1 pr-3">{t('forecastColDist')}</th>
                <th className="py-1 pr-3">{t('forecastColTime')}</th>
                <th className="py-1 pr-3">{t('forecastColPace')}</th>
                <th className="py-1">{t('forecastColNote')}</th>
              </tr>
            </thead>
            <tbody>
              {data.projections.map((p) => {
                const secs = p.timeSecs
                const h = Math.floor(secs / 3600)
                const m = Math.floor((secs % 3600) / 60)
                const sec = Math.round(secs % 60)
                return (
                  <tr key={p.label} className="border-b border-border/50">
                    <td className="py-1.5 pr-3 font-medium text-foreground">{serverText(p.labelKey, p.label)}</td>
                    <td className="py-1.5 pr-3 font-mono text-foreground">
                      {h > 0
                        ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
                        : `${m}:${String(sec).padStart(2, '0')}`}
                    </td>
                    <td className="py-1.5 pr-3 font-mono text-muted-foreground">
                      {formatPace(p.paceSecsPerKm, unit)}
                      {paceUnit(unit)}
                    </td>
                    <td className="py-1.5 text-[11px] text-muted-foreground">
                      {p.reliable ? (
                        <span className="text-emerald-500">{t('forecastReliable')}</span>
                      ) : (
                        <span className="text-accent">⚠ {serverText(p.noteCode, p.note)}</span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>

          <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">
            {t('forecastAlgo')}
          </p>
        </div>
      )}
    </section>
  )
}
