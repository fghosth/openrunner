import { useEffect, useState } from 'react'
import { coach, type CoachActivityAnalysis } from '../api'
import { readActiveAthlete } from '../athlete'

/** 单课耐久性分析块（coach-engine 票 06）：Pa:Hr 解耦 / EF / 心率漂移。
 *  三态：有值 / 不适用（含原因）/ 加载失败静默隐藏。 */

const VERDICT: Record<string, { cls: string; text: string }> = {
  ok: { cls: 'text-emerald-500', text: '良好' },
  moderate: { cls: 'text-amber-500', text: '中等' },
  large: { cls: 'text-red-500', text: '偏大' },
  high: { cls: 'text-amber-500', text: '偏大' },
}

export default function CoachActivityAnalysisBlock({ activityId }: { activityId: number }) {
  const [data, setData] = useState<CoachActivityAnalysis | null>(null)

  useEffect(() => {
    const athleteId = readActiveAthlete()
    if (!athleteId) return
    let cancelled = false
    coach
      .analysis(athleteId, activityId)
      .then((d) => !cancelled && setData(d))
      .catch(() => !cancelled && setData(null))
    return () => {
      cancelled = true
    }
  }, [activityId])

  if (!data) return null
  if (!data.applicable) {
    return (
      <section aria-label="单课分析" className="mt-4 rounded-lg border border-border bg-card p-4">
        <h2 className="mb-1 text-sm font-medium text-muted-foreground">单课分析（Pa:Hr / EF / 漂移）</h2>
        <p className="text-xs text-muted-foreground">不适用：{data.reason}</p>
      </section>
    )
  }

  const dv = data.decouplingVerdict ? VERDICT[data.decouplingVerdict] : null
  const hv = data.hrDriftVerdict ? VERDICT[data.hrDriftVerdict] : null
  const cell = (label: string, value: string, tip: string, verdict?: { cls: string; text: string } | null) => (
    <div className="flex-1 min-w-36 rounded border border-border/60 p-2" title={tip}>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="font-mono text-lg">
        {value}
        {verdict && <span className={`ml-1.5 text-xs ${verdict.cls}`}>{verdict.text}</span>}
      </div>
    </div>
  )

  return (
    <section aria-label="单课分析" className="mt-4 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-2 text-sm font-medium text-muted-foreground">单课分析（Pa:Hr / EF / 漂移）</h2>
      <div className="flex flex-wrap gap-2">
        {cell(
          'Pa:Hr 解耦',
          data.decouplingPct !== undefined ? `${data.decouplingPct.toFixed(1)}%` : '—',
          `后半 vs 前半的「速度/心率」比变化（剔除前 10 分钟热身）。<5% 良好，5–10% 中等，>10% 提示有氧耐力不足。出处 S21（Friel/TrainingPeaks 口径）。前半均值 ${Math.round(data.firstHalfHr ?? 0)} → 后半 ${Math.round(data.secondHalfHr ?? 0)} bpm`,
          dv,
        )}
        {cell(
          '效率因子 EF',
          data.efPerBpm ? data.efPerBpm.toFixed(2) : '—',
          '平均速度（米/分）÷ 平均心率。同配速下 EF 升高 = 适应。与自己同类课纵向比较，不与他人横比。',
        )}
        {cell(
          '心率漂移',
          data.hrDriftPct !== undefined ? `${data.hrDriftPct.toFixed(1)}%` : '—',
          `稳态前 15 分钟 vs 末 15 分钟心率变化。<5% 佳（S50）。${Math.round(data.earlyHr ?? 0)} → ${Math.round(data.lateHr ?? 0)} bpm。偏高常见于高温、脱水或后程疲劳。`,
          hv,
        )}
      </div>
      <p className="mt-2 text-[10px] text-muted-foreground">
        适用条件：≥60 分钟且含心率+速度双流；数值与 running-coach 金标准对照（make coach-check）。
      </p>
    </section>
  )
}
