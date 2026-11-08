import { useEffect, useMemo, useRef, useState } from 'react'
import * as echarts from 'echarts'
import { banister, type BanisterFit } from '../api'

import { readActiveAthlete } from '../App'
import { formatPace, paceUnit, useUnit } from '../units'

/** Banister 表现预测：历史成绩散点 + 模型拟合 + 未来 30 天外推。 */
export default function PerformanceForecast({ metric }: { metric: 'trimp' | 'rtss' }) {
  const { unit } = useUnit()
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
          data: ['实际成绩', '模型拟合', '未来预测'],
          textStyle: { color: '#cbd5e1' },
          top: 0,
        },
        grid: { left: 56, right: 24, top: 36, bottom: 32 },
        xAxis: { type: 'time', axisLabel: { color: '#94a3b8' } },
        yAxis: {
          type: 'value',
          name: `等价 10K 配速 (${paceUnit(unit).slice(1)})`,
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
            name: '实际成绩',
            type: 'scatter',
            data: chartData.actual,
            symbolSize: 10,
            itemStyle: { color: '#22c55e' },
          },
          {
            name: '模型拟合',
            type: 'line',
            data: chartData.fitted,
            smooth: true,
            symbol: 'none',
            lineStyle: { width: 2, color: '#3b82f6' },
          },
          {
            name: '未来预测',
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
  }, [chartData, unit])

  if (error) {
    return (
      <p role="alert" className="mb-6 rounded border border-destructive bg-card p-3 text-sm text-destructive">
        表现预测加载失败：{error}
      </p>
    )
  }
  if (!data) return null
  if (!data.fitted) {
    return (
      <section aria-label="表现预测" className="mb-6 rounded-lg border border-border bg-card p-4">
        <h2 className="mb-2 text-sm font-medium text-muted-foreground">表现预测（Banister 模型）</h2>
        <p className="text-xs text-muted-foreground">{data.reason}</p>
      </section>
    )
  }

  return (
    <section aria-label="表现预测" className="mb-6 rounded-lg border border-border bg-card p-4">
      <div className="mb-1 flex flex-wrap items-baseline gap-2">
        <h2 className="text-sm font-medium text-muted-foreground">表现预测（Banister 模型）</h2>
        <span className="text-xs text-muted-foreground">
          {data.n} 条成绩 · 参考距离 10 km
        </span>
      </div>

      <details className="mb-3 text-xs text-muted-foreground">
        <summary className="cursor-pointer text-foreground">这个模型是怎么算的？</summary>
        <div className="mt-2 space-y-1.5 leading-relaxed">
          <p>
            训练同时产生<b>变强</b>（正效应，衰减慢，τ₁={data.tau1?.toFixed(0)} 天）和{' '}
            <b>疲劳</b>（负效应，衰减快，τ₂={data.tau2?.toFixed(0)} 天）：
          </p>
          <p className="font-mono text-[11px]">
            表现(t) = P₀ + k₁·Σ wᵢ·e^(−(t−i)/τ₁) − k₂·Σ wᵢ·e^(−(t−i)/τ₂)
          </p>
          <p>
            用你的<b>比赛成绩</b>做观测点反推 P₀/k₁/k₂。不同距离的成绩先用 Riegel 公式
            （指数 1.06）换算成「等价 10K 配速」才能互相比较。
          </p>
          <p className="rounded border border-border bg-muted/40 p-2">
            虚线是<b>假设未来不训练</b>的外推 —— 它只反映疲劳消退，不代表你会变快。
            <b>这是模型外推，不是承诺。</b>
          </p>
        </div>
      </details>

      {data.overfit && (
        <p role="status" className="mb-3 rounded border border-accent bg-card p-2 text-xs text-accent">
          ⚠️ 只有 {data.n} 条成绩，而模型有 3 个参数 —— 拟合必然穿过每个点，
          R²≈1 是过拟合，<b>不能</b>据此判断模型好坏。建议再录 2–3 条不同日期的成绩。
        </p>
      )}

      <div ref={ref} className="h-64 w-full" role="img" aria-label="表现预测曲线" />

      {/* 各距离预测（5K / 10K / 半马 / 全马） */}
      {data.projections && data.projections.length > 0 && (
        <div className="mt-4 border-t border-border pt-4">
          <div className="mb-2 flex flex-wrap items-baseline gap-2">
            <h3 className="text-xs font-medium text-foreground">如果今天比赛，各距离的成绩预测</h3>
            <span className="text-[11px] text-muted-foreground">
              基准：模型给出的「今天」等价 10K 表现 · 距离指数 α=
              {data.rieger?.alpha.toFixed(3)}
              {data.rieger?.source === 'personal' ? (
                <span className="text-emerald-500">
                  （用你自己的 {data.rieger.n} 条成绩拟合）
                </span>
              ) : (
                <span>（Riegel 默认值——多录几条不同距离的成绩可显著提高精度）</span>
              )}
            </span>
          </div>

          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
                <th className="py-1 pr-3">距离</th>
                <th className="py-1 pr-3">预测成绩</th>
                <th className="py-1 pr-3">配速</th>
                <th className="py-1">说明</th>
              </tr>
            </thead>
            <tbody>
              {data.projections.map((p) => {
                const t = p.timeSecs
                const h = Math.floor(t / 3600)
                const m = Math.floor((t % 3600) / 60)
                const sec = Math.round(t % 60)
                return (
                  <tr key={p.label} className="border-b border-border/50">
                    <td className="py-1.5 pr-3 font-medium text-foreground">{p.label}</td>
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
                        <span className="text-emerald-500">可信</span>
                      ) : (
                        <span className="text-accent">⚠ {p.note}</span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>

          <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">
            算法：Banister 模型给出「今天」的等价 10K 表现，再用幂律
            <span className="font-mono"> t = t₁₀ₖ · (d/10km)^α</span> 外推到各距离。
            α 优先用你自己的多距离成绩做 log-log 回归（业余跑者通常 1.08–1.15，
            比 Riegel 教科书的 1.06 掉速更快）；成绩不足时退回 1.06 并标注。
            全马预测还必须假设你有足够的专项长距离训练，否则真实成绩通常比预测慢 5–15%。
          </p>
        </div>
      )}
    </section>
  )
}
