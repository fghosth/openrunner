import { useCallback, useEffect, useState } from 'react'
import { athletes, getToken, setToken, clearToken, type Athlete, type AuthStatus, auth } from './api'
import CredentialForm from './CredentialForm'

type Phase = 'loading' | 'setup' | 'login' | 'ready'

const ACTIVE_ATHLETE_KEY = 'openrunner.activeAthlete'

/**
 * 应用外壳（票 02）：首启设置 → 登录 → 主页（Athlete 档案管理/切换）。
 * 功能页随票 03+ 逐步挂进 ready 分支。
 */
export default function App() {
  const [phase, setPhase] = useState<Phase>('loading')
  const [athletesList, setAthletesList] = useState<Athlete[]>([])
  const [active, setActive] = useState<number | null>(null)
  const [newName, setNewName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const refreshAthletes = useCallback(async () => {
    try {
      const list = await athletes.list()
      setAthletesList(list)
      const stored = Number(localStorage.getItem(ACTIVE_ATHLETE_KEY))
      setActive(stored && list.some((a) => a.ID === stored) ? stored : (list[0]?.ID ?? null))
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    }
  }, [])

  useEffect(() => {
    ;(async () => {
      try {
        const status: AuthStatus = await auth.status()
        if (status.needsSetup) {
          setPhase('setup')
          return
        }
        if (!getToken()) {
          setPhase('login')
          return
        }
        await refreshAthletes()
        setPhase('ready')
      } catch {
        setError('后端不可达')
        setPhase('login') // 后端离线时仍可展示登录页
      }
    })()
  }, [refreshAthletes])

  async function handleSetup(username: string, password: string) {
    const { token } = await auth.setup(username, password)
    setToken(token)
    setPhase('ready')
  }

  async function handleLogin(username: string, password: string) {
    const { token } = await auth.login(username, password)
    setToken(token)
    await refreshAthletes()
    setPhase('ready')
  }

  function handleLogout() {
    clearToken()
    setPhase('login')
  }

  async function handleCreate() {
    if (!newName.trim()) return
    await athletes.create(newName.trim())
    setNewName('')
    await refreshAthletes()
  }

  async function handleRename(a: Athlete) {
    const name = prompt('新名称', a.Name)
    if (!name || name === a.Name) return
    await athletes.rename(a.ID, name)
    await refreshAthletes()
  }

  async function handleRemove(a: Athlete) {
    if (!confirm(`删除档案「${a.Name}」？其下活动数据的级联策略未定，当前仅删档案。`)) return
    await athletes.remove(a.ID)
    await refreshAthletes()
  }

  if (phase === 'loading') {
    return <div className="grid min-h-screen place-items-center bg-background text-muted-foreground">加载中…</div>
  }

  if (phase === 'setup') {
    return <CredentialForm title="首次设置 · 创建管理员账号" submitLabel="创建" submit={handleSetup} />
  }

  if (phase === 'login') {
    return <CredentialForm title="Openrunner 登录" submitLabel="登录" submit={handleLogin} />
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border bg-card">
        <div className="mx-auto flex max-w-6xl items-center gap-3 px-6 py-4">
          <span className="font-mono text-lg font-semibold tracking-tight">
            Open<span className="text-secondary">runner</span>
          </span>
          <span className="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">ticket 02</span>
          <button
            onClick={handleLogout}
            className="ml-auto cursor-pointer rounded border border-border px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground"
          >
            登出
          </button>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-10">
        {error && (
          <p role="alert" className="mb-6 rounded border border-destructive bg-card p-3 text-sm text-destructive">
            {error}
          </p>
        )}

        <section aria-label="运动员档案" className="rounded-lg border border-border bg-card p-5">
          <h2 className="mb-4 text-sm font-medium text-muted-foreground">Athlete 档案</h2>

          {athletesList.length === 0 ? (
            <p className="mb-4 text-sm text-muted-foreground">还没有档案，创建第一个：</p>
          ) : (
            <ul className="mb-4 divide-y divide-border">
              {athletesList.map((a) => (
                <li key={a.ID} className="flex items-center gap-3 py-2">
                  <label className="flex flex-1 cursor-pointer items-center gap-2">
                    <input
                      type="radio"
                      name="active-athlete"
                      checked={active === a.ID}
                      onChange={() => {
                        setActive(a.ID)
                        localStorage.setItem(ACTIVE_ATHLETE_KEY, String(a.ID))
                      }}
                      className="accent-secondary"
                    />
                    <span className={active === a.ID ? 'font-medium' : 'text-muted-foreground'}>{a.Name}</span>
                    {active === a.ID && <span className="text-xs text-secondary">当前</span>}
                  </label>
                  <button
                    onClick={() => handleRename(a)}
                    className="cursor-pointer text-sm text-muted-foreground hover:text-foreground"
                  >
                    重命名
                  </button>
                  <button
                    onClick={() => handleRemove(a)}
                    className="cursor-pointer text-sm text-muted-foreground hover:text-destructive"
                  >
                    删除
                  </button>
                </li>
              ))}
            </ul>
          )}

          <div className="flex gap-2">
            <input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
              placeholder="新档案名称，如：我-公路"
              className="w-64 rounded border border-border bg-muted px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-ring"
            />
            <button
              onClick={handleCreate}
              disabled={!newName.trim()}
              className="cursor-pointer rounded bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
            >
              创建档案
            </button>
          </div>
        </section>
      </main>
    </div>
  )
}
