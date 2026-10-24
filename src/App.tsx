import { useEffect, useState } from 'react'

type Health = {
  status: string
  version: string
  database: string
}

/**
 * walking skeleton 占位页（票 01）：
 * 验证「单二进制 + 内嵌 UI + 后端连通」的完整链路。
 * 后续票将替换为真正的导航与页面。
 */
export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/health')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json()
      })
      .then(setHealth)
      .catch((e: Error) => setError(e.message))
  }, [])

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border bg-card">
        <div className="mx-auto flex max-w-6xl items-center gap-3 px-6 py-4">
          <span className="font-mono text-lg font-semibold tracking-tight">
            Open<span className="text-secondary">runner</span>
          </span>
          <span className="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">
            walking skeleton
          </span>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-10">
        <h1 className="text-2xl font-semibold">训练分析仪表盘 · 骨架已就绪</h1>
        <p className="mt-2 text-muted-foreground">
          后端与前端链路打通中，功能页随票 02+ 逐步点亮。
        </p>

        <section
          aria-label="后端健康状态"
          className="mt-8 max-w-md rounded-lg border border-border bg-card p-5"
        >
          <h2 className="text-sm font-medium text-muted-foreground">后端健康检查</h2>
          {error && (
            <p className="mt-3 font-mono text-sm text-destructive">后端不可达：{error}</p>
          )}
          {!error && !health && (
            <p className="mt-3 font-mono text-sm text-muted-foreground">检查中…</p>
          )}
          {health && (
            <dl className="mt-3 grid grid-cols-3 gap-2 font-mono text-sm">
              <div>
                <dt className="text-xs text-muted-foreground">status</dt>
                <dd className={health.status === 'ok' ? 'text-secondary' : 'text-destructive'}>
                  {health.status}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">version</dt>
                <dd>{health.version}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">database</dt>
                <dd className={health.database === 'ok' ? 'text-secondary' : 'text-destructive'}>
                  {health.database}
                </dd>
              </div>
            </dl>
          )}
        </section>
      </main>
    </div>
  )
}
