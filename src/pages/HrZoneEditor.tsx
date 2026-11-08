import { useCallback, useEffect, useState } from 'react'
import { activities, hrZones, type HrZone, type ObservedHr } from '../api'
import { readActiveAthlete } from '../athlete'
import { CURRENT_YEAR, TODAY_STR } from '../clock'

/** 心率区间编辑器（票 05）：按日期区间生效，TRIMP/TRIMP(100) 的生理参数源。 */
export default function HrZoneEditor() {
  const [zones, setZones] = useState<HrZone[]>([])
  const [form, setForm] = useState<HrZone>({
    validFrom: CURRENT_YEAR + '-01-01',
    validTo: CURRENT_YEAR + '-12-31',
    maxHr: 190,
    restHr: 55,
    ltHr: 170,
  })
  const [error, setError] = useState<string | null>(null)
  const [observed, setObserved] = useState<ObservedHr | null>(null)
  const [saved, setSaved] = useState(false)

  const refresh = useCallback(() =>
      // setState 推迟到微任务：effect 调用栈内同步 setState 命中 set-state-in-effect
      Promise.resolve().then(async () => {
      const id = readActiveAthlete()
      if (!id) {
        setError('请先在上方「Athlete 档案」创建并选择一个档案')
        return
      }
      try {
        setZones(await hrZones.list(id))
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载失败')
      }
        }), [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // 把「当前生效的区间」回填到表单。
  // 不这么做的话表单永远显示硬编码默认值（190/55/165），用户以为在改自己的真实参数，
  // 一点保存就把库里正确的值覆盖成默认值（实测把观测到的 MaxHR <占位> 覆盖成 190）。
  // 注意不回填 id：保持「同日期区间=更新、换区间=新增」的语义。
  const [backfilledZones, setBackfilledZones] = useState<typeof zones | null>(null)
  if (zones.length > 0 && zones !== backfilledZones) {
    setBackfilledZones(zones)
    const active = zones.find((z) => z.validFrom <= TODAY_STR && z.validTo >= TODAY_STR) ?? zones[zones.length - 1]
    setForm({
      validFrom: active.validFrom,
      validTo: active.validTo,
      maxHr: active.maxHr,
      restHr: active.restHr,
      ltHr: active.ltHr,
    })
  }

  // 从历史活动心率反推观测 MaxHR（仅作建议，不自动覆盖用户填写值）
  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    hrZones
      .observed(id)
      .then((o) => {
        if (!cancelled) setObserved(o)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [zones.length])

  async function save() {
    const id = readActiveAthlete()
    if (!id) {
      setError('请先在上方「Athlete 档案」创建并选择一个档案')
      return
    }
    setError(null)
    setSaved(false)
    try {
      await hrZones.upsert(id, form)
      await refresh()
      setSaved(true)
      setTimeout(() => setSaved(false), 3000)
      // 配好区间后立即重算历史活动（否则趋势页无数据）
      await activities.recomputeAll(id).catch(() => undefined)
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

      {saved && (
        <p role="status" className="mb-3 rounded border border-border bg-muted/60 px-2 py-1 text-xs text-foreground">
          ✓ 已保存，历史活动指标正在后台重算
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

      {observed && observed.maxHr > 0 && (
        <p className="mb-3 rounded border border-border bg-muted/50 p-2 text-xs text-muted-foreground">
          从 {observed.activities} 次活动的逐秒心率中观测到：
          <strong className="mx-1 text-foreground">峰值 {Math.round(observed.maxHr)} bpm</strong>
          （持续 30 秒的最高均值 {Math.round(observed.sustainedHr)} bpm）
          {form.maxHr !== Math.round(observed.maxHr) && (
            <button
              type="button"
              onClick={() => set('maxHr', Math.round(observed.maxHr))}
              className="ml-1 cursor-pointer underline hover:text-foreground"
            >
              用此值
            </button>
          )}
          <br />
          这是<strong className="text-foreground">观测下界</strong>
          ——若你近期没有全力冲刺/爬坡，真实 MaxHR 可能更高。
          {observed.excluded > 0 && (
            <span>
              另有 {observed.excluded} 次活动因心率数据质量不足（多为力量训练/球类时
              腕表受压产生的伪影）未采纳。
            </span>
          )}
        </p>
      )}
      {observed && observed.maxHr === 0 && (
        <p className="mb-3 rounded border border-border bg-muted/50 p-2 text-xs text-muted-foreground">
          还没有带心率数据的活动，无法给出建议值 —— 可先同步几次活动，或按 220−年龄 粗估。
        </p>
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
