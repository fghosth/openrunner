import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  platforms,
  platformMetas,
  type PlatformConnection,
  type PlatformMeta,
  type SyncResult,
} from '../api'
import { readActiveAthlete } from '../athlete'
import { useLocale, useServerText } from '../i18n'

/** 云平台连接（多平台）：凭证表单按平台元信息动态渲染。
 *
 * 每个平台的凭证字段差异很大（Garmin 的站点/token、Intervals 的 athleteId/key、
 * Strava 的三件套、COROS 的账号密码），硬编码会让增删平台都要改前端。
 * 因此后端 `/api/platforms` 返回字段定义，这里只负责渲染与提交。
 */
export default function PlatformSettings() {
  const { t } = useLocale()
  const serverText = useServerText()
  const [metas, setMetas] = useState<PlatformMeta[]>([])
  const [conns, setConns] = useState<PlatformConnection[]>([])
  const [selected, setSelected] = useState<string>('')
  const [form, setForm] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState<'save' | 'sync' | 'remove' | 'full' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<SyncResult | null>(null)
  const [savedMsg, setSavedMsg] = useState<string | null>(null)

  const activeMeta = useMemo(() => metas.find((m) => m.name === selected), [metas, selected])
  const connOf = (name: string) => conns.find((c) => c.platform === name)

  const refresh = useCallback(() =>
      // setState 推迟到微任务：effect 调用栈内同步 setState 命中 set-state-in-effect
      Promise.resolve().then(async () => {
      const id = readActiveAthlete()
      if (!id) {
        setError(t('settingsNeedAthlete'))
        return
      }
      try {
        const [m, c] = await Promise.all([platformMetas(), platforms.list(id)])
        setMetas(m)
        setConns(c)
        if (!selected && m.length > 0) setSelected(m[0].name)
      } catch (err) {
        setError(err instanceof Error ? err.message : t('commonLoadFailed'))
      }
        }), [selected, t])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // 切换平台时清空表单（凭证不跨平台复用）——渲染期调整
  const [prevSelected, setPrevSelected] = useState(selected)
  if (selected !== prevSelected) {
    setPrevSelected(selected)
    setForm({})
    setResult(null)
    setSavedMsg(null)
  }

  async function save() {
    const id = readActiveAthlete()
    if (!id || !activeMeta) return
    // 只提交填了的字段（可选字段留空=不传）
    const payload: Record<string, string> = {}
    for (const f of activeMeta.fields) {
      const v = form[f.key]?.trim()
      if (v) payload[f.key] = v
    }
    if (Object.keys(payload).length === 0) {
      setError(t('platformNeedCred'))
      return
    }
    setBusy('save')
    setError(null)
    setSavedMsg(null)
    try {
      await platforms.save(id, activeMeta.name, payload)
      setForm({})
      setSavedMsg(t('platformCredSaved'))
      setTimeout(() => setSavedMsg(null), 3000)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('commonSaveFailed'))
    } finally {
      setBusy(null)
    }
  }

  async function sync(full = false) {
    const id = readActiveAthlete()
    if (!id || !activeMeta) return
    setBusy(full ? 'full' : 'sync')
    setError(null)
    setResult(null)
    try {
      setResult(await platforms.sync(id, activeMeta.name, full))
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('platformSyncFailed'))
    } finally {
      setBusy(null)
    }
  }

  async function remove() {
    const id = readActiveAthlete()
    if (!id || !activeMeta) return
    if (!confirm(t('platformDeleteConfirm', { label: activeMeta.label }))) return
    setBusy('remove')
    try {
      await platforms.remove(id, activeMeta.name)
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('commonDeleteFailed'))
    } finally {
      setBusy(null)
    }
  }

  const conn = activeMeta ? connOf(activeMeta.name) : undefined

  return (
    <section aria-label={t('platformAria')} className="mb-6 rounded-lg border border-border bg-card p-4">
      <h2 className="mb-3 text-sm font-medium text-muted-foreground">{t('platformTitle')}</h2>

      {/* 平台切换 */}
      <div className="mb-3 flex flex-wrap gap-1">
        {metas.map((m) => {
          const c = connOf(m.name)
          return (
            <button
              key={m.name}
              onClick={() => setSelected(m.name)}
              aria-pressed={selected === m.name}
              className={`cursor-pointer rounded-full px-3 py-1 text-xs ${
                selected === m.name
                  ? 'bg-secondary text-secondary-foreground'
                  : 'bg-muted text-muted-foreground hover:text-foreground'
              }`}
            >
              {serverText(m.labelKey, m.label)}
              {c && <span className="ml-1 opacity-70">·</span>}
            </button>
          )
        })}
      </div>

      {error && (
        <p role="alert" className="mb-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {activeMeta && (
        <>
          <p className="mb-3 text-xs leading-relaxed text-muted-foreground">
            {serverText(activeMeta.helpKey, activeMeta.help)}
            {activeMeta.setupUrl && (
              <>
                {' '}
                <a
                  href={activeMeta.setupUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="text-secondary hover:underline"
                >
                  {t('platformGetCred')}
                </a>
              </>
            )}
          </p>

          {/* 连接状态 */}
          <div className="mb-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            {conn ? (
              <>
                <span className="text-emerald-500">{t('platformConnected')}</span>
                {conn.region && <span>· {conn.region}</span>}
                {conn.syncCursor && <span>{t('platformCursor', { cursor: conn.syncCursor })}</span>}
                {conn.lastSyncAt > 0 && (
                  <span>{t('platformLastSync', { time: new Date(conn.lastSyncAt * 1000).toLocaleString('zh-CN', { hour12: false }) })}</span>
                )}
                {conn.lastError && <span className="text-destructive">· {conn.lastError}</span>}
              </>
            ) : (
              <span>{t('platformNotConnected')}</span>
            )}
          </div>

          {/* 动态凭证表单 */}
          <div className="flex flex-wrap items-end gap-2">
            {activeMeta.fields.map((f) => (
              <label key={f.key} className="text-xs text-muted-foreground">
                {serverText(f.labelKey, f.label)}
                {f.type === 'select' ? (
                  <select
                    value={form[f.key] ?? f.options?.[0]?.value ?? ''}
                    onChange={(e) => setForm((s) => ({ ...s, [f.key]: e.target.value }))}
                    className="ml-1 rounded border border-border bg-muted px-2 py-1 text-sm"
                  >
                    {f.options?.map((o) => (
                      <option key={o.value} value={o.value}>
                        {serverText(o.labelKey, o.label)}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    type={f.type === 'password' ? 'password' : 'text'}
                    value={form[f.key] ?? ''}
                    onChange={(e) => setForm((s) => ({ ...s, [f.key]: e.target.value }))}
                    placeholder={f.placeholder ?? ''}
                    title={serverText(f.helpKey, f.help)}
                    className="ml-1 w-40 rounded border border-border bg-muted px-2 py-1 text-sm"
                  />
                )}
              </label>
            ))}
          </div>
          <p className="mt-1 text-[11px] text-muted-foreground">
            {t('platformEncrypted')}
          </p>

          <div className="mt-3 flex flex-wrap items-center gap-2">
            <button
              onClick={() => void save()}
              disabled={busy !== null}
              className="cursor-pointer rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
            >
              {busy === 'save' ? t('commonSaving') : t('platformSaveCred')}
            </button>
            <button
              onClick={() => void sync(false)}
              disabled={!conn || busy !== null}
              title={conn ? t('platformSyncTitle') : t('platformSaveFirst')}
              className="cursor-pointer rounded border border-border px-3 py-1.5 text-xs text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
            >
              {busy === 'sync' ? t('platformSyncing') : t('platformSync')}
            </button>
            <button
              onClick={() => void sync(true)}
              disabled={!conn || busy !== null}
              title={t('platformFullTitle')}
              className="cursor-pointer rounded border border-border px-3 py-1.5 text-xs text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
            >
              {busy === 'full' ? t('platformSyncing') : t('platformFull')}
            </button>
            {conn && (
              <button
                onClick={() => void remove()}
                disabled={busy !== null}
                className="cursor-pointer rounded border border-border px-3 py-1.5 text-xs text-muted-foreground hover:text-destructive disabled:opacity-50"
              >
                {t('platformDeleteConn')}
              </button>
            )}
            {savedMsg && <span className="text-xs text-emerald-500">{savedMsg}</span>}
          </div>

          {result && (
            <p
              role="status"
              className={`mt-2 rounded border px-2 py-1 text-xs ${
                result.status === 'ok'
                  ? 'border-border bg-muted/60 text-foreground'
                  : 'border-destructive bg-card text-destructive'
              }`}
            >
              {t('platformSyncResult', {
                status: result.status === 'ok' ? t('platformStatusOk') : t('platformStatusFail'),
                imported: result.imported,
                skipped: result.skipped,
                failed: result.failed,
              })}
              {result.message && ` · ${result.message}`}
            </p>
          )}
        </>
      )}
    </section>
  )
}
