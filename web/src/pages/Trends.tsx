import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import * as echarts from 'echarts'
import { pmc, seasons, type PMCMetric, type PMCSeries, type Season } from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale } from '../i18n'
import PerformanceForecast from './PerformanceForecast'
import MarathonPredict from './MarathonPredict'
import { TODAY_STR } from '../clock'
import { GT } from '../glossary-term'

type Metric = PMCMetric

/** PMC 趋势页（票 10）：LTS/STS/SB/RR 四序列 + 计划/预期叠加 + RAG 色带 + 赛季编辑。 */
export default function Trends() {
  const { locale, t } = useLocale()
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
        setError(err instanceof Error ? err.message : t('trendsLoadFailed'))
      }
        }), [metric, t])

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
          data: [t('trendsLtsName'), t('trendsStsName'), t('trendsSbName'), t('trendsDailyName'), t('trendsPlanName'), t('trendsExpName')],
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
          { name: t('trendsLtsName'), type: 'line', data: shown.lts, smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#3b82f6' } },
          { name: t('trendsStsName'), type: 'line', data: shown.sts, smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#d97706' } },
          {
            name: t('trendsSbName'),
            type: 'line',
            data: shown.sb,
            smooth: true,
            symbol: 'none',
            lineStyle: { width: 2, color: '#22c55e' },
            markArea: { silent: true, data: redZone as never },
          },
          { name: t('trendsDailyName'), type: 'bar', yAxisIndex: 1, data: shown.stress, itemStyle: { color: 'rgba(59,130,246,0.30)' }, barMaxWidth: 5 },
          // 计划（虚线）/预期（点线）：同名 series 由 legend 一项统一开关
          { name: t('trendsPlanName'), type: 'line', data: shown.plannedLts, symbol: 'none', lineStyle: { width: 1.5, color: '#3b82f6', type: 'dashed' }, itemStyle: { color: '#3b82f6' } },
          { name: t('trendsPlanName'), type: 'line', data: shown.plannedSts, symbol: 'none', lineStyle: { width: 1.5, color: '#d97706', type: 'dashed' }, itemStyle: { color: '#d97706' } },
          { name: t('trendsPlanName'), type: 'line', data: shown.plannedSb, symbol: 'none', lineStyle: { width: 1.5, color: '#22c55e', type: 'dashed' }, itemStyle: { color: '#22c55e' } },
          { name: t('trendsExpName'), type: 'line', data: shown.expectedLts, symbol: 'none', lineStyle: { width: 1.5, color: '#3b82f6', type: 'dotted' }, itemStyle: { color: '#3b82f6' } },
          { name: t('trendsExpName'), type: 'line', data: shown.expectedSts, symbol: 'none', lineStyle: { width: 1.5, color: '#d97706', type: 'dotted' }, itemStyle: { color: '#d97706' } },
          { name: t('trendsExpName'), type: 'line', data: shown.expectedSb, symbol: 'none', lineStyle: { width: 1.5, color: '#22c55e', type: 'dotted' }, itemStyle: { color: '#22c55e' } },
        ],
      },
      true,
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shown, locale])

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
      setError(err instanceof Error ? err.message : t('trendsSaveFailed'))
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
      setError(err instanceof Error ? err.message : t('trendsDeleteFailed'))
    }
  }

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <div className="mb-4 flex items-center gap-4">
        <h1 className="text-2xl font-semibold"><GT text={t('trendsTitle')} /></h1>
        <div className="ml-auto flex gap-2 text-sm">
          {(['trimp', 'rtss', 'power'] as Metric[]).map((m) => (
            <button
              key={m}
              onClick={() => setMetric(m)}
              className={`cursor-pointer rounded-full px-3 py-1 ${
                metric === m ? 'bg-secondary text-secondary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
              }`}
            >
              {m === 'trimp' ? 'TRIMP' : m === 'rtss' ? 'rTSS' : t('trendsMetricPower')}
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
          <GT text={t('trendsNoLoad')} /> <b>{t('trendsNoLoadB1')}</b><GT text={t('trendsNoLoadMid')} /><b><GT text={t('trendsNoLoadB2')} /></b>
          <GT text={t('trendsNoLoadTail')} />
        </p>
      )}
      <details
        className="mb-4 rounded-lg border border-border bg-card p-4 text-sm"
        aria-label={t('trendsHowAria')}
      >
        <summary className="cursor-pointer text-sm font-medium text-foreground">
          {t('trendsHowSummary')}
        </summary>

        <div className="mt-3 space-y-4 text-xs leading-relaxed text-muted-foreground">
          <div>
            <p className="mb-1.5 font-medium text-foreground">{t('trendsHowH1')}</p>
            <ul className="space-y-1.5">
              <li>
                <span className="font-medium text-secondary"><GT text={t('trendsLineLts')} /></span>
                <GT text={t('trendsLineLtsText', { b: t('trendsLineLtsBold') })} />
              </li>
              <li>
                <span className="font-medium text-accent"><GT text={t('trendsLineSts')} /></span>
                {t('trendsLineStsText', { b: t('trendsLineStsBold') })}
              </li>
              <li>
                <span className="font-medium text-emerald-500">{t('trendsLineSb')}</span>
                {t('trendsLineSbText')}
              </li>
            </ul>
            <p className="mt-1.5">
              <GT text={t('trendsBarNote')} />
            </p>
          </div>

          <div>
            <p className="mb-1.5 font-medium text-foreground">{t('trendsSbH2')}</p>
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border text-left">
                    <th className="py-1 pr-3">{t('trendsSbColValue')}</th>
                    <th className="py-1 pr-3">{t('trendsSbColState')}</th>
                    <th className="py-1">{t('trendsSbColAdvice')}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr className="border-b border-border/50">
                    <td className="py-1 pr-3 font-mono text-emerald-500">&gt; +10</td>
                    <td className="py-1 pr-3">{t('trendsSbR1State')}</td>
                    <td className="py-1"><GT text={t('trendsSbR1Advice')} /></td>
                  </tr>
                  <tr className="border-b border-border/50">
                    <td className="py-1 pr-3 font-mono">−10 ~ +10</td>
                    <td className="py-1 pr-3">{t('trendsSbR2State')}</td>
                    <td className="py-1">{t('trendsSbR2Advice')}</td>
                  </tr>
                  <tr className="border-b border-border/50">
                    <td className="py-1 pr-3 font-mono text-accent">−30 ~ −10</td>
                    <td className="py-1 pr-3"><GT text={t('trendsSbR3State')} /></td>
                    <td className="py-1">{t('trendsSbR3Advice')}</td>
                  </tr>
                  <tr>
                    <td className="py-1 pr-3 font-mono text-destructive">&lt; −30</td>
                    <td className="py-1 pr-3"><GT text={t('trendsSbR4State')} /></td>
                    <td className="py-1">{t('trendsSbR4Advice')}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div>
            <p className="mb-1.5 font-medium text-foreground"><GT text={t('trendsPatternsH')} /></p>
            <ul className="space-y-1">
              <li>· <b>{t('trendsPattern1')}</b>{t('trendsPattern1Tail')}</li>
              <li>· <b>{t('trendsPattern2')}</b>{t('trendsPattern2Tail')}</li>
              <li>· <b>{t('trendsPattern3')}</b>{t('trendsPattern3Tail')}</li>
              <li>· <b>{t('trendsPattern4')}</b>{t('trendsPattern4Tail')}</li>
              <li>· <b>{t('trendsPattern5')}</b><GT text={t('trendsPattern5Tail')} /></li>
            </ul>
          </div>

          <p className="rounded border border-border bg-muted/40 p-2">
            {t('trendsRelNoteA')} <b>{t('trendsRelNoteB')}</b>{t('trendsRelNoteTail')}
          </p>
        </div>
      </details>

      <section aria-label={t('trendsChartAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
        {series ? (
          <div ref={chartRef} className="h-96 w-full" role="img" aria-label={t('trendsChartImgAria')} />
        ) : (
          <div className="grid h-96 place-items-center text-muted-foreground">{t('trendsLoading')}</div>
        )}
        <p className="mt-2 text-xs text-muted-foreground">
          <GT text={t('trendsLegendGloss')} />
        </p>
        <p className="mt-1 text-xs text-muted-foreground">
          <GT text={t('trendsChartFooter')} />
        </p>
      </section>

      <PerformanceForecast metric={metric} />
      <MarathonPredict />

      <section aria-label={t('trendsSeasonAria')} className="rounded-lg border border-border bg-card p-4">
        <h2 className="mb-2 text-sm font-medium text-muted-foreground"><GT text={t('trendsSeasonTitle')} /></h2>
        <details className="mb-3 text-xs text-muted-foreground">
          <summary className="cursor-pointer text-foreground">{t('trendsSeasonWhat')}</summary>
          <div className="mt-2 space-y-1.5 leading-relaxed">
            <p>
              <GT text={t('trendsSeasonP1a')} /><span className="font-mono"><GT text={t('trendsSeasonP1Formula')} /></span>
              {t('trendsSeasonP1b')}<b>{t('trendsSeasonP1Bold')}</b>{t('trendsSeasonP1Tail')}
            </p>
            <p>
              {t('trendsSeasonP2')}
            </p>
            <p className="font-medium text-foreground">{t('trendsSeasonWhen')}</p>
            <ul className="space-y-1 pl-4">
              <li>· <b>{t('trendsSeasonLi1a')}</b><GT text={t('trendsSeasonLi1b')} /></li>
              <li>· <b>{t('trendsSeasonLi2a')}</b>{t('trendsSeasonLi2b')}</li>
              <li>· <b>{t('trendsSeasonLi3a')}</b>{t('trendsSeasonLi3b')}</li>
            </ul>
            <p className="rounded border border-border bg-muted/40 p-2">
              {t('trendsSeasonNoteA')} <b>{t('trendsSeasonNoteB')}</b>{t('trendsSeasonNoteTail')}
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
                  {t('trendsSeasonDelete')}
                </button>
              </li>
            ))}
          </ul>
        )}
        <div className="flex flex-wrap items-end gap-2 text-sm">
          <input
            value={sName}
            onChange={(e) => setSName(e.target.value)}
            placeholder={t('trendsSeasonNamePh')}
            className="w-32 rounded border border-border bg-muted px-2 py-1"
          />
          <input type="date" value={sStart} onChange={(e) => setSStart(e.target.value)} className="rounded border border-border bg-muted px-2 py-1" />
          <input type="date" value={sEnd} onChange={(e) => setSEnd(e.target.value)} className="rounded border border-border bg-muted px-2 py-1" />
          <input
            value={sSeed}
            onChange={(e) => setSSeed(e.target.value)}
            placeholder={t('trendsSeasonSeedPh')}
            className="w-28 rounded border border-border bg-muted px-2 py-1 text-center font-mono"
          />
          <button
            onClick={addSeason}
            disabled={!sName.trim() || !sStart || !sEnd}
            className="cursor-pointer rounded bg-primary px-4 py-1.5 font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
          >
            {t('trendsSeasonSave')}
          </button>
        </div>
      </section>
    </main>
  )
}
