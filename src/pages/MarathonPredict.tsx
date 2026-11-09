import { useEffect, useState } from 'react'
import { coach, type CoachPredict } from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale, type TKey } from '../i18n'
import { GT } from '../glossary-term'

/** 全马多模型预测（coach-engine 票 05）：Riegel / Vickers 1·2 / Tanda，
 *  recommended 取最慢可信值（skill 纪律「Plan from the slowest credible estimate」）。 */

const fmt = (s?: number) => {
  if (!s || s <= 0) return '—'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.round(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}

const MODELS: { key: 'riegel' | 'vickers1' | 'vickers2' | 'tanda'; nameKey: TKey; inputKey: TKey; src: string }[] = [
  { key: 'riegel', nameKey: 'predictRiegelName', inputKey: 'predictInputRiegel', src: 'S27' },
  { key: 'vickers1', nameKey: 'predictV1Name', inputKey: 'predictInputV1', src: 'S25' },
  { key: 'vickers2', nameKey: 'predictV2Name', inputKey: 'predictInputV2', src: 'S25' },
  { key: 'tanda', nameKey: 'predictTandaName', inputKey: 'predictInputTanda', src: 'S26' },
]

const TIP_KEY = 'openrunner.marathon-riegel-tip'

export default function MarathonPredict() {
  const { t } = useLocale()
  const [data, setData] = useState<CoachPredict | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [tipClosed, setTipClosed] = useState(() => localStorage.getItem(TIP_KEY) === '1')

  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    coach
      .predict(id)
      .then((d) => !cancelled && (setData(d), setErr(null)))
      .catch((e: Error) => !cancelled && setErr(e.message))
    return () => {
      cancelled = true
    }
  }, [])

  if (err) return null
  if (!data) return <div className="mt-4 text-xs text-muted-foreground">{t('predictLoading')}</div>
  if (data.error)
    return (
      <section aria-label={t('predictAria')} className="mb-6 rounded-lg border border-border bg-card p-4 text-xs text-muted-foreground">
        {t('predictError', { error: data.error })}
      </section>
    )

  const p = data.prediction
  if (!p) return null
  const baseKm = (data.base?.distanceM ?? 0) / 1000

  return (
    <section aria-label={t('predictAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
      <div className="mb-2 flex flex-wrap items-baseline gap-2">
        <h3 className="text-xs font-medium text-foreground">{t('predictTitle')}</h3>
        <span className="text-[11px] text-muted-foreground">
          <GT text={t('predictBase', {
            km: baseKm.toFixed(baseKm % 1 ? 2 : 0),
            time: fmt(data.base?.timeSecs),
            source: data.base?.source === 'race' ? t('predictSourceRace') : t('predictSourceManual'),
            kmw: data.weekly_km?.toFixed(1) ?? '—',
            weeks: data.weeks_observed ?? 0,
          })} />
        </span>
      </div>

      {!tipClosed && (
        <div className="mb-2 flex items-start gap-2 rounded border border-border bg-card p-2 text-[11px] text-muted-foreground">
          <span className="flex-1">
            {t('predictTipA')} <b className="text-foreground">{t('predictTipBold')}</b> {t('predictTipB')}
          </span>
          <button
            type="button"
            className="shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => {
              localStorage.setItem(TIP_KEY, '1')
              setTipClosed(true)
            }}
            aria-label={t('predictCloseTip')}
          >
            ✕
          </button>
        </div>
      )}

      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
            <th className="py-1 pr-3">{t('predictColModel')}</th>
            <th className="py-1 pr-3">{t('predictColValue')}</th>
            <th className="py-1 pr-3">{t('predictColInput')}</th>
            <th className="py-1">{t('predictColSrc')}</th>
          </tr>
        </thead>
        <tbody>
          {MODELS.map((m) => {
            const v = p[m.key]
            if (!v) return null
            const isRec = Math.abs(v - p.recommended) < 0.5
            return (
              <tr key={m.key} className="border-b border-border/50">
                <td className="py-1.5 pr-3 font-medium">
                  {t(m.nameKey)}
                  {isRec && (
                    <span className="ml-2 rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] text-emerald-500">
                      {t('predictRecommended')}
                    </span>
                  )}
                </td>
                <td className={`py-1.5 pr-3 font-mono ${isRec ? 'text-emerald-500' : ''}`}>{fmt(v)}</td>
                <td className="py-1.5 pr-3 text-[11px] text-muted-foreground">{t(m.inputKey)}</td>
                <td className="py-1.5 text-[11px] text-muted-foreground">{m.src}</td>
              </tr>
            )
          })}
        </tbody>
      </table>

      {p.lowConfidence && (
        <p className="mt-2 text-[11px] text-accent"><GT text={t('predictLowConfidence')} /></p>
      )}
      {p.notes?.map((n) => (
        <p key={n} className="mt-1 text-[11px] text-muted-foreground">
          · {n}
        </p>
      ))}
      <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">
        <GT text={t('predictFooter')} />
      </p>
    </section>
  )
}
