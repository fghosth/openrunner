import { useEffect, useState } from 'react'
import { coach, type CoachPaces } from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale } from '../i18n'
import { formatPace, paceUnit, useUnit } from '../units'
import { GT } from '../glossary-term'

/** 训练配速表（coach-engine 票 04）：由阈值配速/比赛成绩推导的只读参考表。 // i18n-allow: 服务端英文 use 的 zh 显示映射（en 直接用服务端原文）
 *  数据来源 coach/paces： Daniels VDOT 五档（S46）+ 80/20 七区（S48/S49）。 */ // i18n-allow: 服务端英文 use 的 zh 显示映射（en 直接用服务端原文）
export default function CoachPacesCard() {
  const { unit } = useUnit()
  const { locale, t } = useLocale()
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
    Z1: '恢复、热身放松', Z2: '轻松跑与长距离（上半区稳态）', X: '马拉松配速（有意安排才用）', // i18n-allow: 服务端英文 use 的 zh 显示映射（en 直接用服务端原文）
    Z3: '节奏跑、巡航间歇', Y: '乳酸阈', Z4: 'VO2 间歇（5K–3K 配速）', Z5: '冲刺、短快重复', // i18n-allow: 服务端英文 use 的 zh 显示映射（en 直接用服务端原文）
    E: '轻松跑与长距离', M: '马拉松配速', T: '阈值、节奏', I: '间歇 3–5 分钟', R: '重复 200–400 米', // i18n-allow: 服务端英文 use 的 zh 显示映射（en 直接用服务端原文）
  }
  // en 语言直接用服务端英文（金标准原文）
  const use = (r: { name: string; use: string }) => (locale === 'zh' ? (useZh[r.name] ?? r.use) : r.use)

  if (err) return null
  if (!data) return <div className="my-4 text-sm text-muted-foreground">{t('coachPacesLoading')}</div>
  if (data.error)
    return (
      <div className="my-4 rounded-lg border border-border bg-card p-4 text-sm text-muted-foreground">
        {t('coachPacesError', { error: data.error })}
      </div>
    )

  const srcLabel = data.threshold?.source === 'pace_zones' ? t('coachPacesSourceZones') : t('coachPacesSourceRace')
  return (
    <div className="my-4 rounded-lg border border-border bg-card p-4">
      <h3 className="mb-1 text-sm font-medium">{t('coachPacesTitle')}</h3>
      <p className="mb-3 text-xs text-muted-foreground">
        {t('coachPacesThreshold', { pace: pace(data.threshold?.secsPerKm), unit: paceUnit(unit), source: srcLabel })}
        {data.lthr ? ` · LTHR ${Math.round(data.lthr)}` : ''}
        {data.vdot ? ` · VDOT ${data.vdot.value.toFixed(1)}` : ''}
      </p>

      {data.vdot && (
        <table className="mb-4 w-full text-xs">
          <thead>
            <tr className="text-left text-muted-foreground">
              <th className="py-1 pr-2 font-normal"><GT text={t('coachPacesVdotCol')} /></th>
              <th className="py-1 pr-2 font-normal">{t('coachPacesPaceCol')}</th>
              <th className="py-1 font-normal">{t('coachPacesUseCol')}</th>
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
              <th className="py-1 pr-2 font-normal"><GT text={t('coachPacesZoneCol')} /></th>
              <th className="py-1 pr-2 font-normal">{t('coachPacesPaceCol')}</th>
              <th className="py-1 pr-2 font-normal">{t('coachPacesHrCol')}</th>
              <th className="py-1 font-normal">{t('coachPacesUseCol')}</th>
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
      <p className="mt-3 text-[11px] text-muted-foreground"><GT text={t('coachPacesFooter')} /></p>
    </div>
  )
}
