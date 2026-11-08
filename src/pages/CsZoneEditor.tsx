import { useCallback, useEffect, useState } from 'react'
import { csZones, fitCS, type CsZone } from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale } from '../i18n'

function paceOf(cs: number): string {
  const secsPerKm = 1000 / cs
  return `${Math.floor(secsPerKm / 60)}:${String(Math.round(secsPerKm % 60)).padStart(2, '0')}`
}

/** CS/D' 编辑器 + 自动拟合（票 08）。 */
export default function CsZoneEditor() {
  const { t } = useLocale()
  const [zones, setZones] = useState<CsZone[]>([])
  const [domain, setDomain] = useState<'speed' | 'power'>('speed')
  const [cs, setCs] = useState('4.5')
  const [dprime, setDprime] = useState('180')
  const [fitMsg, setFitMsg] = useState<string | null>(null)
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
        setZones(await csZones.list(id))
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
      await csZones.remove(aid, id)
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
    try {
      await csZones.upsert(id, {
        domain,
        validFrom: '2000-01-01',
        validTo: '2099-12-31',
        cs: Number(cs),
        dprime: Number(dprime),
        source: 'manual',
      })
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('commonSaveFailed'))
    }
  }

  async function fit() {
    const id = readActiveAthlete()
    if (!id) {
      setError(t('commonNeedAthlete'))
      return
    }
    setFitMsg(t('csZoneFitting'))
    setError(null)
    try {
      const r = await fitCS(id, domain)
      if (r.fitted && r.cs && r.dprime) {
        const csText = domain === 'speed' ? r.cs.toFixed(3) : r.cs.toFixed(0)
        setCs(csText)
        setDprime(r.dprime.toFixed(0))
        const desc =
          domain === 'speed'
            ? `CS ${r.cs.toFixed(2)} m/s（${paceOf(r.cs)}/km）· D' ${r.dprime.toFixed(0)} m`
            : `CP ${r.cs.toFixed(0)} W · W' ${r.dprime.toFixed(0)} J`

        // 三参数模型（CP3）与二参数并列展示：它额外给出 Peak（瞬时极限 / Pmax）。
        // τ 没被数据约束住（缺 <30s 的全力段）时 peak 是外推值，明确标注不可用。
        const c = r.cp3 as
          | { cs?: number; dprime?: number; tau?: number; peak?: number; peakReliable?: boolean; r2?: number }
          | undefined
        let cp3Text = ''
        if (c && typeof c.cs === 'number' && typeof c.peak === 'number') {
          if (c.peakReliable) {
            cp3Text =
              domain === 'speed'
                ? t('csZoneThreePeak', { p: c.peak.toFixed(2), pace: paceOf(c.peak), tau: (c.tau ?? 0).toFixed(0) })
                : t('csZoneThreePmax', { p: c.peak.toFixed(0), tau: (c.tau ?? 0).toFixed(0) })
          } else {
            cp3Text = t('csZoneThreeUnreliable')
          }
        }
        setFitMsg(
          t('csZoneFitOk', { n: r.activities ?? '?', desc, r2: (r.r2 ?? 0).toFixed(3), cp3: cp3Text }),
        )
        await refresh()
      } else {
        setFitMsg(t('csZoneFitNone', { reason: r.reason ?? t('csZoneFitUnknown') }))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t('csZoneFitFailed'))
    }
  }

  return (
    <section aria-label={t('csZoneAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">
        {t('csZoneTitle')}
      </h2>

      {error && (
        <p role="alert" className="mb-3 text-sm text-destructive">
          {error}
        </p>
      )}
      {fitMsg && <p className="mb-3 text-sm text-secondary">{fitMsg}</p>}

      {zones.length > 0 && (
        <ul className="mb-3 space-y-1 text-sm">
          {zones.map((z) => (
            <li key={z.id} className="font-mono text-xs text-muted-foreground">
              {t('csZoneRow', { domain: z.domain === 'power' ? t('csZoneDomainPower') : t('csZoneDomainSpeed'), from: z.validFrom, to: z.validTo })} ·{' '}
              {z.domain === 'power'
                ? `CP ${z.cs.toFixed(0)} W · W' ${z.dprime.toFixed(0)} J`
                : `CS ${z.cs.toFixed(2)} m/s（${paceOf(z.cs)}/km）· D' ${z.dprime.toFixed(0)} m`}{' '}
              · {z.source === 'fit' ? t('csZoneSourceFit') : t('csZoneSourceManual')}
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
          {t('csZoneDomainLabel')}
          <select
            value={domain}
            onChange={(e) => {
              const d = e.target.value as 'speed' | 'power'
              setDomain(d)
              setCs(d === 'speed' ? '4.5' : '250')
              setDprime(d === 'speed' ? '180' : '20000')
            }}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          >
            <option value="speed">{t('csZoneOptSpeed')}</option>
            <option value="power">{t('csZoneOptPower')}</option>
          </select>
        </label>
        <label className="text-muted-foreground">
          {domain === 'speed' ? 'CS (m/s)' : 'CP (W)'}
          <input
            value={cs}
            onChange={(e) => setCs(e.target.value)}
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1 text-center font-mono"
          />
        </label>
        <label className="text-muted-foreground">
          {domain === 'speed' ? "D' (m)" : "W' (J)"}
          <input
            value={dprime}
            onChange={(e) => setDprime(e.target.value)}
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1 text-center font-mono"
          />
        </label>
        <button
          onClick={save}
          className="cursor-pointer rounded bg-primary px-4 py-1.5 font-medium text-primary-foreground hover:opacity-90"
        >
          {t('commonSave')}
        </button>
        <button
          onClick={fit}
          className="cursor-pointer rounded border border-border px-4 py-1.5 text-muted-foreground hover:text-foreground"
          title={t('csZoneFitTitle')}
        >
          {domain === 'speed' ? t('csZoneFitSpeed') : t('csZoneFitPower')}
        </button>
      </div>

      <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
        {domain === 'speed' ? (
          <>
            {t('csZoneHintSpeedA')}D&apos;{t('csZoneHintSpeedTail')}
          </>
        ) : (
          <>
            {t('csZoneHintPowerA')}W&apos;{t('csZoneHintPowerMid')}<b>{t('csZoneHintPowerBold')}</b>{t('csZoneHintPowerMid2')}
            <b>{t('csZoneHintPowerBold2')}</b>{t('csZoneHintPowerTail')}
          </>
        )}
      </p>
    </section>
  )
}
