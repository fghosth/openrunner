import { useCallback, useEffect, useState } from 'react'
import { platforms, type PlatformConnection, type SyncResult } from '../api'
import { readActiveAthlete } from '../App'

/** 云平台连接（票 15）：Garmin Personal API Key 配置 + 手动同步。 */
export default function PlatformSettings() {
  const [conns, setConns] = useState<PlatformConnection[]>([])
  const [apiKey, setApiKey] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [result, setResult] = useState<SyncResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) return
    try {
      setConns(await platforms.list(id))
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  async function save() {
    const id = readActiveAthlete()
    if (!id || !apiKey.trim()) return
    setError(null)
    setBusy('save')
    try {
      await platforms.upsert(id, 'garmin', apiKey.trim())
      setApiKey('')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setBusy(null)
    }
  }

  async function sync() {
    const id = readActiveAthlete()
    if (!id) return
    setError(null)
    setResult(null)
    setBusy('sync')
    try {
      setResult(await platforms.sync(id, 'garmin'))
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '同步失败')
    } finally {
      setBusy(null)
    }
  }

  async function remove() {
    const id = readActiveAthlete()
    if (!id) return
    if (!confirm('删除 Garmin 连接？凭证将被清除，需重新配置。')) return
    try {
      await platforms.remove(id, 'garmin')
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  const garmin = conns.find((c) => c.platform === 'garmin')

  return (
    <section aria-label="云平台连接" className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">
        云平台连接（Garmin Personal API）
      </h2>

      {error && (
        <p role="alert" className="mb-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {garmin ? (
        <div className="mb-3 space-y-1 text-sm">
          <p className="text-secondary">
            已连接 · 凭证已加密存储
            {garmin.syncCursor && <span className="ml-2 text-muted-foreground">游标 {garmin.syncCursor}</span>}
          </p>
          {garmin.lastSyncAt > 0 && (
            <p className="text-xs text-muted-foreground">
              上次同步 {new Date(garmin.lastSyncAt * 1000).toLocaleString('zh-CN', { hour12: false })}
            </p>
          )}
          {garmin.lastError && <p className="text-xs text-destructive">上次错误：{garmin.lastError}</p>}
          <div className="mt-2 flex gap-2">
            <button
              onClick={sync}
              disabled={busy === 'sync'}
              className="cursor-pointer rounded bg-primary px-4 py-1.5 font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
            >
              {busy === 'sync' ? '同步中…' : '立即同步'}
            </button>
            <button
              onClick={remove}
              className="cursor-pointer rounded border border-border px-3 py-1.5 text-muted-foreground hover:text-destructive"
            >
              删除连接
            </button>
          </div>
        </div>
      ) : (
        <p className="mb-3 text-sm text-muted-foreground">
          尚未连接。在{' '}
          <a
            href="https://connect.garmin.com/app/settings/personalApiToken"
            target="_blank"
            rel="noreferrer"
            className="text-secondary hover:underline"
          >
            Garmin Connect → 设置 → Personal API Key
          </a>{' '}
          生成密钥后填入下方。
        </p>
      )}

      {result && (
        <p className={`mb-3 text-sm ${result.status === 'ok' ? 'text-secondary' : 'text-destructive'}`}>
          同步{result.status === 'ok' ? '完成' : '失败'}：导入 {result.imported} · 跳过 {result.skipped} · 失败{' '}
          {result.failed}
          {result.message ? `（${result.message}）` : ''}
        </p>
      )}

      <div className="flex flex-wrap items-end gap-2 text-sm">
        <label className="text-muted-foreground">
          {garmin ? '更换 API Key' : 'API Key'}
          <input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="粘贴 Personal API Key"
            className="ml-1 w-64 rounded border border-border bg-muted px-2 py-1 font-mono"
          />
        </label>
        <button
          onClick={save}
          disabled={busy === 'save' || !apiKey.trim()}
          className="cursor-pointer rounded border border-border px-4 py-1.5 text-muted-foreground hover:text-foreground disabled:opacity-50"
        >
          {busy === 'save' ? '保存中…' : '保存'}
        </button>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        Key 以 AES-256-GCM 加密存储在本地数据库，不会回显。服务端每 6 小时自动同步一次（
        <code>OPENRUNNER_SYNC_INTERVAL</code> 可调，<code>OPENRUNNER_SYNC_ENABLED=false</code> 关闭）。
      </p>
    </section>
  )
}
