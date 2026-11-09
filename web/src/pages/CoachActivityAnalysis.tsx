import { useEffect, useState } from 'react'
import { coach, type CoachActivityAnalysis } from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale, useServerText, type TKey } from '../i18n'
import { analysisReason } from '../analysis-text'
import { GT } from '../glossary-term'

/** 单课耐久性分析块（coach-engine 票 06）：Pa:Hr 解耦 / EF / 心率漂移。
 *  三态：有值 / 不适用（含原因）/ 加载失败静默隐藏。 */

const VERDICT: Record<string, { cls: string; textKey: TKey }> = {
  ok: { cls: 'text-emerald-500', textKey: 'coachVerdictOk' },
  moderate: { cls: 'text-amber-500', textKey: 'coachVerdictModerate' },
  large: { cls: 'text-red-500', textKey: 'coachVerdictLarge' },
  high: { cls: 'text-amber-500', textKey: 'coachVerdictLarge' },
}

export default function CoachActivityAnalysisBlock({ activityId }: { activityId: number }) {
  const { t } = useLocale()
  const serverText = useServerText()
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
      <section aria-label={t('coachAnalysisAria')} className="mt-4 rounded-lg border border-border bg-card p-4">
        <h2 className="mb-1 text-sm font-medium text-muted-foreground"><GT text={t('coachAnalysisTitle')} /></h2>
        <p className="text-xs text-muted-foreground">{t('coachAnalysisNotApplicable', { reason: analysisReason(data.reasonCode, data.reason, serverText) })}</p>
      </section>
    )
  }

  const dv = data.decouplingVerdict ? VERDICT[data.decouplingVerdict] : null
  const hv = data.hrDriftVerdict ? VERDICT[data.hrDriftVerdict] : null
  const cell = (label: string, value: string, tip: string, verdict?: { cls: string; textKey: TKey } | null) => (
    <div className="flex-1 min-w-36 rounded border border-border/60 p-2" title={tip}>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="font-mono text-lg">
        {value}
        {verdict && <span className={`ml-1.5 text-xs ${verdict.cls}`}>{t(verdict.textKey)}</span>}
      </div>
    </div>
  )

  return (
    <section aria-label={t('coachAnalysisAria')} className="mt-4 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-2 text-sm font-medium text-muted-foreground"><GT text={t('coachAnalysisTitle')} /></h2>
      <div className="flex flex-wrap gap-2">
        {cell(
          t('coachAnalysisPaHr'),
          data.decouplingPct !== undefined ? `${data.decouplingPct.toFixed(1)}%` : '—',
          t('coachAnalysisPaHrTip', { first: Math.round(data.firstHalfHr ?? 0), second: Math.round(data.secondHalfHr ?? 0) }),
          dv,
        )}
        {cell(
          t('coachAnalysisEf'),
          data.efPerBpm ? data.efPerBpm.toFixed(2) : '—',
          t('coachAnalysisEfTip'),
        )}
        {cell(
          t('coachAnalysisDrift'),
          data.hrDriftPct !== undefined ? `${data.hrDriftPct.toFixed(1)}%` : '—',
          t('coachAnalysisDriftTip', { early: Math.round(data.earlyHr ?? 0), late: Math.round(data.lateHr ?? 0) }),
          hv,
        )}
      </div>
      <p className="mt-2 text-[10px] text-muted-foreground">{t('coachAnalysisFooter')}</p>
    </section>
  )
}
