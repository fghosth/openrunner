import { useEffect, useState } from 'react'
import { coach, type CoachPaces } from '../api'
import { readActiveAthlete } from '../athlete'
import { formatPace, paceUnit, useUnit } from '../units'

/** 训练配速表（coach-engine 票 04）：由阈值配速/比赛成绩推导的只读参考表。
 *  数据来源 coach/paces： Daniels VDOT 五档（S46）+ 80/20 七区（S48/S49）。 */
export default function CoachPacesCard() {
  const { unit } = useUnit()
  const [data, setData] = useState<CoachPaces | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    coach
      .paces(id)
      .then((d) => !cancelled && (setData(d), setErr(null)))
      .catch((e: Error) => !cancelled && setErr(e.message))
    return () => {
      cancelled = true
    }
  }, [])

  const pace = (s?: number) => (s && s > 0 ? formatPace(s, unit) : '—')
  const range = (fast?: number, slow?: number) =>
    fast && slow ? `${pace(fast)}–${pace(slow)}` : slow ? `≤ ${pace(slow)}` : '—'
  const hr = (lo?: number, hi?: number) => (lo && hi ? `${Math.round(lo)}–${Math.round(hi)}` : '—')
  // 服务端 use 为 zones.py 逐字英文（金标准口径），UI 按档名映射中文
  const useZh: Record<string, string> = {
    Z1: '恢复、热身放松', Z2: '轻松跑与长距离（上半区稳态）', X: '马拉松配速（有意安排才用）',
    Z3: '节奏跑、巡航间歇', Y: '乳酸阈', Z4: 'VO2 间歇（5K–3K 配速）', Z5: '冲刺、短快重复',
    E: '轻松跑与长距离', M: '马拉松配速', T: '阈值、节奏', I: '间歇 3–5 分钟', R: '重复 200–400 米',
  }
  const use = (r: { name: string; use: string }) => useZh[r.name] ?? r.use

  if (err) return null
  if (!data) return <div className="my-4 text-sm text-muted-foreground">训练配速表加载中…</div>
  if (data.error)
    return (
      <div className="my-4 rounded-lg border border-border bg-card p-4 text-sm text-muted-foreground">
        训练配速表：{data.error}
      </div>
    )

  const srcLabel = data.threshold?.source === 'pace_zones' ? '设置页阈值配速' : '比赛成绩反推'
  return (
    <div className="my-4 rounded-lg border border-border bg-card p-4">
      <h3 className="mb-1 text-sm font-medium">训练配速表</h3>
      <p className="mb-3 text-xs text-muted-foreground">
        阈值 {pace(data.threshold?.secsPerKm)}
        {paceUnit(unit)}（来源：{srcLabel}）
        {data.lthr ? ` · LTHR ${Math.round(data.lthr)}` : ''}
        {data.vdot ? ` · VDOT ${data.vdot.value.toFixed(1)}` : ''}
      </p>

      {data.vdot && (
        <table className="mb-4 w-full text-xs">
          <thead>
            <tr className="text-left text-muted-foreground">
              <th className="py-1 pr-2 font-normal">VDOT 档</th>
              <th className="py-1 pr-2 font-normal">配速</th>
              <th className="py-1 font-normal">用途</th>
            </tr>
          </thead>
          <tbody>
            {data.vdot.rows.map((r) => (
              <tr key={r.name} className="border-t border-border/50">
                <td className="py-1 pr-2 font-medium">{r.name}</td>
                <td className="py-1 pr-2 tabular-nums">
                  {range(r.paceFast, r.paceSlow)}
                </td>
                <td className="py-1 text-muted-foreground">{use(r)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {data.zones8020 && (
        <table className="w-full text-xs">
          <thead>
            <tr className="text-left text-muted-foreground">
              <th className="py-1 pr-2 font-normal">80/20 区</th>
              <th className="py-1 pr-2 font-normal">配速</th>
              <th className="py-1 pr-2 font-normal">心率</th>
              <th className="py-1 font-normal">用途</th>
            </tr>
          </thead>
          <tbody>
            {data.zones8020.map((r) => (
              <tr key={r.name} className="border-t border-border/50">
                <td className="py-1 pr-2 font-medium">
                  {r.name}
                </td>
                <td className="py-1 pr-2 tabular-nums">{range(r.paceFast, r.paceSlow)}</td>
                <td className="py-1 pr-2 tabular-nums">{hr(r.hrLo, r.hrHi)}</td>
                <td className="py-1 text-muted-foreground">{use(r)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <p className="mt-3 text-[11px] text-muted-foreground">
        Daniels VDOT（S46）与 80/20 分区（S48/S49）为已发表公式；数值与 running-coach
        金标准逐档对照（make coach-check）。
      </p>
    </div>
  )
}
