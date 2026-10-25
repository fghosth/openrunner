import { useCallback, useEffect, useState } from 'react'
import { platforms, type PlatformConnection, type SyncResult } from '../api'
import { readActiveAthlete } from '../App'

type Mode = 'global' | 'cn'

/** 云平台连接（票 15）：Garmin 国际站（Personal API Key）/ 中国站（OAuth1 会话）。 */
export default function PlatformSettings() {
  const [conns, setConns] = useState<PlatformConnection[]>([])
  const [mode, setMode] = useState<Mode>('global')
  const [apiKey, setApiKey] = useState('')
  const [oToken, setOToken] = useState('')
  const [oSecret, setOSecret] = useState('')
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
    if (!id) return
    setError(null)
    setBusy('save')
    try {
      if (mode === 'global') {
        if (!apiKey.trim()) return
        await platforms.upsert(id, 'garmin', { apiKey: apiKey.trim(), region: 'global' })
        setApiKey('')
      } else {
        if (!oToken.trim() || !oSecret.trim()) return
        await platforms.upsert(id, 'garmin', {
          oauth1Token: oToken.trim(),
          oauth1Secret: oSecret.trim(),
          region: 'cn',
        })
        setOToken('')
        setOSecret('')
      }
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
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">云平台连接（Garmin）</h2>

      {error && (
        <p role="alert" className="mb-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {garmin ? (
        <div className="mb-3 space-y-1 text-sm">
          <p className="text-secondary">
            已连接 · {garmin.region === 'cn' ? '中国站（garmin.cn）' : '国际站'} · 凭证已加密存储
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
        <p className="mb-3 text-sm text-muted-foreground">尚未连接。选择账号所在站点后填入凭证。</p>
      )}

      {result && (
        <p className={`mb-3 text-sm ${result.status === 'ok' ? 'text-secondary' : 'text-destructive'}`}>
          同步{result.status === 'ok' ? '完成' : '失败'}：导入 {result.imported} · 跳过 {result.skipped} · 失败{' '}
          {result.failed}
          {result.message ? `（${result.message}）` : ''}
        </p>
      )}

      <div className="mb-3 flex gap-2 text-sm">
        {(['global', 'cn'] as Mode[]).map((m) => (
          <button
            key={m}
            onClick={() => setMode(m)}
            className={`cursor-pointer rounded-full px-3 py-1 ${
              mode === m ? 'bg-secondary text-secondary-foreground' : 'bg-muted text-muted-foreground'
            }`}
          >
            {m === 'global' ? '国际站（API Key）' : '中国站（OAuth1）'}
          </button>
        ))}
      </div>

      {mode === 'global' ? (
        <div className="flex flex-wrap items-end gap-2 text-sm">
          <label className="text-muted-foreground">
            Personal API Key
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="在 connect.garmin.com 生成"
              className="ml-1 w-64 rounded border border-border bg-muted px-2 py-1 font-mono"
            />
          </label>
          <button
            onClick={save}
            disabled={busy === 'save' || !apiKey.trim()}
            className="cursor-pointer rounded border border-border px-4 py-1.5 text-muted-foreground hover:text-foreground disabled:opacity-50"
          >
            保存
          </button>
          <p className="w-full text-xs text-muted-foreground">
            在{' '}
            <a
              href="https://connect.garmin.com/app/settings/personalApiToken"
              target="_blank"
              rel="noreferrer"
              className="text-secondary hover:underline"
            >
              Garmin Connect → 设置 → Personal API Key
            </a>{' '}
            生成（30 天~1 年有效）。
          </p>
        </div>
      ) : (
        <div className="space-y-2 text-sm">
          <pre className="overflow-x-auto rounded border border-border bg-muted p-2 text-xs text-muted-foreground">
{`# 在你本机运行一次（需 python3 + garth）
python3 -m pip install garth
python3 scripts/garmin-cn-token.py 你的账号 你的密码`}
          </pre>
          <div className="flex flex-wrap items-end gap-2">
            <label className="text-muted-foreground">
              oauth1_token
              <input
                type="password"
                value={oToken}
                onChange={(e) => setOToken(e.target.value)}
                className="ml-1 w-52 rounded border border-border bg-muted px-2 py-1 font-mono"
              />
            </label>
            <label className="text-muted-foreground">
              oauth1_secret
              <input
                type="password"
                value={oSecret}
                onChange={(e) => setOSecret(e.target.value)}
                className="ml-1 w-52 rounded border border-border bg-muted px-2 py-1 font-mono"
              />
            </label>
            <button
              onClick={save}
              disabled={busy === 'save' || !oToken.trim() || !oSecret.trim()}
              className="cursor-pointer rounded border border-border px-4 py-1.5 text-muted-foreground hover:text-foreground disabled:opacity-50"
            >
              保存
            </button>
          </div>
          <p className="text-xs text-muted-foreground">
            中国站未开通官方 Personal API，改用网页会话凭据：脚本登录一次输出长期有效的
            OAuth1 token/secret，服务端每次同步自动换取 24 小时 OAuth2 token（无需反复运行脚本）。
          </p>
        </div>
      )}
      <p className="mt-2 text-xs text-muted-foreground">
        所有凭证以 AES-256-GCM 加密存储，不会回显。服务端每 6 小时自动同步（
        <code>OPENRUNNER_SYNC_INTERVAL</code> 可调，<code>OPENRUNNER_SYNC_ENABLED=false</code> 关闭）。
      </p>
    </section>
  )
}
