/** 当前档案 id 的 localStorage 读写（从 App.tsx 移出：react-refresh 只允许组件文件导出组件）。 */
const ACTIVE_ATHLETE_KEY = 'openrunner.activeAthlete'

export function readActiveAthlete(): number | null {
  const v = Number(localStorage.getItem(ACTIVE_ATHLETE_KEY))
  return Number.isFinite(v) && v > 0 ? v : null
}

export function writeActiveAthlete(id: number) {
  localStorage.setItem(ACTIVE_ATHLETE_KEY, String(id))
}
