import { Component, useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import ThemeToggle from './ThemeToggle'
import { UnitProvider } from './units'
import UnitToggle from './UnitToggle'
import { readActiveAthlete, writeActiveAthlete } from './athlete'
import { LocaleProvider, useLocale } from './i18n'
import { GlossaryProvider } from './glossary-term'
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
import RaceCalendar from './pages/RaceCalendar'
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

  /** 导航项：图标是纯装饰（aria-hidden），可访问名仍是文字 —— e2e 按文字定位。 */
  const navItems: { to: string; label: string; icon: string }[] = [
    { to: '/', label: t('navActivities'), icon: 'M3 3h7v9H3zM14 3h7v5h-7zM14 12h7v9h-7zM3 16h7v5H3z' },
    { to: '/trends', label: t('navTrends'), icon: 'M3 15l4-5 4 3 4-7 6 6' },
    { to: '/map', label: t('navTracksWall'), icon: 'M4 19c4 0 5-14 9-14s4 14 7 6' },
    { to: '/races', label: t('navRaces'), icon: 'M6 3h12l-2 6H8L6 3zM8 9v3a4 4 0 008 0V9M12 16v5M8 21h8' },
    { to: '/stats', label: t('navSummary'), icon: 'M4 20V9M10 20V4M16 20v-7M22 20H2' },
    { to: '/ai-analysis', label: t('navAiAnalysis'), icon: 'M12 3l1.8 4.2L18 9l-4.2 1.8L12 15l-1.8-4.2L6 9l4.2-1.8L12 3z' },
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
    <div className="flex min-h-screen flex-col bg-background text-foreground lg:flex-row">
      {/*
       * 左侧导航（「训练台」重设计 第 4 步）。
       * 窄屏（<lg）退化成顶部横向条：同一份 markup 靠 max-lg: 变体换轴向，
       * 不复制第二套导航 —— 复制出来的导航迟早和主导航走岔。
       */}
      <aside className="sticky top-0 z-10 flex h-screen w-[236px] shrink-0 flex-col gap-4 border-r border-border bg-card px-3 py-4 max-lg:h-auto max-lg:w-full max-lg:min-w-0 max-lg:flex-row max-lg:items-center max-lg:gap-3 max-lg:flex-wrap max-lg:border-b max-lg:border-r-0">
        <div className="flex shrink-0 items-center gap-2 px-2">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M2 17c3.6 0 4.6-9.5 8.2-9.5S15 17 22 8.5" stroke="var(--color-accent)" strokeWidth="2.4" strokeLinecap="round" />
            <circle cx="18.5" cy="16.5" r="3" stroke="var(--color-brand)" strokeWidth="2.2" />
          </svg>
          <span className="whitespace-nowrap font-mono text-[15px] font-semibold tracking-tight">
            Open<span className="text-secondary">runner</span>
          </span>
        </div>

        {/* 窄屏让入口换行而不是横向滚动：横滚会把「赛事地图 / 汇总 / AI 分析」
            藏到屏幕外，用户不知道还有这些页。min-w-0 是必须的，
            否则 nav 在横向 flex 里被内容顶宽、换行不生效 */}
        <nav aria-label="主导航" className="flex flex-col gap-0.5 text-sm lg:shrink-0 max-lg:min-w-0 max-lg:flex-1 max-lg:flex-row max-lg:flex-wrap max-lg:items-center">
          {navItems.map((item) => {
            const active = isActive(item.to)
            return (
              <Link
                key={item.to}
                to={item.to}
                aria-current={active ? 'page' : undefined}
                className={`relative flex items-center gap-2.5 whitespace-nowrap rounded-md px-2.5 py-2 transition-colors ${
                  active
                    ? 'bg-muted font-semibold text-foreground'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                }`}
              >
                {/* 选中指示条用信号色，只在竖向导航下出现 */}
                {active && (
                  <span aria-hidden="true" className="absolute inset-y-2 -left-3 hidden w-[3px] rounded-r bg-accent lg:block" />
                )}
                <svg
                  aria-hidden="true"
                  className={`h-4 w-4 shrink-0 ${active ? 'text-secondary' : 'opacity-80'}`}
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={1.8}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d={item.icon} />
                </svg>
                {item.label}
              </Link>
            )
          })}
        </nav>

        {/* 工具组降权：图标按钮 + aria-label，登出不再与主导航同权重 */}
        {/* 窄屏 basis-full 把工具组顶到第二行右端，不与六个入口挤同一行 */}
        <div className="ml-auto flex shrink-0 items-center gap-2 max-lg:ml-auto max-lg:basis-full max-lg:justify-end lg:mt-auto lg:flex-col lg:items-stretch">
          <div className="flex items-center gap-2">
            <LocaleToggle />
            <UnitToggle />
            <ThemeToggle />
          </div>
          <div className="flex items-center gap-2 lg:w-full">
            <Link
              to="/settings"
              aria-label="设置"
              title="设置（档案 / 导入 / 平台 / 区间）"
              aria-current={pathname.startsWith('/settings') ? 'page' : undefined}
              className={`flex-1 rounded border px-2 py-1.5 text-center transition-colors ${
                pathname.startsWith('/settings')
                  ? 'border-secondary bg-secondary/15 text-secondary'
                  : 'border-border text-muted-foreground hover:text-foreground'
              }`}
            >
              <svg
                aria-hidden="true"
                className="mx-auto h-4 w-4"
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
              className="flex-1 cursor-pointer rounded border border-border px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              {t('logout')}
            </button>
          </div>
        </div>
      </aside>

      <div className="min-w-0 flex-1">

      {error && (
        <div className="mx-auto mt-4 max-w-[var(--page-max)] px-6">
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
        <Route path="/races" element={<RaceCalendar />} />
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
    </div>
  )
}

/** 顶层：把语言/单位 Provider 包在 AppShell 外层，
 *  这样 header 里的切换按钮和所有页面都能读到当前语言与单位制。
 *  GlossaryProvider 在最外层：全站只挂一个术语浮层（portal 到 body），
 *  这样 tooltip 不会被卡片的 overflow/圆角裁掉。 */
export default function App() {
  return (
    <LocaleProvider>
      <UnitProvider>
        <GlossaryProvider>
          <AppShell />
        </GlossaryProvider>
      </UnitProvider>
    </LocaleProvider>
  )
}
