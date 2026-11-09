import { useCallback, useEffect, useState } from 'react'
import { raceResults, type RaceResult } from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale } from '../i18n'
import { formatDistance, formatPace, paceUnit, useUnit } from '../units'
import { GT } from '../glossary-term'

/** 比赛成绩录入（Banister 表现模型的观测点）。
 *
 * 不要求是官方赛事 —— 自己跑的 5K/10K 全力计时同样有效，而且更容易凑够
 * 拟合所需的 3 个点。
 */
export default function RaceResultsEditor() {
  const { t } = useLocale()
  const { unit } = useUnit()
  const [list, setList] = useState<RaceResult[]>([])
  // 必须用**本地**日期：toISOString() 是 UTC，东八区凌晨 0–8 点会录成「昨天」，
  // 日期错一天 → 列表顺序看起来就是乱的。
  const [date, setDate] = useState(() => {
    const d = new Date()
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  })
  const [distanceKm, setDistanceKm] = useState('10')
  const [time, setTime] = useState('') // mm:ss 或 hh:mm:ss
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(() =>
      // setState 推迟到微任务：effect 调用栈内同步 setState 命中 set-state-in-effect
      Promise.resolve().then(async () => {
      const id = readActiveAthlete()
      if (!id) return
      try {
        setList(await raceResults.list(id))
      } catch (err) {
        setError(err instanceof Error ? err.message : t('commonLoadFailed'))
      }
        }), [t])

  useEffect(() => {
    void refresh()
  }, [refresh])

  /** 把 "1:55:19" / "22:39" 解析成秒。 */
  function parseTime(s: string): number {
    const parts = s.trim().split(':').map(Number)
    if (parts.some((v) => !Number.isFinite(v) || v < 0)) return 0
    if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2]
    if (parts.length === 2) return parts[0] * 60 + parts[1]
    if (parts.length === 1) return parts[0]
    return 0
  }

  async function add() {
    const id = readActiveAthlete()
    if (!id) {
      setError(t('settingsNeedAthlete'))
      return
    }
    const secs = parseTime(time)
    const km = Number(distanceKm)
    if (!secs || !km) {
      setError(t('raceFormatErr'))
      return
    }
    setBusy(true)
    setError(null)
    try {
      await raceResults.create(id, {
        date,
        distanceM: km * 1000,
        timeSecs: secs,
        name: name.trim(),
      })
      setTime('')
      setName('')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('commonSaveFailed'))
    } finally {
      setBusy(false)
    }
  }

  async function remove(rid?: number) {
    const id = readActiveAthlete()
    if (!id || !rid) return
    try {
      await raceResults.remove(id, rid)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('commonDeleteFailed'))
    }
  }

  return (
    <section aria-label={t('raceAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
      <div className="mb-1 flex items-baseline gap-2">
        <h2 className="text-sm font-medium text-foreground">{t('raceTitle')}</h2>
        <span className="text-[11px] text-muted-foreground">
          <GT text={t('raceSubtitle')} />
        </span>
      </div>
      <p className="mb-3 text-[11px] leading-relaxed text-muted-foreground">
        {t('raceHintA')}<b>{t('raceHintBold')}</b>{t('raceHintTail')}
      </p>

      {error && (
        <p role="alert" className="mb-2 text-xs text-destructive">
          {error}
        </p>
      )}

      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-xs text-muted-foreground">
          {t('raceDate')}
          <input
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-xs text-muted-foreground">
          {t('raceDistance')}
          <input
            type="number"
            step="0.1"
            value={distanceKm}
            onChange={(e) => setDistanceKm(e.target.value)}
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1"
          />
          km
        </label>
        <label className="text-xs text-muted-foreground">
          {t('raceTime')}
          <input
            value={time}
            onChange={(e) => setTime(e.target.value)}
            placeholder={t('raceTimePh')}
            className="ml-1 w-32 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-xs text-muted-foreground">
          {t('raceName')}
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('raceNamePh')}
            className="ml-1 w-32 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <button
          onClick={() => void add()}
          disabled={busy}
          className="cursor-pointer rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
        >
          {busy ? t('commonSaving') : t('raceAdd')}
        </button>
      </div>

      {list.length > 0 && (
        <ul className="mt-3 divide-y divide-border text-sm">
          {list.map((r) => {
            const pace = r.timeSecs / (r.distanceM / 1000)
            return (
              <li key={r.id} className="flex flex-wrap items-baseline gap-2 py-1.5">
                <span className="font-mono text-xs text-muted-foreground">{r.date}</span>
                <span className="font-mono">{formatDistance(r.distanceM, unit, 2)}</span>
                <span className="font-mono text-foreground">
                  {Math.floor(r.timeSecs / 3600) > 0
                    ? `${Math.floor(r.timeSecs / 3600)}:${String(Math.floor((r.timeSecs % 3600) / 60)).padStart(2, '0')}:${String(r.timeSecs % 60).padStart(2, '0')}`
                    : `${Math.floor(r.timeSecs / 60)}:${String(r.timeSecs % 60).padStart(2, '0')}`}
                </span>
                <span className="text-xs text-muted-foreground">
                  {formatPace(pace, unit)}
                  {paceUnit(unit)}
                </span>
                {r.name && <span className="text-xs text-muted-foreground">{r.name}</span>}
                <button
                  onClick={() => void remove(r.id)}
                  className="ml-auto cursor-pointer text-xs text-muted-foreground hover:text-destructive"
                  aria-label={t('raceDeleteAria', { date: r.date })}
                >
                  ✕
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
