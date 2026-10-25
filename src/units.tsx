import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

/** 单位制（对齐 running_page 的 metric/imperial 切换）。
 *
 * 存 localStorage 并挂在 Context 上，这样切换后所有使用方立即重渲染
 * （模块级变量做不到这点）。
 */

export type Unit = 'metric' | 'imperial'

const STORAGE_KEY = 'openrunner.unit'
const KM_PER_MILE = 1.609344
const FT_PER_M = 3.280839895

type Ctx = { unit: Unit; setUnit: (u: Unit) => void }

const UnitContext = createContext<Ctx>({ unit: 'metric', setUnit: () => undefined })

function readUnit(): Unit {
  const v = localStorage.getItem(STORAGE_KEY)
  return v === 'imperial' ? 'imperial' : 'metric'
}

export function UnitProvider({ children }: { children: ReactNode }) {
  const [unit, setUnitState] = useState<Unit>(readUnit)

  const setUnit = useCallback((u: Unit) => {
    setUnitState(u)
    localStorage.setItem(STORAGE_KEY, u)
  }, [])

  const value = useMemo(() => ({ unit, setUnit }), [unit, setUnit])
  return <UnitContext.Provider value={value}>{children}</UnitContext.Provider>
}

export function useUnit(): Ctx {
  return useContext(UnitContext)
}

// ---------- 格式化 ----------

/** 距离（米）→ 带单位数值。返回 [数值, 单位]。 */
export function distanceValue(meters: number, unit: Unit): [number, string] {
  if (unit === 'imperial') return [meters / 1000 / KM_PER_MILE, 'mi']
  return [meters / 1000, 'km']
}

export function formatDistance(meters: number, unit: Unit, digits?: number): string {
  const [v, u] = distanceValue(meters, unit)
  const d = digits ?? (v >= 100 ? 0 : 1)
  return `${v.toFixed(d)} ${u}`
}

/** 配速：输入 secs/km，输出目标单位下的 mm:ss。 */
export function formatPace(secsPerKm: number, unit: Unit): string {
  if (!secsPerKm || !Number.isFinite(secsPerKm)) return '—'
  const v = unit === 'imperial' ? secsPerKm * KM_PER_MILE : secsPerKm
  const m = Math.floor(v / 60)
  const s = Math.round(v % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}

export function paceUnit(unit: Unit): string {
  return unit === 'imperial' ? '/mi' : '/km'
}

/** 爬升（米）→ 带单位。 */
export function formatElevation(meters: number, unit: Unit): string {
  if (unit === 'imperial') return `${(meters * FT_PER_M).toFixed(0)} ft`
  return `${meters.toFixed(0)} m`
}

/** 速度（m/s）→ 带单位（骑行常用）。 */
export function formatSpeed(mps: number, unit: Unit): string {
  if (mps <= 0) return '—'
  const kph = mps * 3.6
  return unit === 'imperial' ? `${(kph / KM_PER_MILE).toFixed(1)} mph` : `${kph.toFixed(1)} km/h`
}

/** 单位切换按钮（放进 header）。 */
export function UnitToggle() {
  const { unit, setUnit } = useUnit()
  const next: Unit = unit === 'metric' ? 'imperial' : 'metric'
  return (
    <button
      onClick={() => setUnit(next)}
      title={next === 'imperial' ? '切换到英制（mi/ft）' : '切换到公制（km/m）'}
      className="cursor-pointer rounded border border-border px-2 py-1.5 text-sm text-muted-foreground hover:text-foreground"
    >
      {unit === 'metric' ? 'km' : 'mi'}
    </button>
  )
}

/** 只在首次挂载时把单位写回（避免 SSR/首屏闪烁时缺失）。 */
export function useUnitEffectSync() {
  const { unit } = useUnit()
  useEffect(() => {
    document.documentElement.dataset.unit = unit
  }, [unit])
}
