import { useCallback, useEffect, useState } from 'react'
import { paceZones, type PaceZone } from '../api'
import { readActiveAthlete } from '../App'

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
  const [zones, setZones] = useState<PaceZone[]>([])
  const [validFrom, setValidFrom] = useState('2026-01-01')
  const [validTo, setValidTo] = useState('2026-12-31')
  const [pace, setPace] = useState('5:00')
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) return
    try {
      setZones(await paceZones.list(id))
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  async function save() {
    const id = readActiveAthlete()
    if (!id) return
    setError(null)
    const secs = paceToSecs(pace)
    if (secs <= 0) {
      setError('配速格式：分:秒/公里，如 5:00')
      return
    }
    try {
      await paceZones.upsert(id, { validFrom, validTo, thresholdPaceSecsPerKm: secs })
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  return (
    <section aria-label="阈值配速" className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">
        阈值配速（rTSS 参数，按日期生效）
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
              {z.validFrom} ~ {z.validTo} · 阈值 {secsToPace(z.thresholdPaceSecsPerKm)} /km
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-muted-foreground">
          起
          <input
            type="date"
            value={validFrom}
            onChange={(e) => setValidFrom(e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          止
          <input
            type="date"
            value={validTo}
            onChange={(e) => setValidTo(e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          阈值配速
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
          保存
        </button>
      </div>
    </section>
  )
}
