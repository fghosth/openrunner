import { Component, useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, Navigate, Route, Routes } from 'react-router-dom'
import ThemeToggle from './ThemeToggle'
import {
  athletes,
  clearToken,
  getToken,
  ApiError,
  setToken,
  type Athlete,
  type AuthStatus,
  auth,
} from './api'
import CredentialForm from './CredentialForm'
import Home from './pages/Home'
import Trends from './pages/Trends'
import MapView, { StatsView } from './pages/MapStats'
import ActivityDetail from './pages/ActivityDetail'

const ACTIVE_ATHLETE_KEY = 'openrunner.activeAthlete'

export function readActiveAthlete(): number | null {
  const v = Number(localStorage.getItem(ACTIVE_ATHLETE_KEY))
  return Number.isFinite(v) && v > 0 ? v : null
}

export function writeActiveAthlete(id: number) {
  localStorage.setItem(ACTIVE_ATHLETE_KEY, String(id))
}

type Phase = 'loading' | 'setup' | 'login' | 'ready'

/** 错误边界：单个页面崩溃时显示错误而非整页白屏（便于定位）。 */
class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error) {
      return (
        <main className="mx-auto max-w-4xl px-6 py-10">
          <p role="alert" className="rounded border border-destructive bg-card p-4 text-sm text-destructive">
            页面渲染出错：{this.state.error.message}
          </p>
          <pre className="mt-3 overflow-x-auto rounded bg-muted p-3 text-xs text-muted-foreground">
            {this.state.error.stack?.split('\n').slice(0, 5).join('\n')}
          </pre>
          <button
            onClick={() => this.setState({ error: null })}
            className="mt-3 cursor-pointer rounded border border-border px-3 py-1.5 text-sm"
          >
            重试
          </button>
        </main>
      )
    }
    return this.props.children
  }
}

/**
 * 应用外壳（票 02 鉴权 + 票 03 路由化）：
 * 首启设置 → 登录 → 主应用（上传/活动列表/详情随票增长）。
 */
export default function App() {
  const [phase, setPhase] = useState<Phase>('loading')
  const [athleteList, setAthleteList] = useState<Athlete[]>([])
  const [error, setError] = useState<string | null>(null)

  const refreshAthletes = useCallback(async (): Promise<Athlete[]> => {
    const list = await athletes.list()
    setAthleteList(list)
    const stored = readActiveAthlete()
    if (!stored || !list.some((a) => a.id === stored)) {
      if (list[0]) writeActiveAthlete(list[0].id)
    }
    return list
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
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          clearToken()
          setPhase('login')
          return
        }
        setError(err instanceof Error ? err.message : '后端不可达')
        setPhase('login')
      }
    })()
  }, [refreshAthletes])

  function handleAuthSuccess() {
    refreshAthletes()
      .then(() => setPhase('ready'))
      .catch(() => setPhase('ready'))
  }

  if (phase === 'loading') {
    return <div className="grid min-h-screen place-items-center bg-background text-muted-foreground">加载中…</div>
  }

  if (phase === 'setup') {
    return (
      <CredentialForm
        title="首次设置 · 创建管理员账号"
        submitLabel="创建"
        submit={async (u, p) => {
          const { token } = await auth.setup(u, p)
          setToken(token)
          handleAuthSuccess()
        }}
      />
    )
  }

  if (phase === 'login') {
    return (
      <CredentialForm
        title="Openrunner 登录"
        submitLabel="登录"
        submit={async (u, p) => {
          const { token } = await auth.login(u, p)
          setToken(token)
          handleAuthSuccess()
        }}
      />
    )
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border bg-card">
        <div className="mx-auto flex max-w-6xl items-center gap-4 px-6 py-4">
          <span className="font-mono text-lg font-semibold tracking-tight">
            Open<span className="text-secondary">runner</span>
          </span>
          <nav className="flex gap-4 text-sm text-muted-foreground">
            <Link className="hover:text-foreground" to="/">
              活动
            </Link>
            <Link className="hover:text-foreground" to="/trends">
              趋势
            </Link>
            <Link className="hover:text-foreground" to="/map">
              地图
            </Link>
            <Link className="hover:text-foreground" to="/stats">
              年度
            </Link>
          </nav>
          <ThemeToggle />
          <button
            onClick={() => {
              clearToken()
              setPhase('login')
            }}
            className="cursor-pointer rounded border border-border px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground"
          >
            登出
          </button>
        </div>
      </header>

      {error && (
        <div className="mx-auto mt-4 max-w-6xl px-6">
          <p role="alert" className="rounded border border-destructive bg-card p-3 text-sm text-destructive">
            {error}
          </p>
        </div>
      )}

      <ErrorBoundary>
      <Routes>
        <Route
          path="/"
          element={
            <Home
              athleteList={athleteList}
              onAthletesChanged={refreshAthletes}
              onError={setError}
            />
          }
        />
        <Route path="/activities/:id" element={<ActivityDetail />} />
        <Route path="/trends" element={<Trends />} />
        <Route path="/map" element={<MapView />} />
        <Route path="/stats" element={<StatsView />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      </ErrorBoundary>
    </div>
  )
}
