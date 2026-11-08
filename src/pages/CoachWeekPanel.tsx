import { useMemo, useRef, useState } from 'react'
import { coach, activities as activitiesApi } from '../api'
import type { Activity, CoachPlanUnit, CoachVerdict, CoachWeek } from '../api'
import { useLocale } from '../i18n'
import { distanceValue, useUnit } from '../units'
import { readActiveAthlete } from '../athlete'
import { NOW } from '../clock'

/** 下周计划校验面板（coach-engine 票 03）。 // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
 *
 * 数据流：日历里下周的 isPlanned 跑步 = 基线课表；用户可加草稿课； // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
 * 「校验」把两者合并发给 /coach/week-check（服务端引擎，与 skill 同一实现）； // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
 * 「写入日历」只落草稿——沿用 running-coach 的纪律：确认后且无 FAIL 才写。 // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
 */

const TYPES = ['easy', 'recovery', 'long', 'tempo', 'threshold', 'intervals', 'hills', 'race'] as const
const ROLES = ['build', 'base', 'peak', 'deload', 'taper', 'race', 'return'] as const

function nextMonday(from: Date): Date {
  const off = (from.getDay() + 6) % 7
  return new Date(from.getFullYear(), from.getMonth(), from.getDate() - off + 7)
}

function iso(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const LEVEL_STYLE: Record<string, string> = {
  OK: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
  WARN: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
  FAIL: 'bg-red-500/15 text-red-400 border-red-500/30',
}

type CalUnit = CoachPlanUnit & { activityId: number }
type PlanRow = { kind: 'cal'; unit: CalUnit } | { kind: 'draft'; unit: CoachPlanUnit }

export default function CoachWeekPanel({
  activities,
  onActivitiesChanged,
}: {
  activities?: Activity[]
  /** 删除/修改日历计划课后让 Home 重拉活动列表 */
  onActivitiesChanged?: () => void
}) {
  const { locale, t } = useLocale()
  const { unit } = useUnit()
  // 距离随单位制（票 03 验收项；时长分钟制两制通用）
  const kmFmt = (km?: number) => {
    if (!km) return '—'
    const [v, label] = distanceValue(km * 1000, unit)
    return `${Math.round(v * 10) / 10} ${label}`
  }
  const zh = locale === 'zh'

  const weekStart = useMemo(() => nextMonday(NOW), [])
  const weekDates = useMemo(
    () => Array.from({ length: 7 }, (_, i) => new Date(weekStart.getFullYear(), weekStart.getMonth(), weekStart.getDate() + i)),
    [weekStart],
  )
  const weekSet = useMemo(() => new Set(weekDates.map(iso)), [weekDates])

  // 日历里下周的计划跑步（服务端 week-check 的缺省 plan 来源，这里展示同一批）
  const calendarUnits: CalUnit[] = useMemo(() => {
    const out: CalUnit[] = []
    for (const a of activities ?? []) {
      if (!a.isPlanned || a.sport !== 'run') continue
      const d = new Date(a.startTime * 1000)
      const key = iso(d)
      if (!weekSet.has(key)) continue
      out.push({
        activityId: a.id,
        date: key,
        type: a.planType || 'easy',
        minutes: Math.round((a.timerSecs || a.durationSecs) / 60),
        km: Math.round((a.distanceM / 1000) * 10) / 10,
      })
    }
    return out.sort((x, y) => x.date.localeCompare(y.date))
  }, [activities, weekSet])

  const [drafts, setDrafts] = useState<CoachPlanUnit[]>([])
  const [role, setRole] = useState('build')
  const [verdicts, setVerdicts] = useState<CoachVerdict[] | null>(null)
  const [week, setWeek] = useState<CoachWeek | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [override, setOverride] = useState(false)
  const [written, setWritten] = useState(false)

  // 草稿编辑行
  const [draft, setDraft] = useState({ date: iso(weekDates[5]), type: 'easy', minutes: 60, km: 10 })

  const rows: PlanRow[] = [
    ...calendarUnits.map((u) => ({ kind: 'cal' as const, unit: u })),
    ...drafts.map((u) => ({ kind: 'draft' as const, unit: u })),
  ]
  const all = rows.map((r) => r.unit)
  const hasFail = !!verdicts?.some((v) => v.level === 'FAIL')

  async function check() {
    const id = readActiveAthlete()
    if (!id || all.length === 0) return
    setBusy(true)
    setError(null)
    setWritten(false)
    try {
      const r = await coach.weekCheck(id, { week_role: role, plan: all })
      setVerdicts(r.verdicts)
      setWeek(r.week)
    } catch (e) {
      setVerdicts(null)
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  // 同步防重锁：setBusy 是异步 state，快速双击会在重渲染前跑两遍循环，
  // 把每条草稿写两遍（用户实测 11-14 出现两条完全相同的计划课）
  const writingRef = useRef(false)

  async function writeCalendar() {
    const id = readActiveAthlete()
    if (!id || writingRef.current) return
    writingRef.current = true
    setBusy(true)
    setError(null)
    try {
      for (const s of drafts) {
        await activitiesApi.createManual(id, {
          date: s.date,
          sport: 'run',
          durationSecs: Math.round(s.minutes * 60),
          distanceM: Math.round((s.km ?? 0) * 1000),
          avgHr: 0,
          isPlanned: true,
          type: s.type, // 课型落库——否则日历行回读全变 easy，强度校验失真
        })
      }
      setWritten(true)
      setDrafts([])
      onActivitiesChanged?.() // 写入的课立刻以「日历」行回到面板（否则要刷新页面才出现）
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
      writingRef.current = false
    }
  }

  // 删除日历计划课（草稿删除只动本地 state，日历行要落库）
  async function removeCalendarUnit(u: CalUnit) {
    const id = readActiveAthlete()
    if (!id) return
    setBusy(true)
    setError(null)
    try {
      await activitiesApi.remove(id, u.activityId)
      setVerdicts(null) // 计划变了，旧判定作废
      onActivitiesChanged?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  // 修改日历计划课（日期/时长/距离）；失焦提交
  async function updateCalendarUnit(u: CalUnit, next: { date: string; minutes: number; km: number; type?: string }) {
    const id = readActiveAthlete()
    if (!id) return
    if (next.date === u.date && next.minutes === u.minutes && Math.abs(next.km - (u.km ?? 0)) < 1e-6 && (!next.type || next.type === u.type)) return
    setBusy(true)
    setError(null)
    try {
      await activitiesApi.updateManual(id, u.activityId, {
        date: next.date,
        durationSecs: Math.round(next.minutes * 60),
        distanceM: Math.round(next.km * 1000),
        type: next.type,
      })
      setVerdicts(null)
      onActivitiesChanged?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const ruleLabel: Record<string, [string, string]> = {
    volume: ['周增量', 'Volume'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    'deload-rhythm': ['减量节奏', 'Deload rhythm'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    spike: ['单次 +10%', 'Single-run spike'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    'long-share': ['长课占比', 'Long-run share'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    'hard-days': ['硬课安排', 'Hard days'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    strength: ['力量课位置', 'Strength placement'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    intensity: ['强度分布', 'Intensity split'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    rest: ['休息日', 'Rest days'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    input: ['输入', 'Input'], // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
  }

  // 票 02：zh 按模板渲染 Params，en 回落英文 detail（与金标准逐字同源）。
  // 无模板/缺参数一律回落 detail，绝不空白。
  const roleZh: Record<string, string> = {
    base: '基础', build: '构建', peak: '巅峰', deload: '减量', taper: '赛前减量', race: '比赛', return: '回归', // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
  }
  const verdictText = (v: CoachVerdict): string => {
    if (!zh) return v.detail
    const p = v.params ?? {}
    const ps = v.paramsStr ?? {}
    const pct = (n?: number) => (n === undefined ? '?' : `${n >= 0 ? '+' : ''}${Math.round(n)}%`)
    const light = p.lightLookback ? '（上周偏轻，对比再前一周）' : '' // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
    const unit = ps.unit === 'km' ? 'km' : 'min'
    switch (v.rule) {
      case 'volume':
        if (p.noHistory) return '未提供历史，无法校验进展' // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (ps.role && p.change !== undefined && !('changeAvg' in p) && !('warnAt' in p) && !('failAt' in p) && !('twoWeeks' in p))
          return v.level === 'OK'
            ? `${roleZh[ps.role] ?? ps.role}周总量 ${pct(p.change)}` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
            : `${roleZh[ps.role] ?? ps.role}周但总量 ${pct(p.change)}，目标减 20–40%` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (p.twoWeeks !== undefined) return `两周增幅 ${pct(p.twoWeeks)}，应 ≤ +30%` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (p.failAt !== undefined)
          return `周总量 ${pct(p.change)}（${Math.round(p.base ?? 0)}→${Math.round(p.totalMin ?? 0)} min）${light}，上限 +${Math.round(p.failAt)}%` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (p.warnAt !== undefined) return `周总量 ${pct(p.change)}${light}，常规上限 +${Math.round(p.warnAt)}%` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (p.changeAvg !== undefined) return `周总量 ${pct(p.change)}${light}，对比 3 周均值 ${pct(p.changeAvg)}` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        return v.detail
      case 'deload-rhythm':
        return '连续 3 周爬升无轻周，建议安排减量周' // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
      case 'long-share':
        if (p.share === undefined) return v.detail
        return `长课占周时长 ${Math.round(p.share)}%（${Math.round(p.runs ?? 0)} 课时上限约 ${Math.round(p.limit)}%）` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
      case 'hard-days':
        if (ps.d1) return `背靠背硬课：${ps.d1} 与 ${ps.d2}` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (ps.long) return `长课 ${ps.long} 与硬课 ${ps.hard} 相邻` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (p.count !== undefined)
          return `高强度课 ${Math.round(p.count)} 次（${roleZh[ps.role ?? ''] ?? ps.role}周上限 ${Math.round(p.max ?? 0)}）` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        return v.detail
      case 'strength':
        return ps.date ? `力量课 ${ps.date} 在硬课/长课前一天` : v.detail // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
      case 'intensity':
        if (p.q === undefined) return v.detail
        return `X/Z3 以上时长占比约 ${Math.round(p.q)}%（目标 ≤20%，>25% 预警）` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
      case 'rest':
        if (p.days === undefined) return v.detail
        return `${Math.round(p.days)} 天无跑` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
      case 'spike':
        if (p.clean) return '没有课超过前 30 天最长课的 +10%' // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (ps.date && v.level === 'WARN')
          return `已完成 ${ps.date}：${Math.round(p.v ?? 0)}${unit} 比当时 30 天最长（${Math.round(p.ref ?? 0)}${unit}）长 ${pct(p.pct)}——比赛还是失控加量？` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (ps.date && ps.unit)
          return `${ps.date}：${Math.round(p.v ?? 0)}${unit} 比 30 天最长（${Math.round(p.ref ?? 0)}${unit}）长 ${pct(p.pct)}，单课应 ≤ +10%` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        if (p.longest !== undefined)
          return `最长课 ${Math.round(p.longest)} min 比近期最长（${Math.round(p.ref ?? 0)} min）高 ${pct(p.pct)}，单课应 ≤ +10%` // i18n-allow: 票 02 判定模板：zh 模板 + en 回落服务端英文 detail（两层输出设计）
        return v.detail
      default:
        return v.detail
    }
  }

  return (
    <section aria-label={t('coachPanelAria')} className="rounded-lg border border-border bg-card p-4">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-medium">{t('coachPanelTitle')}</h2>
        <span className="text-xs text-muted-foreground">
          {iso(weekStart)} ~ {iso(weekDates[6])}
        </span>
        <select
          aria-label={t('coachPanelWeekRole')}
          value={role}
          onChange={(e) => setRole(e.target.value)}
          className="ml-auto rounded border border-border bg-background px-2 py-1 text-xs"
        >
          {ROLES.map((r) => (
            <option key={r} value={r}>{r}</option>
          ))}
        </select>
      </div>

      {all.length === 0 && (
        <p className="mb-2 text-xs text-muted-foreground">
          {t('coachPanelEmpty')}
        </p>
      )}
      {all.length > 0 && (
        <ul className="mb-2 space-y-0.5 text-xs">
          {rows.map((row, i) => {
            const isCal = row.kind === 'cal'
            const u = row.unit
            const kmOf = u.km ?? 0
            const draftIdx = i - calendarUnits.length
            const inputCls =
              'w-16 rounded border border-border bg-background px-1 py-1 text-right font-mono'
            return (
              <li key={isCal ? `c${(u as CalUnit).activityId}` : `d${draftIdx}`} data-act={isCal ? (u as CalUnit).activityId : undefined} className="flex flex-wrap items-center gap-2">
                <select
                  value={u.date}
                  aria-label={t('coachPanelDraftDate')}
                  onChange={(e) => {
                    const v = e.target.value
                    if (isCal) {
                      void updateCalendarUnit(u as CalUnit, { date: v, minutes: u.minutes, km: kmOf })
                    } else {
                      setDrafts(drafts.map((d, j) => (j === draftIdx ? { ...d, date: v } : d)))
                      setVerdicts(null)
                    }
                  }}
                  className="rounded border border-border bg-background px-1 py-1"
                >
                  {weekDates.map((d) => (
                    <option key={iso(d)} value={iso(d)}>
                      {iso(d).slice(5)}
                    </option>
                  ))}
                </select>
                {isCal ? (
                  <>
                    <select
                      value={u.type}
                      aria-label={t('coachPanelType')}
                      onChange={(e) =>
                        void updateCalendarUnit(u as CalUnit, {
                          date: u.date,
                          minutes: u.minutes,
                          km: kmOf,
                          type: e.target.value,
                        })
                      }
                      className="rounded border border-border bg-background px-1 py-1"
                    >
                      {TYPES.map((tp) => (
                        <option key={tp} value={tp}>
                          {tp}
                        </option>
                      ))}
                    </select>
                    <input
                      type="number"
                      min={5}
                      defaultValue={u.minutes}
                      aria-label={t('coachPanelMinutes')}
                      className={inputCls}
                      onBlur={(e) =>
                        void updateCalendarUnit(u as CalUnit, {
                          date: u.date,
                          minutes: Math.max(5, Math.round(Number(e.target.value) || u.minutes)),
                          km: kmOf,
                        })
                      }
                    />
                    <span>min</span>
                    <input
                      type="number"
                      min={0}
                      step={0.5}
                      defaultValue={kmOf}
                      aria-label={t('coachPanelKm')}
                      className={inputCls}
                      onBlur={(e) =>
                        void updateCalendarUnit(u as CalUnit, {
                          date: u.date,
                          minutes: u.minutes,
                          km: Math.max(0, Number(e.target.value) || kmOf),
                        })
                      }
                    />
                    <span className="text-muted-foreground">{t('coachPanelCal')}</span>
                  </>
                ) : (
                  <>
                    <select
                      value={u.type}
                      aria-label={t('coachPanelType')}
                      onChange={(e) => {
                        const v = e.target.value
                        setDrafts(drafts.map((d, j) => (j === draftIdx ? { ...d, type: v } : d)))
                        setVerdicts(null)
                      }}
                      className="rounded border border-border bg-background px-1 py-1"
                    >
                      {TYPES.map((tp) => (
                        <option key={tp} value={tp}>
                          {tp}
                        </option>
                      ))}
                    </select>
                    <input
                      type="number"
                      min={5}
                      value={u.minutes}
                      aria-label={t('coachPanelMinutes')}
                      className={inputCls}
                      onChange={(e) => {
                        const v = Math.max(5, Math.round(Number(e.target.value) || 0))
                        setDrafts(drafts.map((d, j) => (j === draftIdx ? { ...d, minutes: v } : d)))
                        setVerdicts(null)
                      }}
                    />
                    <span>min</span>
                    <input
                      type="number"
                      min={0}
                      step={0.5}
                      value={u.km}
                      aria-label={t('coachPanelKm')}
                      className={inputCls}
                      onChange={(e) => {
                        const v = Math.max(0, Number(e.target.value) || 0)
                        setDrafts(drafts.map((d, j) => (j === draftIdx ? { ...d, km: v } : d)))
                        setVerdicts(null)
                      }}
                    />
                    <span>km</span>
                  </>
                )}
                <button
                  type="button"
                  onClick={() => {
                    if (isCal) {
                      void removeCalendarUnit(u as CalUnit)
                    } else {
                      setDrafts(drafts.filter((_, j) => j !== draftIdx))
                      // 草稿变了旧判定即作废——否则陈旧 FAIL/OK 会误导写入闸门
                      setVerdicts(null)
                    }
                  }}
                  className="cursor-pointer text-muted-foreground hover:text-destructive"
                  aria-label={isCal ? t('coachPanelRemoveCal') : t('coachPanelRemoveDraft')}
                  title={isCal ? t('coachPanelRemoveCalHint') : undefined}
                >
                  ×
                </button>
              </li>
            )
          })}
        </ul>
      )}

      <div className="mb-3 flex flex-wrap items-center gap-1 text-xs">
        <select
          aria-label={t('coachPanelDraftDate')}
          value={draft.date}
          onChange={(e) => setDraft({ ...draft, date: e.target.value })}
          className="rounded border border-border bg-background px-1 py-1"
        >
          {weekDates.map((d) => (
            <option key={iso(d)} value={iso(d)}>{iso(d).slice(5)}</option>
          ))}
        </select>
        <select
          aria-label={t('coachPanelType')}
          value={draft.type}
          onChange={(e) => setDraft({ ...draft, type: e.target.value })}
          className="rounded border border-border bg-background px-1 py-1"
        >
          {TYPES.map((tp) => (
            <option key={tp} value={tp}>{tp}</option>
          ))}
        </select>
        <input
          aria-label={t('coachPanelMinutes')}
          type="number"
          min={5}
          value={draft.minutes}
          onChange={(e) => setDraft({ ...draft, minutes: Number(e.target.value) })}
          className="w-16 rounded border border-border bg-background px-1 py-1"
        />
        <input
          aria-label={t('coachPanelKm')}
          type="number"
          min={0}
          step={0.5}
          value={draft.km}
          onChange={(e) => setDraft({ ...draft, km: Number(e.target.value) })}
          className="w-16 rounded border border-border bg-background px-1 py-1"
        />
        <button
          type="button"
          onClick={() => {
            setDrafts([...drafts, { ...draft }])
            setVerdicts(null)
          }}
          className="cursor-pointer rounded border border-border px-2 py-1 hover:border-secondary"
        >
          {t('coachPanelAddDraft')}
        </button>
        <button
          type="button"
          onClick={() => void check()}
          disabled={busy || all.length === 0}
          className="cursor-pointer rounded bg-primary px-3 py-1 font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? '…' : t('coachPanelCheck')}
        </button>
      </div>

      {error && <p className="mb-2 text-xs text-destructive">{error}</p>}

      {verdicts && (
        <ul aria-label={t('coachPanelVerdicts')} className="mb-2 space-y-1">
          {week && (
            <li className="text-xs text-muted-foreground">
              {week.runs} {t('coachPanelRuns')} · {kmFmt(week.total_km)} · {week.total_min} min ·{' '}
              {t('coachPanelHistory')} {week.history_sessions}
            </li>
          )}
          {verdicts.map((v, i) => (
            <li key={i} className="flex items-start gap-2 text-xs">
              <span
                className={`shrink-0 rounded border px-1.5 py-0.5 font-medium ${LEVEL_STYLE[v.level] ?? ''}`}
                title={v.evidence}
              >
                {v.level}
              </span>
              <span className="shrink-0 pt-0.5 text-muted-foreground">
                {(ruleLabel[v.rule] ?? [v.rule, v.rule])[zh ? 0 : 1]}
              </span>
              <span className="pt-0.5">{verdictText(v)}</span>
            </li>
          ))}
        </ul>
      )}

      {drafts.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => void writeCalendar()}
            disabled={busy || !verdicts || (hasFail && !override)}
            title={!verdicts ? t('coachPanelWriteTitle') : undefined}
            className="cursor-pointer rounded bg-primary px-3 py-1 text-xs font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {t('coachPanelWriteBtn')}
          </button>
          {hasFail && (
            <label className="flex items-center gap-1 text-xs text-muted-foreground">
              <input type="checkbox" checked={override} onChange={(e) => setOverride(e.target.checked)} />
              {t('coachPanelOverride')}
            </label>
          )}
          {written && <span className="text-xs text-secondary">{t('coachPanelWritten')}</span>}
        </div>
      )}
    </section>
  )
}
