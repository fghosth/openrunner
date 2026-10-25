import { useCallback, useEffect, useState } from 'react'
import { csZones, fitCS, type CsZone } from '../api'
import { readActiveAthlete } from '../App'

function paceOf(cs: number): string {
  const secsPerKm = 1000 / cs
  return `${Math.floor(secsPerKm / 60)}:${String(Math.round(secsPerKm % 60)).padStart(2, '0')}`
}

/** CS/D' 编辑器 + 自动拟合（票 08）。 */
export default function CsZoneEditor() {
  const [zones, setZones] = useState<CsZone[]>([])
  const [cs, setCs] = useState('4.5')
  const [dprime, setDprime] = useState('180')
  const [fitMsg, setFitMsg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) return
    try {
      setZones(await csZones.list(id))
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
      await csZones.upsert(id, {
        validFrom: '2000-01-01',
        validTo: '2099-12-31',
        cs: Number(cs),
        dprime: Number(dprime),
        source: 'manual',
      })
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function fit() {
    const id = readActiveAthlete()
    if (!id) return
    setFitMsg('拟合中…')
    setError(null)
    try {
      const r = await fitCS(id)
      if (r.fitted && r.cs && r.dprime) {
        setCs(r.cs.toFixed(3))
        setDprime(r.dprime.toFixed(0))
        setFitMsg(`拟合成功：CS ${r.cs.toFixed(2)} m/s（${paceOf(r.cs)}/km）· D' ${r.dprime.toFixed(0)} m · R²=${(r.r2 ?? 0).toFixed(3)}`)
        await refresh()
      } else {
        setFitMsg(`未拟合：${r.reason ?? '原因未知'}`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '拟合失败')
    }
  }

  return (
    <section aria-label="临界速度" className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">
        临界速度 CS / D'（D'bal 曲线参数，按日期生效）
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
              {z.validFrom} ~ {z.validTo} · CS {z.cs.toFixed(2)} m/s（{paceOf(z.cs)}/km）· D' {z.dprime.toFixed(0)} m ·{' '}
              {z.source === 'fit' ? '自动拟合' : '手动'}
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-muted-foreground">
          CS (m/s)
          <input
            value={cs}
            onChange={(e) => setCs(e.target.value)}
            className="ml-1 w-20 rounded border border-border bg-muted px-2 py-1 text-center font-mono"
          />
        </label>
        <label className="text-muted-foreground">
          D' (m)
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
          保存
        </button>
        <button
          onClick={fit}
          className="cursor-pointer rounded border border-border px-4 py-1.5 text-muted-foreground hover:text-foreground"
          title="从历史跑步活动的全力段自动拟合"
        >
          从跑步历史自动拟合
        </button>
      </div>
    </section>
  )
}
