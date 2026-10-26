import { useEffect, useRef, useState } from 'react'
import {
  activities,
  athletes,
  downloadExport,
  type Athlete,
  type BatchResult,
  type ManualActivityInput,
} from '../api'
import HrZoneEditor from './HrZoneEditor'
import PaceZoneEditor from './PaceZoneEditor'
import CsZoneEditor from './CsZoneEditor'
import ManualActivityForm from './ManualActivityForm'
import PlatformSettings from './PlatformSettings'
import { readActiveAthlete, writeActiveAthlete } from '../App'

type Props = {
  athleteList: Athlete[]
  onAthletesChanged: () => Promise<unknown>
  onError: (msg: string | null) => void
}

/** 设置页：档案、导入、平台连接、各项区间参数。
 *
 * 从首页搬出来 —— 首页应当只承载「看数据」（概览 + 活动列表），
 * 配置类内容集中到这里，入口是导航右上角的齿轮图标。
 */
export default function Settings({ athleteList, onAthletesChanged, onError }: Props) {
  const [active, setActive] = useState<number | null>(readActiveAthlete())
  const [newName, setNewName] = useState('')
  const [avatarUrl, setAvatarUrl] = useState('')
  const [bio, setBio] = useState('')
  const [yearlyGoal, setYearlyGoal] = useState(0)
  const [monthlyGoal, setMonthlyGoal] = useState(0)
  const [weeklyGoal, setWeeklyGoal] = useState(0)
  const [savingProfile, setSavingProfile] = useState(false)
  const [saveMsg, setSaveMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [uploading, setUploading] = useState(false)
  const [batch, setBatch] = useState<BatchResult | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const activeAthlete = athleteList.find((a) => a.id === active) ?? null

  useEffect(() => {
    setActive(readActiveAthlete())
  }, [athleteList])

  // 切换档案时把资料表单同步成该档案的值（避免把 A 的头像存到 B 上）
  useEffect(() => {
    setAvatarUrl(activeAthlete?.avatarUrl ?? '')
    setBio(activeAthlete?.bio ?? '')
    setYearlyGoal(activeAthlete?.yearlyGoalKm ?? 0)
    setMonthlyGoal(activeAthlete?.monthlyGoalKm ?? 0)
    setWeeklyGoal(activeAthlete?.weeklyGoalKm ?? 0)
  }, [
    activeAthlete?.id,
    activeAthlete?.avatarUrl,
    activeAthlete?.bio,
    activeAthlete?.yearlyGoalKm,
    activeAthlete?.monthlyGoalKm,
    activeAthlete?.weeklyGoalKm,
  ])

  async function switchAthlete(id: number) {
    writeActiveAthlete(id)
    setActive(id)
  }

  // 目标联动：改任一目标时按比例推算另外两个（52 周/年、12 月/年）。
  // 只填一个就能得到合理的三档目标，不用心算。
  const setGoal = (which: 'year' | 'month' | 'week', v: number) => {
    const n = Number.isFinite(v) ? Math.max(0, Math.round(v)) : 0
    if (which === 'year') {
      setYearlyGoal(n)
      setMonthlyGoal(Math.round(n / 12))
      setWeeklyGoal(Math.round(n / 52))
    } else if (which === 'month') {
      setMonthlyGoal(n)
      setYearlyGoal(n * 12)
      setWeeklyGoal(Math.round((n * 12) / 52))
    } else {
      setWeeklyGoal(n)
      setYearlyGoal(n * 52)
      setMonthlyGoal(Math.round((n * 52) / 12))
    }
  }

  /** 保存资料 + 跑量目标（合并成一次请求），并给出可见反馈。 */
  async function handleSaveProfile() {
    if (!activeAthlete) return
    setSavingProfile(true)
    setSaveMsg(null)
    try {
      await athletes.rename(activeAthlete.id, activeAthlete.name, activeAthlete.sex, {
        avatarUrl,
        bio,
        yearlyGoalKm: yearlyGoal,
        monthlyGoalKm: monthlyGoal,
        weeklyGoalKm: weeklyGoal,
      })
      await onAthletesChanged()
      setSaveMsg({ ok: true, text: '✓ 已保存' })
      setTimeout(() => setSaveMsg(null), 3000)
    } catch (err) {
      setSaveMsg({ ok: false, text: err instanceof Error ? err.message : '保存失败' })
    } finally {
      setSavingProfile(false)
    }
  }

  async function handleSexToggle(a: Athlete) {
    const sex = a.sex === 'F' ? 'M' : 'F'
    try {
      await athletes.rename(a.id, a.name, sex)
      await onAthletesChanged()
    } catch (err) {
      onError(err instanceof Error ? err.message : '更新失败')
    }
  }

  async function handleCreate() {
    if (!newName.trim()) return
    try {
      await athletes.create(newName.trim())
      setNewName('')
      await onAthletesChanged()
    } catch (err) {
      onError(err instanceof Error ? err.message : '创建失败')
    }
  }

  async function handleManualCreate(input: ManualActivityInput) {
    const id = readActiveAthlete()
    if (!id) return
    onError(null)
    try {
      await activities.createManual(id, input)
    } catch (err) {
      onError(err instanceof Error ? err.message : '创建失败')
    }
  }

  async function handleUpload(files: File[]) {
    const id = readActiveAthlete()
    if (!id) {
      onError('请先创建并选择 Athlete 档案')
      return
    }
    if (files.length === 0) return
    setUploading(true)
    onError(null)
    setBatch(null)
    try {
      if (files.length === 1) {
        await activities.importFIT(id, files[0])
        setBatch({
          imported: 1,
          skipped: 0,
          failed: 0,
          results: [{ file: files[0].name, status: 'imported' }],
        })
      } else {
        setBatch(await activities.importBatch(id, files))
      }
    } catch (err) {
      onError(err instanceof Error ? err.message : '导入失败')
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <h1 className="mb-6 text-2xl font-semibold">设置</h1>

      {athleteList.length === 0 && (
        <p role="alert" className="mb-4 rounded border border-accent bg-card p-3 text-sm text-accent">
          ⚠️ 还没有 Athlete 档案 —— 请先在下方创建一个（例如「我」），
          否则凭证、区间都无法保存（数据按档案隔离）。
        </p>
      )}

      <section aria-label="Athlete 档案" className="mb-6 rounded-lg border border-border bg-card p-4">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-muted-foreground">档案：</span>
          {athleteList.map((a) => (
            <button
              key={a.id}
              onClick={() => switchAthlete(a.id)}
              className={`cursor-pointer rounded-full px-3 py-1 text-sm ${
                active === a.id
                  ? 'bg-secondary text-secondary-foreground'
                  : 'bg-muted text-muted-foreground hover:text-foreground'
              }`}
            >
              {a.name}
            </button>
          ))}
          {athleteList.length > 0 && active != null && (
            <button
              onClick={() => {
                const a = athleteList.find((x) => x.id === active)
                if (a) void handleSexToggle(a)
              }}
              className="cursor-pointer rounded border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
              title="切换性别（TRIMP Ksex 系数）"
            >
              性别：{athleteList.find((x) => x.id === active)?.sex === 'F' ? '女' : '男'}
            </button>
          )}
          {activeAthlete && (
            <div className="mt-3 w-full space-y-3 border-t border-border pt-3">
              <div>
                <div className="mb-1.5 text-xs font-medium text-foreground">个人资料</div>
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    value={avatarUrl}
                    onChange={(e) => setAvatarUrl(e.target.value)}
                    placeholder="头像图片 URL（可选）"
                    className="w-52 rounded border border-border bg-muted px-2 py-1 text-xs"
                  />
                  <input
                    value={bio}
                    onChange={(e) => setBio(e.target.value)}
                    placeholder="一句话简介（可选）"
                    maxLength={120}
                    className="w-60 rounded border border-border bg-muted px-2 py-1 text-xs"
                  />
                </div>
              </div>

              <div>
                <div className="mb-1.5 text-xs font-medium text-foreground">
                  跑量目标
                  <span className="ml-2 font-normal text-muted-foreground">
                    首页目标卡的进度条用这些值
                  </span>
                </div>
                <div className="flex flex-wrap items-end gap-3">
                  {[
                    { label: '年度目标', value: yearlyGoal, which: 'year' as const },
                    { label: '月度目标', value: monthlyGoal, which: 'month' as const },
                    { label: '周目标', value: weeklyGoal, which: 'week' as const },
                  ].map((f) => (
                    <label key={f.label} className="text-xs text-muted-foreground">
                      {f.label}
                      <span className="ml-1 inline-flex items-center">
                        <input
                          type="number"
                          min={0}
                          value={f.value}
                          onChange={(e) => setGoal(f.which, Number(e.target.value))}
                          className="w-20 rounded border border-border bg-muted px-2 py-1 text-right font-mono text-sm text-foreground"
                          aria-label={`${f.label}（公里）`}
                        />
                        <span className="ml-1">km</span>
                      </span>
                    </label>
                  ))}
                  <span className="text-[11px] text-muted-foreground">
                    改任一项会自动推算另两项（年 = 月×12 = 周×52）；填 0 表示不设目标
                  </span>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <button
                  onClick={() => void handleSaveProfile()}
                  disabled={savingProfile}
                  className="cursor-pointer rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {savingProfile ? '保存中…' : '保存资料与目标'}
                </button>
                {saveMsg && (
                  <span
                    role="status"
                    className={`rounded border px-2 py-1 text-xs ${
                      saveMsg.ok
                        ? 'border-border bg-muted/60 text-foreground'
                        : 'border-destructive bg-card text-destructive'
                    }`}
                  >
                    {saveMsg.text}
                  </span>
                )}
              </div>
            </div>
          )}
          <button
            onClick={async () => {
              const id = readActiveAthlete()
              if (!id) return
              try {
                await downloadExport(id)
              } catch (err) {
                onError(err instanceof Error ? err.message : '导出失败')
              }
            }}
            className="cursor-pointer rounded border border-border px-3 py-1 text-sm text-muted-foreground hover:text-foreground"
            title="导出当前档案全量数据（活动+Stream+指标+配置）为 JSON"
          >
            导出 JSON
          </button>
          <div className="ml-2 flex gap-2">
            <input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
              placeholder="新档案名称"
              className="w-40 rounded border border-border bg-muted px-2 py-1 text-sm focus-visible:outline-2 focus-visible:outline-ring"
            />
            <button
              onClick={handleCreate}
              disabled={!newName.trim()}
              className="cursor-pointer rounded border border-border px-3 py-1 text-sm hover:text-foreground disabled:opacity-50"
            >
              创建
            </button>
          </div>
        </div>
      </section>

      <section aria-label="导入 FIT" className="mb-6 rounded-lg border border-border bg-card p-4">
        <label
          className={`flex cursor-pointer flex-col items-center justify-center gap-2 rounded border border-dashed border-border p-8 text-center hover:border-secondary ${
            uploading ? 'opacity-50' : ''
          }`}
        >
          <span className="text-sm">
            {uploading ? '解析中…' : '拖入或点击选择 FIT / GPX / TCX 文件（可多选批量导入）'}
          </span>
          <span className="text-xs text-muted-foreground">按内容自动识别格式；重复文件自动跳过</span>
          <input
            ref={fileRef}
            type="file"
            accept=".fit,.gpx,.tcx"
            multiple
            disabled={uploading || !active}
            className="hidden"
            onChange={(e) => {
              const files = Array.from(e.target.files ?? [])
              if (files.length) void handleUpload(files)
            }}
          />
        </label>
        {batch && (
          <ul className="mt-2 space-y-0.5 text-xs">
            <li className="text-muted-foreground">
              导入 {batch.imported} · 跳过 {batch.skipped} · 失败 {batch.failed}
            </li>
            {batch.results.map((r, i) => (
              <li
                key={i}
                className={
                  r.status === 'imported'
                    ? 'text-secondary'
                    : r.status === 'skipped'
                      ? 'text-muted-foreground'
                      : 'text-destructive'
                }
              >
                {r.file} — {r.status === 'imported' ? '成功' : r.status === 'skipped' ? '跳过' : '失败'}
                {r.reason ? `（${r.reason}）` : ''}
              </li>
            ))}
          </ul>
        )}
      </section>

      <PlatformSettings />

      <HrZoneEditor />
      <PaceZoneEditor />
      <CsZoneEditor />

      <ManualActivityForm onSubmit={handleManualCreate} />
    </main>
  )
}
