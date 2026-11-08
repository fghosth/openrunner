import { useMemo, useState } from 'react'
import { coach, activities as activitiesApi } from '../api'
import type { Activity, CoachPlanSession, CoachVerdict, CoachWeek } from '../api'
import { useLocale } from '../i18n'
import { readActiveAthlete } from '../App'
import { NOW } from '../clock'

/** 下周计划校验面板（coach-engine 票 03）。
 *
 * 数据流：日历里下周的 isPlanned 跑步 = 基线课表；用户可加草稿课；
 * 「校验」把两者合并发给 /coach/week-check（服务端引擎，与 skill 同一实现）；
 * 「写入日历」只落草稿——沿用 running-coach 的纪律：确认后且无 FAIL 才写。
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

export default function CoachWeekPanel({ activities }: { activities?: Activity[] }) {
  const { locale } = useLocale()
  const zh = locale === 'zh'

  const weekStart = useMemo(() => nextMonday(NOW), [])
  const weekDates = useMemo(
    () => Array.from({ length: 7 }, (_, i) => new Date(weekStart.getFullYear(), weekStart.getMonth(), weekStart.getDate() + i)),
    [weekStart],
  )
  const weekSet = useMemo(() => new Set(weekDates.map(iso)), [weekDates])

  // 日历里下周的计划跑步（服务端 week-check 的缺省 plan 来源，这里展示同一批）
  const calendarSessions: CoachPlanSession[] = useMemo(() => {
    const out: CoachPlanSession[] = []
    for (const a of activities ?? []) {
      if (!a.isPlanned || a.sport !== 'run') continue
      const d = new Date(a.startTime * 1000)
      const key = iso(d)
      if (!weekSet.has(key)) continue
      out.push({
        date: key,
        type: 'easy',
        minutes: Math.round((a.timerSecs || a.durationSecs) / 60),
        km: Math.round((a.distanceM / 1000) * 10) / 10,
      })
    }
    return out.sort((x, y) => x.date.localeCompare(y.date))
  }, [activities, weekSet])

  const [drafts, setDrafts] = useState<CoachPlanSession[]>([])
  const [role, setRole] = useState('build')
  const [verdicts, setVerdicts] = useState<CoachVerdict[] | null>(null)
  const [week, setWeek] = useState<CoachWeek | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [override, setOverride] = useState(false)
  const [written, setWritten] = useState(false)

  // 草稿编辑行
  const [draft, setDraft] = useState({ date: iso(weekDates[5]), type: 'easy', minutes: 60, km: 10 })

  const all = [...calendarSessions, ...drafts]
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

  async function writeCalendar() {
    const id = readActiveAthlete()
    if (!id) return
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
        })
      }
      setWritten(true)
      setDrafts([])
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const ruleLabel: Record<string, [string, string]> = {
    volume: ['周增量', 'Volume'],
    'deload-rhythm': ['减量节奏', 'Deload rhythm'],
    spike: ['单次 +10%', 'Single-run spike'],
    'long-share': ['长课占比', 'Long-run share'],
    'hard-days': ['硬课安排', 'Hard days'],
    strength: ['力量课位置', 'Strength placement'],
    intensity: ['强度分布', 'Intensity split'],
    rest: ['休息日', 'Rest days'],
    input: ['输入', 'Input'],
  }

  return (
    <section aria-label={zh ? '下周计划校验' : 'Next-week plan check'} className="rounded-lg border border-border bg-card p-4">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-medium">{zh ? '下周计划校验' : 'Check next week'}</h2>
        <span className="text-xs text-muted-foreground">
          {iso(weekStart)} ~ {iso(weekDates[6])}
        </span>
        <select
          aria-label={zh ? '周角色' : 'Week role'}
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
          {zh ? '下周还没有计划课。添加草稿，或先在日历里安排。' : 'No planned runs next week. Add drafts or plan on the calendar.'}
        </p>
      )}
      {all.length > 0 && (
        <ul className="mb-2 space-y-0.5 text-xs">
          {all.map((s, i) => (
            <li key={i} className="flex items-center gap-2">
              <span className="w-20 text-muted-foreground">{s.date.slice(5)}</span>
              <span className="w-20">{s.type}</span>
              <span className="w-16 text-right">{s.minutes} min</span>
              <span className="w-16 text-right">{s.km} km</span>
              {i < calendarSessions.length ? (
                <span className="text-muted-foreground">{zh ? '日历' : 'cal'}</span>
              ) : (
                <button
                  type="button"
                  onClick={() => setDrafts(drafts.filter((_, j) => j !== i - calendarSessions.length))}
                  className="cursor-pointer text-muted-foreground hover:text-destructive"
                  aria-label={zh ? '删除草稿' : 'Remove draft'}
                >
                  ×
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      <div className="mb-3 flex flex-wrap items-center gap-1 text-xs">
        <select
          aria-label={zh ? '草稿日期' : 'Draft date'}
          value={draft.date}
          onChange={(e) => setDraft({ ...draft, date: e.target.value })}
          className="rounded border border-border bg-background px-1 py-1"
        >
          {weekDates.map((d) => (
            <option key={iso(d)} value={iso(d)}>{iso(d).slice(5)}</option>
          ))}
        </select>
        <select
          aria-label={zh ? '课型' : 'Type'}
          value={draft.type}
          onChange={(e) => setDraft({ ...draft, type: e.target.value })}
          className="rounded border border-border bg-background px-1 py-1"
        >
          {TYPES.map((tp) => (
            <option key={tp} value={tp}>{tp}</option>
          ))}
        </select>
        <input
          aria-label={zh ? '分钟' : 'Minutes'}
          type="number"
          min={5}
          value={draft.minutes}
          onChange={(e) => setDraft({ ...draft, minutes: Number(e.target.value) })}
          className="w-16 rounded border border-border bg-background px-1 py-1"
        />
        <input
          aria-label={zh ? '公里' : 'Kilometres'}
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
          {zh ? '加草稿' : '+ draft'}
        </button>
        <button
          type="button"
          onClick={() => void check()}
          disabled={busy || all.length === 0}
          className="cursor-pointer rounded bg-primary px-3 py-1 font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? '…' : zh ? '校验' : 'Check'}
        </button>
      </div>

      {error && <p className="mb-2 text-xs text-destructive">{error}</p>}

      {verdicts && (
        <ul aria-label={zh ? '校验结果' : 'Verdicts'} className="mb-2 space-y-1">
          {week && (
            <li className="text-xs text-muted-foreground">
              {week.runs} {zh ? '课' : 'runs'} · {week.total_km} km · {week.total_min} min ·{' '}
              {zh ? '历史' : 'history'} {week.history_sessions}
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
              <span className="pt-0.5">{v.detail}</span>
            </li>
          ))}
        </ul>
      )}

      {drafts.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => void writeCalendar()}
            disabled={busy || (hasFail && !override)}
            className="cursor-pointer rounded bg-primary px-3 py-1 text-xs font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {zh ? '把草稿写入日历' : 'Write drafts to calendar'}
          </button>
          {hasFail && (
            <label className="flex items-center gap-1 text-xs text-muted-foreground">
              <input type="checkbox" checked={override} onChange={(e) => setOverride(e.target.checked)} />
              {zh ? '存在 FAIL，仍要写入' : 'There are FAILs, write anyway'}
            </label>
          )}
          {written && <span className="text-xs text-secondary">{zh ? '已写入日历' : 'Written'}</span>}
        </div>
      )}
    </section>
  )
}
