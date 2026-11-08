import { Component, useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import ThemeToggle from './ThemeToggle'
import { UnitProvider } from './units'
import UnitToggle from './UnitToggle'
import { readActiveAthlete, writeActiveAthlete } from './athlete'
import { LocaleProvider, useLocale } from './i18n'
import LocaleToggle from './LocaleToggle'
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
import SummaryPage from './pages/SummaryPage'
import AiAnalysis from './pages/AiAnalysis'
import Settings from './pages/Settings'
import TracksWall from './pages/TracksWall'
import ActivityDetail from './pages/ActivityDetail'


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
function AppShell() {
  const { t } = useLocale()
  const { pathname } = useLocation()

  // 导航高亮：活动详情（/activities/:id）归到「活动」；其余按前缀匹配。
  const isActive = (to: string) =>
    to === '/' ? pathname === '/' || pathname.startsWith('/activities') : pathname.startsWith(to)

  const navItems: { to: string; label: string }[] = [
    { to: '/', label: t('navActivities') },
    { to: '/trends', label: t('navTrends') },
    { to: '/map', label: t('navTracksWall') },
    { to: '/stats', label: t('navSummary') },
    { to: '/ai-analysis', label: t('navAiAnalysis') },
  ]
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
          <nav className="flex gap-1 text-sm">
            {navItems.map((item) => {
              const active = isActive(item.to)
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  aria-current={active ? 'page' : undefined}
                  className={`rounded px-3 py-1.5 transition-colors ${
                    active
                      ? 'bg-secondary/15 font-medium text-secondary'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                  }`}
                >
                  {item.label}
                </Link>
              )
            })}
          </nav>
          {/* 右侧工具组：ml-auto 把它推到最右（此前它们紧贴导航栏，没有靠右） */}
          <div className="ml-auto flex items-center gap-2">
            <LocaleToggle />
            <UnitToggle />
            <ThemeToggle />
            <Link
              to="/settings"
              aria-label="设置"
              title="设置（档案 / 导入 / 平台 / 区间）"
              aria-current={pathname.startsWith('/settings') ? 'page' : undefined}
              className={`rounded border px-2 py-1.5 transition-colors ${
                pathname.startsWith('/settings')
                  ? 'border-secondary bg-secondary/15 text-secondary'
                  : 'border-border text-muted-foreground hover:text-foreground'
              }`}
            >
              <svg
                className="h-4 w-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth={1.8}
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <circle cx="12" cy="12" r="3" />
                <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
              </svg>
            </Link>
            <button
              onClick={() => {
                clearToken()
                setPhase('login')
              }}
              className="cursor-pointer rounded border border-border px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground"
            >
                {t('logout')}
              </button>
          </div>
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
            <Home athleteList={athleteList} onError={setError} />
          }
        />
        <Route path="/activities/:id" element={<ActivityDetail />} />
        <Route path="/trends" element={<Trends />} />
        <Route path="/map" element={<TracksWall />} />
        <Route path="/stats" element={<SummaryPage />} />
        <Route path="/ai-analysis" element={<AiAnalysis />} />
        <Route
          path="/settings"
          element={
            <Settings
              athleteList={athleteList}
              onAthletesChanged={refreshAthletes}
              onError={setError}
            />
          }
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      </ErrorBoundary>
    </div>
  )
}

/** 顶层：把语言/单位 Provider 包在 AppShell 外层，
 *  这样 header 里的切换按钮和所有页面都能读到当前语言与单位制。 */
export default function App() {
  return (
    <LocaleProvider>
      <UnitProvider>
        <AppShell />
      </UnitProvider>
    </LocaleProvider>
  )
}
