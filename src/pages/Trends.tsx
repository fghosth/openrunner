import { useCallback, useEffect, useRef, useState } from 'react'
import * as echarts from 'echarts'
import { pmc, seasons, type PMCSeries, type Season } from '../api'
import { readActiveAthlete } from '../App'

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

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) return
    try {
      const [s, list] = await Promise.all([pmc(id, metric), seasons.list(id)])
      setSeries(s)
      setSeasonList(list)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    }
  }, [metric])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // 渲染/更新图表（最小可用：LTS/STS/SB 折线 + 日负荷柱；不含 markArea/dataZoom/双轴，
  // 那些在 echarts 6 下曾触发内部异常 __ec_inner_57 —— 后续逐步验证再加回）
  useEffect(() => {
    const el = chartRef.current
    if (!el || !series) return
    if (!chart.current) chart.current = echarts.init(el)
    chart.current.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: { trigger: 'axis' },
        legend: { data: ['LTS 体能', 'STS 疲劳', 'SB 状态'], textStyle: { color: '#cbd5e1' }, top: 0 },
        grid: { left: 48, right: 48, top: 36, bottom: 28 },
        xAxis: { type: 'category', data: series.dates ?? [], axisLabel: { color: '#94a3b8' } },
        yAxis: {
          type: 'value',
          axisLabel: { color: '#94a3b8' },
          splitLine: { lineStyle: { color: '#1e293b' } },
        },
        series: [
          { name: 'LTS 体能', type: 'line', data: series.lts ?? [], smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#3b82f6' } },
          { name: 'STS 疲劳', type: 'line', data: series.sts ?? [], smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#d97706' } },
          { name: 'SB 状态', type: 'line', data: series.sb ?? [], smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#22c55e' } },
          { name: '日负荷', type: 'bar', data: series.stress ?? [], itemStyle: { color: 'rgba(59,130,246,0.30)' }, barMaxWidth: 5 },
        ],
      },
      true,
    )
  }, [series])

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
      <section aria-label="PMC 图表" className="mb-6 rounded-lg border border-border bg-card p-4">
        {series ? (
          <div ref={chartRef} className="h-96 w-full" role="img" aria-label="PMC 趋势图" />
        ) : (
          <div className="grid h-96 place-items-center text-muted-foreground">加载中…</div>
        )}
        <p className="mt-2 text-xs text-muted-foreground">
          LTS=42 天体能 · STS=7 天疲劳 · SB=体能−疲劳（红区=过劳风险）· RR=周增速 · 虚线=计划 · 点线=预期（今日前实际+今日起计划）
        </p>
      </section>

      <section aria-label="赛季" className="rounded-lg border border-border bg-card p-4">
        <h2 className="mb-3 text-sm font-medium text-muted-foreground">赛季（播种 LTS/STS 初值）</h2>
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
