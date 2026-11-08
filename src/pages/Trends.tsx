import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import * as echarts from 'echarts'
import { pmc, seasons, type PMCSeries, type Season } from '../api'
import { readActiveAthlete } from '../athlete'
import PerformanceForecast from './PerformanceForecast'
import MarathonPredict from './MarathonPredict'
import { TODAY_STR } from '../clock'

type Metric = 'trimp' | 'rtss'

/** PMC 趋势页（票 10）：LTS/STS/SB/RR 四序列 + 计划/预期叠加 + RAG 色带 + 赛季编辑。 */
export default function Trends() {
  const [metric, setMetric] = useState<Metric>('trimp')
  const [series, setSeries] = useState<PMCSeries | null>(null)
  const [seasonList, setSeasonList] = useState<Season[]>([])
  const [error, setError] = useState<string | null>(null)
  const chartRef = useRef<HTMLDivElement>(null)
  const chart = useRef<echarts.ECharts | null>(null)

  const [sName, setSName] = useState('')
  const [sStart, setSStart] = useState('')
  const [sEnd, setSEnd] = useState('')
  const [sSeed, setSSeed] = useState('')

  const refresh = useCallback(() =>
      // setState 推迟到微任务：effect 调用栈内同步 setState 命中 set-state-in-effect
      Promise.resolve().then(async () => {
      const id = readActiveAthlete()
      if (!id) return
      try {
        const [s, list] = await Promise.all([pmc(id, metric), seasons.list(id)])
        setSeries(s)
        setSeasonList(list)
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载失败')
      }
        }), [metric])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // PMC 图：LTS/STS/SB 折线 + 日负荷柱（右轴）+ 计划虚线/预期点线 + SB 红区 + dataZoom。
  // markArea/dataZoom/双轴在 echarts 6 下曾触发 __ec_inner_57，debt-sprint 票 01 逐项验证加回。
  // 后端按 GoldenCheetah 口径把序列延伸到「今天 + 365 天」；无未来计划时截断到今天，
  // 有则保留（计划/预期线要看得到）。
  const shown = useMemo(() => {
    if (!series) return null
    const dates = series.dates ?? []
    // 未来段有计划课时保留（计划/预期线要看得到），否则截断到今天防 0 值压扁
    const futureHasPlan = dates.some((d, i) => d > TODAY_STR && (series.plannedLts?.[i] ?? 0) > 0)
    let cut = dates.findIndex((x) => x > TODAY_STR)
    if (cut < 0 || futureHasPlan) cut = dates.length
    const slice = <T,>(arr: T[] | undefined) => (arr ?? []).slice(0, cut)
    // 计划线：0（无计划日）转 null 断开，避免从年初贴零画到年底
    const planSlice = (arr?: number[]) => slice(arr).map((v) => (v > 0 ? v : null))
    // 预期线：今天前与实际重合，只画今日起（null 断开）
    const expSlice = (arr?: number[]) =>
      slice(arr).map((v, i) => (dates[i] > TODAY_STR && v > 0 ? v : null))
    return {
      dates: slice(dates),
      lts: slice(series.lts),
      sts: slice(series.sts),
      sb: slice(series.sb),
      stress: slice(series.stress),
      plannedLts: planSlice(series.plannedLts),
      plannedSts: planSlice(series.plannedSts),
      plannedSb: planSlice(series.plannedSb),
      expectedLts: expSlice(series.expectedLts),
      expectedSts: expSlice(series.expectedSts),
      expectedSb: expSlice(series.expectedSb),
      hasFuturePlan: futureHasPlan,
    }
  }, [series])

  useEffect(() => {
    const el = chartRef.current
    if (!el || !shown) return
    if (!chart.current) chart.current = echarts.init(el)
    const n = shown.dates.length
    // 默认视窗锚定今天：左含近 120 天历史，右含计划未来段（若有）——
    // 后端序列延伸到今天+365，锚在尾部会只看见衰减尾巴
    let todayIdx = shown.dates.findIndex((d) => d > TODAY_STR)
    if (todayIdx < 0) todayIdx = n
    const futureTail = shown.hasFuturePlan ? Math.min(n - todayIdx, 21) : 0
    const zStart = Math.max(0, ((todayIdx - 120) / n) * 100)
    const zEnd = Math.min(100, ((todayIdx + futureTail - 1) / n) * 100)
    // SB 红区（GC sbColor：SB < −40 过劳）；数据没探到 -40 以下就不画带
    const minSb = shown.sb.length ? Math.min(...shown.sb) : 0
    const redZone =
      minSb < -40
        ? [
            [
              { yAxis: minSb - 5, itemStyle: { color: 'rgba(239,68,68,0.10)' } },
              { yAxis: -40 },
            ],
          ]
        : []
    chart.current.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: { trigger: 'axis' },
        legend: {
          data: ['LTS 体能', 'STS 疲劳', 'SB 状态', '日负荷', '计划', '预期'],
          textStyle: { color: '#cbd5e1' },
          top: 0,
        },
        grid: { left: 48, right: 52, top: 36, bottom: 56 },
        // 默认视窗近 120 天；inside 滚轮缩放 + slider 范围条
        dataZoom: [
          { type: 'inside', start: zStart, end: zEnd },
          // slider 与 inside 同步（同轴联动），显式给同一视窗
          {
            type: 'slider',
            start: zStart,
            end: zEnd,
            height: 16,
            bottom: 8,
            borderColor: '#1e293b',
            backgroundColor: 'rgba(30,41,59,0.35)',
            fillerColor: 'rgba(59,130,246,0.15)',
            dataBackground: { lineStyle: { color: '#334155' }, areaStyle: { color: 'rgba(51,65,85,0.4)' } },
            textStyle: { color: '#94a3b8', fontSize: 10 },
          },
        ],
        xAxis: { type: 'category', data: shown.dates, axisLabel: { color: '#94a3b8' } },
        // 双轴：PMC 线挂左轴，日负荷柱挂右轴互不压扁
        yAxis: [
          {
            type: 'value',
            axisLabel: { color: '#94a3b8' },
            splitLine: { lineStyle: { color: '#1e293b' } },
          },
          {
            type: 'value',
            axisLabel: { color: '#64748b', fontSize: 10 },
            splitLine: { show: false },
          },
        ],
        series: [
          { name: 'LTS 体能', type: 'line', data: shown.lts, smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#3b82f6' } },
          { name: 'STS 疲劳', type: 'line', data: shown.sts, smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#d97706' } },
          {
            name: 'SB 状态',
            type: 'line',
            data: shown.sb,
            smooth: true,
            symbol: 'none',
            lineStyle: { width: 2, color: '#22c55e' },
            markArea: { silent: true, data: redZone as never },
          },
          { name: '日负荷', type: 'bar', yAxisIndex: 1, data: shown.stress, itemStyle: { color: 'rgba(59,130,246,0.30)' }, barMaxWidth: 5 },
          // 计划（虚线）/预期（点线）：同名 series 由 legend 一项统一开关
          { name: '计划', type: 'line', data: shown.plannedLts, symbol: 'none', lineStyle: { width: 1.5, color: '#3b82f6', type: 'dashed' }, itemStyle: { color: '#3b82f6' } },
          { name: '计划', type: 'line', data: shown.plannedSts, symbol: 'none', lineStyle: { width: 1.5, color: '#d97706', type: 'dashed' }, itemStyle: { color: '#d97706' } },
          { name: '计划', type: 'line', data: shown.plannedSb, symbol: 'none', lineStyle: { width: 1.5, color: '#22c55e', type: 'dashed' }, itemStyle: { color: '#22c55e' } },
          { name: '预期', type: 'line', data: shown.expectedLts, symbol: 'none', lineStyle: { width: 1.5, color: '#3b82f6', type: 'dotted' }, itemStyle: { color: '#3b82f6' } },
          { name: '预期', type: 'line', data: shown.expectedSts, symbol: 'none', lineStyle: { width: 1.5, color: '#d97706', type: 'dotted' }, itemStyle: { color: '#d97706' } },
          { name: '预期', type: 'line', data: shown.expectedSb, symbol: 'none', lineStyle: { width: 1.5, color: '#22c55e', type: 'dotted' }, itemStyle: { color: '#22c55e' } },
        ],
      },
      true,
    )
  }, [shown])

  useEffect(() => {
    const onResize = () => chart.current?.resize()
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      chart.current?.dispose()
      chart.current = null
    }
  }, [])

  async function addSeason() {
    const id = readActiveAthlete()
    if (!id || !sName.trim() || !sStart || !sEnd) return
    setError(null)
    try {
      await seasons.upsert(id, {
        name: sName.trim(),
        startDate: sStart,
        endDate: sEnd,
        seed: Number(sSeed) || 0,
      })
      setSName('')
      setSSeed('')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function removeSeason(sid: number | undefined) {
    if (!sid) return
    const id = readActiveAthlete()
    if (!id) return
    try {
      await seasons.remove(id, sid)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <div className="mb-4 flex items-center gap-4">
        <h1 className="text-2xl font-semibold">训练趋势（PMC）</h1>
        <div className="ml-auto flex gap-2 text-sm">
          {(['trimp', 'rtss'] as Metric[]).map((m) => (
            <button
              key={m}
              onClick={() => setMetric(m)}
              className={`cursor-pointer rounded-full px-3 py-1 ${
                metric === m ? 'bg-secondary text-secondary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
              }`}
            >
              {m === 'trimp' ? 'TRIMP' : 'rTSS'}
            </button>
          ))}
        </div>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {series && (series.stress ?? []).every((v) => v === 0) && (
        <p role="status" className="mb-4 rounded border border-accent bg-card p-3 text-sm text-accent">
          ⚠️ PMC 需要每日训练负荷，但当前没有指标数据（负荷全为 0）。
          请先到主页配置<b>心率区间</b>（TRIMP）或<b>阈值配速</b>（rTSS）—— 保存后会自动重算历史活动。
        </p>
      )}
      <details
        className="mb-4 rounded-lg border border-border bg-card p-4 text-sm"
        aria-label="图例说明"
      >
        <summary className="cursor-pointer text-sm font-medium text-foreground">
          怎么看这张图（点这里展开）
        </summary>

        <div className="mt-3 space-y-4 text-xs leading-relaxed text-muted-foreground">
          <div>
            <p className="mb-1.5 font-medium text-foreground">三条线在说同一件事的三个侧面</p>
            <ul className="space-y-1.5">
              <li>
                <span className="font-medium text-secondary">蓝线 LTS（体能）</span>
                ：过去 <b>42 天</b>训练负荷的加权平均。涨得慢、掉得也慢，代表你的基础耐力。
                一次大课不会让它飙起来，停练一周也不会塌。
              </li>
              <li>
                <span className="font-medium text-accent">橙线 STS（疲劳）</span>
                ：过去 <b>7 天</b>的加权平均。涨得快、掉得也快，代表「最近有多累」。
                昨天跑了个长距离，它今天就会明显上升。
              </li>
              <li>
                <span className="font-medium text-emerald-500">绿线 SB（状态）= LTS − STS</span>
                ：你还有多少余力。这是真正用来指导今天练什么的线。
              </li>
            </ul>
            <p className="mt-1.5">
              浅蓝柱 = 每天的训练负荷（TRIMP 或 rTSS，右上角可切换口径）。
            </p>
          </div>

          <div>
            <p className="mb-1.5 font-medium text-foreground">绿线（SB）怎么读</p>
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border text-left">
                    <th className="py-1 pr-3">SB 值</th>
                    <th className="py-1 pr-3">身体状态</th>
                    <th className="py-1">建议</th>
                  </tr>
                </thead>
                <tbody>
                  <tr className="border-b border-border/50">
                    <td className="py-1 pr-3 font-mono text-emerald-500">&gt; +10</td>
                    <td className="py-1 pr-3">休息充分，有余力</td>
                    <td className="py-1">适合比赛、测验、冲 PB</td>
                  </tr>
                  <tr className="border-b border-border/50">
                    <td className="py-1 pr-3 font-mono">−10 ~ +10</td>
                    <td className="py-1 pr-3">正常训练区</td>
                    <td className="py-1">按计划练，这也是训练日的大部分时间</td>
                  </tr>
                  <tr className="border-b border-border/50">
                    <td className="py-1 pr-3 font-mono text-accent">−30 ~ −10</td>
                    <td className="py-1 pr-3">疲劳在累积</td>
                    <td className="py-1">别再叠加高强度，注意睡眠和补给</td>
                  </tr>
                  <tr>
                    <td className="py-1 pr-3 font-mono text-destructive">&lt; −30</td>
                    <td className="py-1 pr-3">过度疲劳</td>
                    <td className="py-1">减量或完全休息，继续练容易受伤</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div>
            <p className="mb-1.5 font-medium text-foreground">典型形态</p>
            <ul className="space-y-1">
              <li>· <b>LTS 稳步爬升</b> → 训练在系统性地积累，这是最健康的状态</li>
              <li>· <b>LTS 长时间走平</b> → 平台期，训练量没增加</li>
              <li>· <b>STS 出现尖峰</b> → 一次大强度课或长距离</li>
              <li>· <b>SB 挖坑后回升</b> → 正常的「负荷 → 恢复」循环</li>
              <li>· <b>LTS 持续下降</b> → 停练或量太少，体能正在流失</li>
            </ul>
          </div>

          <p className="rounded border border-border bg-muted/40 p-2">
            注意：这三条线是<b>相对值，没有绝对好坏</b>，只跟你自己的历史比。
            LTS 从 30 涨到 45 说明你在进步；别人的 LTS 是 90 跟你无关（训练年限和量级不同）。
          </p>
        </div>
      </details>

      <section aria-label="PMC 图表" className="mb-6 rounded-lg border border-border bg-card p-4">
        {series ? (
          <div ref={chartRef} className="h-96 w-full" role="img" aria-label="PMC 趋势图" />
        ) : (
          <div className="grid h-96 place-items-center text-muted-foreground">加载中…</div>
        )}
        <p className="mt-2 text-xs text-muted-foreground">
          LTS=42 天体能 · STS=7 天疲劳 · SB=体能−疲劳（红区 SB&lt;−40 过劳）· 虚线=计划 · 点线=预期（今日起）· 滚轮/滑条缩放
        </p>
      </section>

      <PerformanceForecast metric={metric} />
      <MarathonPredict />

      <section aria-label="赛季" className="rounded-lg border border-border bg-card p-4">
        <h2 className="mb-2 text-sm font-medium text-muted-foreground">赛季（给 PMC 设定起点）</h2>
        <details className="mb-3 text-xs text-muted-foreground">
          <summary className="cursor-pointer text-foreground">这个功能是干什么的？</summary>
          <div className="mt-2 space-y-1.5 leading-relaxed">
            <p>
              PMC 是「滚雪球」式的递归计算：<span className="font-mono">今天的体能 = 昨天的体能 × 衰减 + 今天的负荷</span>。
              所以它必须有一个<b>起点值</b>，否则只能从 0 开始。
            </p>
            <p>
              从 0 开始的问题是：前 42 天的曲线会明显偏低 —— 算法不知道你在这之前练了多少。
              一条从 0 慢慢爬升的曲线，看起来像「刚开始训练的新手」，但其实你可能已经练了好几年。
            </p>
            <p className="font-medium text-foreground">什么时候需要填：</p>
            <ul className="space-y-1 pl-4">
              <li>· <b>换了设备/账号，从某天开始有数据</b> → 填你当时的估计体能（长期规律训练的人大约 50–80）</li>
              <li>· <b>受伤/长时间断档后重新开始</b> → 填一个偏低的值（20–40），避免曲线虚高</li>
              <li>· <b>想把训练按赛季分段</b> → 给每个赛季起点设一个值，各自独立演进</li>
            </ul>
            <p className="rounded border border-border bg-muted/40 p-2">
              你的数据从 2026-02 起连续、没有断档，<b>不需要配赛季</b> —— 留空即可，
              曲线会从 0 自然爬到你现在的实际水平（现在 LTS 约 60–80，说明已经累积起来了）。
            </p>
          </div>
        </details>
        {seasonList.length > 0 && (
          <ul className="mb-3 space-y-1 text-sm">
            {seasonList.map((s) => (
              <li key={s.id} className="flex items-center gap-3">
                <span className="font-medium">{s.name}</span>
                <span className="font-mono text-xs text-muted-foreground">
                  {s.startDate} ~ {s.endDate} · seed {s.seed || '—'}
                </span>
                <button
                  onClick={() => removeSeason(s.id)}
                  className="ml-auto cursor-pointer text-xs text-muted-foreground hover:text-destructive"
                >
                  删除
                </button>
              </li>
            ))}
          </ul>
        )}
        <div className="flex flex-wrap items-end gap-2 text-sm">
          <input
            value={sName}
            onChange={(e) => setSName(e.target.value)}
            placeholder="赛季名称"
            className="w-32 rounded border border-border bg-muted px-2 py-1"
          />
          <input type="date" value={sStart} onChange={(e) => setSStart(e.target.value)} className="rounded border border-border bg-muted px-2 py-1" />
          <input type="date" value={sEnd} onChange={(e) => setSEnd(e.target.value)} className="rounded border border-border bg-muted px-2 py-1" />
          <input
            value={sSeed}
            onChange={(e) => setSSeed(e.target.value)}
            placeholder="播种值(0=无)"
            className="w-28 rounded border border-border bg-muted px-2 py-1 text-center font-mono"
          />
          <button
            onClick={addSeason}
            disabled={!sName.trim() || !sStart || !sEnd}
            className="cursor-pointer rounded bg-primary px-4 py-1.5 font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
          >
            保存赛季
          </button>
        </div>
      </section>
    </main>
  )
}
