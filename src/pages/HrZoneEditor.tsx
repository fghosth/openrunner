import { useCallback, useEffect, useState } from 'react'
import { hrZones, type HrZone } from '../api'
import { readActiveAthlete } from '../App'

/** 心率区间编辑器（票 05）：按日期区间生效，TRIMP/TRIMP(100) 的生理参数源。 */
export default function HrZoneEditor() {
  const [zones, setZones] = useState<HrZone[]>([])
  const [form, setForm] = useState<HrZone>({
    validFrom: '2026-01-01',
    validTo: '2026-12-31',
    maxHr: 190,
    restHr: 55,
    ltHr: 170,
  })
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) return
    try {
      setZones(await hrZones.list(id))
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
    try {
      await hrZones.upsert(id, form)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  function set<K extends keyof HrZone>(key: K, value: HrZone[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  return (
    <section aria-label="心率区间" className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">
        心率区间（TRIMP 参数，按日期生效）
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
              {z.validFrom} ~ {z.validTo} · Max {z.maxHr} / Rest {z.restHr} / LTHR {z.ltHr}
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-muted-foreground">
          起
          <input
            type="date"
            value={form.validFrom}
            onChange={(e) => set('validFrom', e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          止
          <input
            type="date"
            value={form.validTo}
            onChange={(e) => set('validTo', e.target.value)}
            className="ml-1 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          MaxHR
          <input
            type="number"
            value={form.maxHr}
            onChange={(e) => set('maxHr', Number(e.target.value))}
            className="ml-1 w-16 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          RestHR
          <input
            type="number"
            value={form.restHr}
            onChange={(e) => set('restHr', Number(e.target.value))}
            className="ml-1 w-16 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <label className="text-muted-foreground">
          LTHR
          <input
            type="number"
            value={form.ltHr}
            onChange={(e) => set('ltHr', Number(e.target.value))}
            className="ml-1 w-16 rounded border border-border bg-muted px-2 py-1"
          />
        </label>
        <button
          onClick={save}
          className="cursor-pointer rounded bg-primary px-4 py-1.5 font-medium text-primary-foreground hover:opacity-90"
        >
          保存区间
        </button>
      </div>
    </section>
  )
}
