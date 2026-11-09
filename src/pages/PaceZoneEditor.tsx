import { useCallback, useEffect, useState } from 'react'
import { activities, paceZones, type PaceZone } from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale } from '../i18n'
import { CURRENT_YEAR } from '../clock'
import { GT } from '../glossary-term'

function secsToPace(secs: number): string {
  return `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}`
}

function paceToSecs(mmss: string): number {
  const [m, s] = mmss.split(':').map(Number)
  if (!Number.isFinite(m) || !Number.isFinite(s)) return 0
  return m * 60 + s
}

/** 阈值配速编辑器（票 06）：rTSS 的强度基准，按日期区间生效。 */
export default function PaceZoneEditor() {
  const { t } = useLocale()
  const [zones, setZones] = useState<PaceZone[]>([])
  const [validFrom, setValidFrom] = useState(CURRENT_YEAR + '-01-01')
  const [validTo, setValidTo] = useState(CURRENT_YEAR + '-12-31')
  const [pace, setPace] = useState('5:00')
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(() =>
      // setState 推迟到微任务：effect 调用栈内同步 setState 命中 set-state-in-effect
      Promise.resolve().then(async () => {
      const id = readActiveAthlete()
      if (!id) {
        setError(t('commonNeedAthlete'))
        return
      }
      try {
        setZones(await paceZones.list(id))
      } catch (err) {
        setError(err instanceof Error ? err.message : t('commonLoadFailed'))
      }
        }), [t])

  // 删除区间记录（历史指标不自动重算——与 upsert 同口径，重算走设置页按钮）
  async function removeZone(id: number) {
    const aid = readActiveAthlete()
    if (!aid) return
    setError(null)
    try {
      await paceZones.remove(aid, id)
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('commonDeleteFailed'))
    }
  }

  useEffect(() => {
    void refresh()
  }, [refresh])

  async function save() {
    const id = readActiveAthlete()
    if (!id) {
      setError(t('commonNeedAthlete'))
      return
    }
    setError(null)
    const secs = paceToSecs(pace)
    if (secs <= 0) {
      setError(t('paceZoneFormatErr'))
      return
    }
    try {
      await paceZones.upsert(id, { validFrom, validTo, thresholdPaceSecsPerKm: secs })
      await refresh()
      await activities.recomputeAll(id).catch(() => undefined)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('commonSaveFailed'))
    }
  }

  return (
    <section aria-label={t('paceZoneAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">
        <GT text={t('paceZoneTitle')} />
      </h2>

      {error && (
        <p role="alert" className="mb-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {zones.length > 0 && (
        <ul className="mb-3 space-y-1 text-sm">
          {zones.map((z) => (
            <li key={z.id} className="font-mono text-muted-foreground">
              {t('paceZoneRow', { from: z.validFrom, to: z.validTo, pace: secsToPace(z.thresholdPaceSecsPerKm) })}
              <button
                type="button"
                className="ml-2 cursor-pointer text-muted-foreground hover:text-destructive"
                aria-label={t('commonDeleteRecord')}
                title={t('commonDeleteRecordHint')}
                onClick={() => void removeZone(z.id!)}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-muted-foreground">
          {t('commonFrom')}
          <input
            type="date"
            value={validFrom}
            onChange={(e) => setValidFrom(e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          {t('commonTo')}
          <input
            type="date"
            value={validTo}
            onChange={(e) => setValidTo(e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          <GT text={t('paceZoneThreshold')} />
          <input
            value={pace}
            onChange={(e) => setPace(e.target.value)}
            placeholder="5:00"
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1 text-center font-mono"
          />
        </label>
        <button
          onClick={save}
          className="cursor-pointer rounded bg-primary px-4 py-1.5 font-medium text-primary-foreground hover:opacity-90"
        >
          {t('commonSave')}
        </button>
      </div>
    </section>
  )
}
