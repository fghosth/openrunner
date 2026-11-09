import { useRef, useState } from 'react'
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
import CoachPacesCard from './CoachPacesCard'
import CsZoneEditor from './CsZoneEditor'
import ManualActivityForm from './ManualActivityForm'
import PlatformSettings from './PlatformSettings'
import RaceResultsEditor from './RaceResultsEditor'
import { readActiveAthlete, writeActiveAthlete } from '../athlete'
import { useLocale } from '../i18n'
import { GT } from '../glossary-term'

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
  const { t } = useLocale()
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
  const [recomputing, setRecomputing] = useState(false)
  const [recomputeArmed, setRecomputeArmed] = useState(false)
  const [recomputeMsg, setRecomputeMsg] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const importRef = useRef<HTMLInputElement>(null)
  const [importing, setImporting] = useState(false)
  const [importMsg, setImportMsg] = useState<{ ok: boolean; text: string } | null>(null)

  const activeAthlete = athleteList.find((a) => a.id === active) ?? null

  const [prevList, setPrevList] = useState(athleteList)
  if (athleteList !== prevList) {
    setPrevList(athleteList)
    setActive(readActiveAthlete())
  }

  // 切换档案时把资料表单同步成该档案的值（避免把 A 的头像存到 B 上）。
  // 渲染期调整：以持久化值拼 key，变了才回填
  const syncKey = [
    activeAthlete?.id,
    activeAthlete?.avatarUrl,
    activeAthlete?.bio,
    activeAthlete?.yearlyGoalKm,
    activeAthlete?.monthlyGoalKm,
    activeAthlete?.weeklyGoalKm,
  ].join('|')
  const [syncedKey, setSyncedKey] = useState<string | null>(null)
  if (syncKey !== syncedKey) {
    setSyncedKey(syncKey)
    setAvatarUrl(activeAthlete?.avatarUrl ?? '')
    setBio(activeAthlete?.bio ?? '')
    setYearlyGoal(activeAthlete?.yearlyGoalKm ?? 0)
    setMonthlyGoal(activeAthlete?.monthlyGoalKm ?? 0)
    setWeeklyGoal(activeAthlete?.weeklyGoalKm ?? 0)
  }

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
      setSaveMsg({ ok: true, text: t('settingsSaved') })
      setTimeout(() => setSaveMsg(null), 3000)
    } catch (err) {
      setSaveMsg({ ok: false, text: err instanceof Error ? err.message : t('settingsSaveFailed') })
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
      onError(err instanceof Error ? err.message : t('settingsUpdateFailed'))
    }
  }

  async function handleCreate() {
    if (!newName.trim()) return
    try {
      await athletes.create(newName.trim())
      setNewName('')
      await onAthletesChanged()
    } catch (err) {
      onError(err instanceof Error ? err.message : t('settingsCreateFailed'))
    }
  }

  async function handleManualCreate(input: ManualActivityInput) {
    const id = readActiveAthlete()
    if (!id) return
    onError(null)
    try {
      await activities.createManual(id, input)
    } catch (err) {
      onError(err instanceof Error ? err.message : t('settingsCreateFailed'))
    }
  }

  async function handleUpload(files: File[]) {
    const id = readActiveAthlete()
    if (!id) {
      onError(t('settingsNeedAthlete'))
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
      onError(err instanceof Error ? err.message : t('settingsImportFailed'))
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  // 指标口径变更后（例如 NP 改成 Coggan 30 秒窗口口径），历史活动不会自动重算。
  // 端点会返回「重算后值真的变了多少条」，所以点一次就能看出当前算法与存量数据的差距。
  async function handleRecomputeAll() {
    const id = readActiveAthlete()
    if (!id) {
      onError(t('settingsNeedAthlete'))
      return
    }
    if (!recomputeArmed) {
      setRecomputeArmed(true)
      setRecomputeMsg(null)
      return
    }
    setRecomputing(true)
    setRecomputeMsg(null)
    onError(null)
    try {
      const r = await activities.recomputeAll(id)
      setRecomputeMsg(
t('settingsRecomputeResult', { total: r.total, computed: r.computed, skipped: r.skipped, failed: r.failed }),
      )
    } catch (err) {
      onError(err instanceof Error ? err.message : t('settingsRecomputeFailed'))
    } finally {
      setRecomputing(false)
      setRecomputeArmed(false)
    }
  }

  // 导入导出归档（合并模式；服务端去重，重复导入幂等）
  async function handleImportFile(file: File) {
    const id = readActiveAthlete()
    if (!id) {
      onError(t('settingsNeedAthlete'))
      return
    }
    if (!confirm(t('settingsImportConfirm'))) return
    setImporting(true)
    setImportMsg(null)
    onError(null)
    try {
      const text = await file.text()
      const r = await activities.importJSON(id, text)
      setImportMsg({
        ok: true,
        text: t('settingsImportResult', {
          acts: r.activitiesImported,
          skipActs: r.activitiesSkipped,
          zones: r.zonesImported,
          skipZones: r.zonesSkipped,
          seasons: r.seasonsImported,
          races: r.racesImported,
        }),
      })
      await onAthletesChanged() // 档案设置（性别/目标/简介）可能已更新
    } catch (err) {
      setImportMsg({ ok: false, text: err instanceof Error ? err.message : t('settingsImportFailed') })
    } finally {
      setImporting(false)
      if (importRef.current) importRef.current.value = ''
    }
  }

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <h1 className="mb-6 text-2xl font-semibold">{t('settingsTitle')}</h1>

      {athleteList.length === 0 && (
        <p role="alert" className="mb-4 rounded border border-accent bg-card p-3 text-sm text-accent">
          {t('settingsNoAthlete')}
        </p>
      )}

      <section aria-label={t('settingsAthleteSection')} className="mb-6 rounded-lg border border-border bg-card p-4">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-muted-foreground">{t('settingsProfileLabel')}</span>
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
              title={t('settingsSexToggle')}
            >
              {t('settingsSex', { sex: athleteList.find((x) => x.id === active)?.sex === 'F' ? t('sexFemale') : t('sexMale') })}
            </button>
          )}
          {activeAthlete && (
            <div className="mt-3 w-full space-y-3 border-t border-border pt-3">
              <div>
                <div className="mb-1.5 text-xs font-medium text-foreground">{t('settingsPersonalProfile')}</div>
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    value={avatarUrl}
                    onChange={(e) => setAvatarUrl(e.target.value)}
                    placeholder={t('settingsAvatarPh')}
                    className="w-52 rounded border border-border bg-muted px-2 py-1 text-xs"
                  />
                  <input
                    value={bio}
                    onChange={(e) => setBio(e.target.value)}
                    placeholder={t('settingsBioPh')}
                    maxLength={120}
                    className="w-60 rounded border border-border bg-muted px-2 py-1 text-xs"
                  />
                </div>
              </div>

              <div>
                <div className="mb-1.5 text-xs font-medium text-foreground">
                  {t('settingsGoalsTitle')}
                  <span className="ml-2 font-normal text-muted-foreground">{t('settingsGoalsHint')}</span>
                </div>
                <div className="flex flex-wrap items-end gap-3">
                  {[
                    { label: t('settingsGoalYear'), value: yearlyGoal, which: 'year' as const },
                    { label: t('settingsGoalMonth'), value: monthlyGoal, which: 'month' as const },
                    { label: t('settingsGoalWeek'), value: weeklyGoal, which: 'week' as const },
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
                          aria-label={t('settingsGoalAria', { label: f.label })}
                        />
                        <span className="ml-1">km</span>
                      </span>
                    </label>
                  ))}
                  <span className="text-[11px] text-muted-foreground">{t('settingsGoalCalcHint')}</span>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <button
                  onClick={() => void handleSaveProfile()}
                  disabled={savingProfile}
                  className="cursor-pointer rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {savingProfile ? t('settingsSaving') : t('settingsSaveBtn')}
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
                onError(err instanceof Error ? err.message : t('settingsExportFailed'))
              }
            }}
            className="cursor-pointer rounded border border-border px-3 py-1 text-sm text-muted-foreground hover:text-foreground"
            title={t('settingsExportTitle')}
          >
            {t('settingsExportBtn')}
          </button>
          <button
            onClick={() => importRef.current?.click()}
            disabled={importing}
            className="cursor-pointer rounded border border-border px-3 py-1 text-sm text-muted-foreground hover:text-foreground disabled:opacity-50"
            title={t('settingsImportTitle')}
          >
            {importing ? t('settingsImporting') : t('settingsImportBtn')}
          </button>
          <input
            ref={importRef}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) void handleImportFile(f)
            }}
          />
          {importMsg && (
            <span
              role="status"
              className={`self-center rounded border px-2 py-1 text-xs ${
                importMsg.ok
                  ? 'border-border bg-muted/60 text-foreground'
                  : 'border-destructive bg-card text-destructive'
              }`}
            >
              {importMsg.text}
            </span>
          )}
          <div className="ml-2 flex gap-2">
            <input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
              placeholder={t('settingsNewNamePh')}
              className="w-40 rounded border border-border bg-muted px-2 py-1 text-sm focus-visible:outline-2 focus-visible:outline-ring"
            />
            <button
              onClick={handleCreate}
              disabled={!newName.trim()}
              className="cursor-pointer rounded border border-border px-3 py-1 text-sm hover:text-foreground disabled:opacity-50"
            >
              {t('settingsCreate')}
            </button>
          </div>
        </div>
      </section>

      <section aria-label={t('settingsImportSection')} className="mb-6 rounded-lg border border-border bg-card p-4">
        <label
          className={`flex cursor-pointer flex-col items-center justify-center gap-2 rounded border border-dashed border-border p-8 text-center hover:border-secondary ${
            uploading ? 'opacity-50' : ''
          }`}
        >
          <span className="text-sm">
            {uploading ? t('settingsParsing') : t('settingsImportDrop')}
          </span>
          <span className="text-xs text-muted-foreground">{t('settingsImportHint')}</span>
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
              {t('settingsBatchSummary', { imported: batch.imported, skipped: batch.skipped, failed: batch.failed })}
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
                {r.file} — {r.status === 'imported' ? t('settingsStatusOk') : r.status === 'skipped' ? t('settingsStatusSkipped') : t('settingsStatusFailed')}
                {r.reason ? `（${r.reason}）` : ''}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-label={t('settingsRecomputeTitle')} className="mb-6 rounded-lg border border-border bg-card p-4">
        <h2 className="mb-1 text-sm font-medium">{t('settingsRecomputeTitle')}</h2>
        <p className="mb-3 text-xs text-muted-foreground"><GT text={t('settingsRecomputeDesc')} /></p>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void handleRecomputeAll()}
            disabled={recomputing || !active}
            className="cursor-pointer rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {recomputing ? t('settingsRecomputing') : recomputeArmed ? t('settingsRecomputeConfirm') : t('settingsRecomputeBtn')}
          </button>
          {recomputeArmed && !recomputing && (
            <span className="text-xs text-muted-foreground">{t('settingsRecomputeArmed')}</span>
          )}
          {recomputeMsg && (
            <span role="status" className="text-xs text-secondary">
              {recomputeMsg}
            </span>
          )}
        </div>
      </section>

      <PlatformSettings />

      <HrZoneEditor />
      <PaceZoneEditor />
      <CoachPacesCard />
      <CsZoneEditor />

      <ManualActivityForm onSubmit={handleManualCreate} />

      <RaceResultsEditor />
    </main>
  )
}
