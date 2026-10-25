import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { activities, athletes, downloadExport, type Activity, type Athlete, type BatchResult, type ManualActivityInput } from '../api'
import HrZoneEditor from './HrZoneEditor'
import PaceZoneEditor from './PaceZoneEditor'
import CsZoneEditor from './CsZoneEditor'
import ManualActivityForm from './ManualActivityForm'
import { readActiveAthlete, writeActiveAthlete } from '../App'
import { fmtDateTime, fmtDuration, sportNames } from '../format'

type Props = {
  athleteList: Athlete[]
  onAthletesChanged: () => Promise<unknown>
  onError: (msg: string | null) => void
}

/** 主页：档案切换 + FIT 上传 + 活动列表（票 03）。 */
export default function Home({ athleteList, onAthletesChanged, onError }: Props) {
  const [active, setActive] = useState<number | null>(readActiveAthlete())
  const [list, setList] = useState<Activity[]>([])
  const [newName, setNewName] = useState('')
  const [uploading, setUploading] = useState(false)
  const [batch, setBatch] = useState<BatchResult | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const refreshList = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) {
      setList([])
      return
    }
    try {
      setList(await activities.list(id))
    } catch (err) {
      onError(err instanceof Error ? err.message : '加载活动失败')
    }
  }, [onError])

  useEffect(() => {
    setActive(readActiveAthlete())
    void refreshList()
  }, [refreshList, athleteList])

  async function switchAthlete(id: number) {
    writeActiveAthlete(id)
    setActive(id)
    await refreshList()
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
      await refreshList()
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
        setBatch({ imported: 1, skipped: 0, failed: 0, results: [{ file: files[0].name, status: 'imported' }] })
      } else {
        setBatch(await activities.importBatch(id, files))
      }
      await refreshList()
    } catch (err) {
      onError(err instanceof Error ? err.message : '导入失败')
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
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

      <HrZoneEditor />
      <PaceZoneEditor />
      <CsZoneEditor />

      <ManualActivityForm onSubmit={handleManualCreate} />

      <section aria-label="活动列表" className="rounded-lg border border-border bg-card">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="px-4 py-3">日期</th>
              <th className="px-4 py-3">类型</th>
              <th className="px-4 py-3">距离</th>
              <th className="px-4 py-3">时长</th>
              <th className="px-4 py-3">爬升</th>
              <th className="px-4 py-3">来源</th>
            </tr>
          </thead>
          <tbody>
            {list.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">
                  还没有活动，先导入一个 FIT 文件
                </td>
              </tr>
            )}
            {list.map((a) => (
              <tr key={a.id} className="border-b border-border/50 hover:bg-muted/50">
                <td className="px-4 py-2">
                  <Link className="text-secondary hover:underline" to={`/activities/${a.id}`}>
                    {fmtDateTime(a.startTime)}
                  </Link>
                </td>
                <td className="px-4 py-2">
                  {sportNames[a.sport] ?? a.sport}
                  {a.isPlanned && (
                    <span className="ml-1 rounded bg-muted px-1 text-xs text-accent">计划</span>
                  )}
                </td>
                <td className="px-4 py-2 font-mono">{(a.distanceM / 1000).toFixed(2)} km</td>
                <td className="px-4 py-2 font-mono">{fmtDuration(a.durationSecs)}</td>
                <td className="px-4 py-2 font-mono">{a.elevationGainM.toFixed(0)} m</td>
                <td className="px-4 py-2 text-xs text-muted-foreground">{a.source}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </main>
  )
}
