import { useUnit, type Unit } from './units'

/** 单位切换按钮（放进 header）。 */
export default function UnitToggle() {
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

