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
  const [domain, setDomain] = useState<'speed' | 'power'>('speed')
  const [cs, setCs] = useState('4.5')
  const [dprime, setDprime] = useState('180')
  const [fitMsg, setFitMsg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) {
      setError('请先在上方「Athlete 档案」创建并选择一个档案')
      return
    }
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
    if (!id) {
      setError('请先在上方「Athlete 档案」创建并选择一个档案')
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
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function fit() {
    const id = readActiveAthlete()
    if (!id) {
      setError('请先在上方「Athlete 档案」创建并选择一个档案')
      return
    }
    setFitMsg('拟合中…')
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
        setFitMsg(`拟合成功：${desc} · R²=${(r.r2 ?? 0).toFixed(3)}`)
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
              [{z.domain === 'power' ? '功率' : '速度'}] {z.validFrom} ~ {z.validTo} ·{' '}
              {z.domain === 'power'
                ? `CP ${z.cs.toFixed(0)} W · W' ${z.dprime.toFixed(0)} J`
                : `CS ${z.cs.toFixed(2)} m/s（${paceOf(z.cs)}/km）· D' ${z.dprime.toFixed(0)} m`}{' '}
              · {z.source === 'fit' ? '自动拟合' : '手动'}
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-muted-foreground">
          域
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
            <option value="speed">速度（CS m/s / D' m）</option>
            <option value="power">功率（CP W / W' J）</option>
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
          保存
        </button>
        <button
          onClick={fit}
          className="cursor-pointer rounded border border-border px-4 py-1.5 text-muted-foreground hover:text-foreground"
          title="从历史活动的全力段自动拟合（速度域用跑步；功率域优先骑行，骑行不足时用全部带功率流的活动）"
        >
          {domain === 'speed' ? '从跑步历史自动拟合' : '从功率历史自动拟合'}
        </button>
      </div>

      <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
        {domain === 'speed' ? (
          <>
            速度域：CS 是理论上能无限久维持的最快配速，D&apos; 是超过 CS 时能动用的距离储备
            （成人正常 150–300 m）。需要至少 3 次不同强度的跑步（含间歇/计时跑）才能拟合可靠。
          </>
        ) : (
          <>
            功率域：CP 是理论上能无限久维持的最大功率，W&apos; 是超过 CP 时能动用的能量储备。
            跑步的临界功率通常约为 <b>3.5–4.5 W/kg</b>（例：60 kg 的人约 210–270 W），
            <b>不要直接照抄骑行的 FTP</b>。功率负荷 = 100 × 时长 × (NP/CP)²，配好 CP 才有值。
          </>
        )}
      </p>
    </section>
  )
}
