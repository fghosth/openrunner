import { useState, type FormEvent } from 'react'

type Props = {
  submit: (username: string, password: string) => Promise<void>
  title: string
  submitLabel: string
}

/** 首次设置与管理员登录共用的账密表单（票 02）。 */
export default function CredentialForm({ submit, title, submitLabel }: Props) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await submit(username, password)
    } catch (err) {
      setError(err instanceof Error ? err.message : '请求失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm rounded-lg border border-border bg-card p-6"
      >
        <h1 className="mb-6 text-center text-xl font-semibold">{title}</h1>

        <label className="mb-1 block text-sm text-muted-foreground" htmlFor="username">
          用户名
        </label>
        <input
          id="username"
          autoComplete="username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          required
          className="mb-4 w-full rounded border border-border bg-muted px-3 py-2 text-foreground focus-visible:outline-2 focus-visible:outline-ring"
        />

        <label className="mb-1 block text-sm text-muted-foreground" htmlFor="password">
          密码
        </label>
        <input
          id="password"
          type="password"
          autoComplete={submitLabel === '登录' ? 'current-password' : 'new-password'}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          minLength={6}
          className="mb-4 w-full rounded border border-border bg-muted px-3 py-2 text-foreground focus-visible:outline-2 focus-visible:outline-ring"
        />

        {error && (
          <p role="alert" className="mb-4 text-sm text-destructive">
            {error}
          </p>
        )}

        <button
          type="submit"
          disabled={busy}
          className="w-full cursor-pointer rounded bg-primary py-2 font-medium text-primary-foreground transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          {busy ? '处理中…' : submitLabel}
        </button>
      </form>
    </div>
  )
}
