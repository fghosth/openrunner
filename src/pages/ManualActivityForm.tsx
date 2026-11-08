import { useState } from 'react'
import { useLocale } from '../i18n'
import type { ManualActivityInput } from '../api'

/** 手工录入（计划）活动（票 07）：无 Stream，走估算指标路径。 */
export default function ManualActivityForm({
  onSubmit,
}: {
  onSubmit: (input: ManualActivityInput) => Promise<void>
}) {
  const { t } = useLocale()
  const [open, setOpen] = useState(false)
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10))
  const [sport, setSport] = useState('run')
  const [durationMin, setDurationMin] = useState(60)
  const [distanceKm, setDistanceKm] = useState(10)
  const [avgHr, setAvgHr] = useState(150)
  const [planned, setPlanned] = useState(true)
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      await onSubmit({
        date,
        sport,
        durationSecs: durationMin * 60,
        distanceM: distanceKm * 1000,
        avgHr,
        isPlanned: planned,
      })
    } finally {
      setBusy(false)
    }
  }

  if (!open) {
    return (
      <div className="mb-6">
        <button
          onClick={() => setOpen(true)}
          className="cursor-pointer rounded border border-border bg-card px-4 py-2 text-sm text-muted-foreground hover:text-foreground"
        >
          {t('manualToggle')}
        </button>
      </div>
    )
  }

  return (
    <section aria-label={t('manualAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">
        {t('manualTitle')}
      </h2>
      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-muted-foreground">
          {t('manualDate')}
          <input
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          {t('manualType')}
          <select
            value={sport}
            onChange={(e) => setSport(e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          >
            <option value="run">{t('sportRun')}</option>
            <option value="ride">{t('sportRide')}</option>
            <option value="swim">{t('sportSwim')}</option>
            <option value="other">{t('sportOther')}</option>
          </select>
        </label>
        <label className="text-muted-foreground">
          {t('manualDuration')}
          <input
            type="number"
            value={durationMin}
            onChange={(e) => setDurationMin(Number(e.target.value))}
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          {t('manualDistance')}
          <input
            type="number"
            value={distanceKm}
            onChange={(e) => setDistanceKm(Number(e.target.value))}
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          {t('manualAvgHr')}
          <input
            type="number"
            value={avgHr}
            onChange={(e) => setAvgHr(Number(e.target.value))}
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="flex cursor-pointer items-center gap-1 text-muted-foreground">
          <input type="checkbox" checked={planned} onChange={(e) => setPlanned(e.target.checked)} />
          {t('manualPlanned')}
        </label>
        <button
          onClick={submit}
          disabled={busy}
          className="cursor-pointer rounded bg-primary px-4 py-1.5 font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
        >
          {busy ? t('commonSaving') : t('commonSave')}
        </button>
        <button
          onClick={() => setOpen(false)}
          className="cursor-pointer rounded border border-border px-3 py-1.5 text-muted-foreground hover:text-foreground"
        >
          {t('manualCollapse')}
        </button>
      </div>
    </section>
  )
}
