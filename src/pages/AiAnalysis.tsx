import { useCallback, useEffect, useRef, useState } from 'react'
import { activities as fetchActivities, aiPrompt, type Activity } from '../api'
import { readActiveAthlete } from '../athlete'
import { formatDistance, useUnit } from '../units'

type RangeKey = 'activity' | 'week' | 'month' | 'quarter' | 'year'
type GoalKey = '5k' | '10k' | 'half' | 'marathon'

const RANGES: { key: RangeKey; label: string }[] = [
  { key: 'activity', label: '单次训练' },
  { key: 'week', label: '最近一周' },
  { key: 'month', label: '最近一个月' },
  { key: 'quarter', label: '最近一季度' },
  { key: 'year', label: '最近一年' },
]

const GOALS: { key: GoalKey; label: string }[] = [
  { key: '5k', label: '5 公里' },
  { key: '10k', label: '10 公里' },
  { key: 'half', label: '半程马拉松' },
  { key: 'marathon', label: '全程马拉松' },
]

/** AI 分析页：把训练数据整理成专业提示词，用户复制到任意对话式 AI。
 *
 * 不调任何外部 AI（自托管项目不绑第三方 key），只负责把「档案 + 负荷 +
 * 强度分布 + 活动明细 + 个人最佳 + 目标」整理成结构化 Markdown。
 */
export default function AiAnalysis() {
  const { unit } = useUnit()
  const [range, setRange] = useState<RangeKey>('month')
  const [goal, setGoal] = useState<GoalKey>('10k')
  const [list, setList] = useState<Activity[]>([])
  const [activityId, setActivityId] = useState<number | null>(null)
  const [markdown, setMarkdown] = useState('')
  const [meta, setMeta] = useState<{ from: string; to: string; n: number } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const taRef = useRef<HTMLTextAreaElement>(null)

  // 可供选择的单次活动（只取有数据的，按时间倒序）
  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    fetchActivities
      .list(id)
      .then((l) => {
        const sorted = [...l].sort((a, b) => b.startTime - a.startTime)
        setList(sorted)
        if (sorted.length > 0) setActivityId((cur) => cur ?? sorted[0].id)
      })
      .catch(() => undefined)
  }, [])

  const generate = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) {
      setError('请先创建并选择 Athlete 档案')
      return
    }
    setBusy(true)
    setError(null)
    setCopied(false)
    try {
      const r = await aiPrompt(id, {
        range,
        goal,
        activityId: range === 'activity' ? activityId ?? undefined : undefined,
      })
      setMarkdown(r.markdown)
      setMeta({ from: r.from, to: r.to, n: r.activities })
    } catch (err) {
      setError(err instanceof Error ? err.message : '生成失败')
      setMarkdown('')
      setMeta(null)
    } finally {
      setBusy(false)
    }
  }, [range, goal, activityId])

  async function copy() {
    setError(null)
    // 优先用异步剪贴板 API；被拒（非 https / 无用户手势 / 权限受限）时
    // 退回 execCommand —— 对已聚焦的 textarea 通常仍然有效。
    try {
      // 某些环境（无用户手势 / 权限策略 / headless）下 writeText 会**挂起**
      // 既不 resolve 也不 reject —— 加 800ms 超时，超时就走下面的 execCommand 兜底。
      await Promise.race([
        navigator.clipboard.writeText(markdown),
        new Promise((_, reject) => setTimeout(() => reject(new Error('clipboard timeout')), 800)),
      ])
      setCopied(true)
      setTimeout(() => setCopied(false), 2500)
      return
    } catch {
      /* fall through to execCommand */
    }
    const ta = taRef.current
    if (ta) {
      ta.focus()
      ta.select()
      const ok = document.execCommand('copy')
      if (ok) {
        setCopied(true)
        setTimeout(() => setCopied(false), 2500)
        return
      }
    }
    setError('浏览器拒绝了剪贴板访问，请手动全选文本框内容复制（Ctrl/Cmd+A → Ctrl/Cmd+C）')
  }

  return (
    <main className="mx-auto max-w-5xl px-6 py-8">
      <div className="mb-5">
        <h1 className="text-2xl font-semibold">AI 分析</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          把你的真实训练数据整理成一份专业提示词，复制到任意 AI 对话（ChatGPT / Claude / Kimi…）
          即可获得训练分析与改进建议。数据不会离开本机 —— 只有你主动复制的那段文本。
        </p>
      </div>

      <section aria-label="分析配置" className="mb-4 rounded-lg border border-border bg-card p-4">
        <div className="space-y-3">
          <div>
            <div className="mb-1.5 text-xs font-medium text-foreground">分析范围</div>
            <div className="flex flex-wrap gap-1">
              {RANGES.map((r) => (
                <button
                  key={r.key}
                  onClick={() => setRange(r.key)}
                  aria-pressed={range === r.key}
                  className={`cursor-pointer rounded-full px-3 py-1 text-xs ${
                    range === r.key
                      ? 'bg-secondary text-secondary-foreground'
                      : 'bg-muted text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {r.label}
                </button>
              ))}
            </div>
            {range === 'activity' && (
              <select
                value={activityId ?? ''}
                onChange={(e) => setActivityId(Number(e.target.value))}
                className="mt-2 w-full max-w-xl rounded border border-border bg-muted px-2 py-1.5 text-sm"
                aria-label="选择要分析的活动"
              >
                {list.slice(0, 200).map((a) => (
                  <option key={a.id} value={a.id}>
                    {new Date(a.startTime * 1000).toLocaleString('zh-CN', { hour12: false })} ·{' '}
                    {formatDistance(a.distanceM, unit)} · {a.sport}
                  </option>
                ))}
              </select>
            )}
          </div>

          <div>
            <div className="mb-1.5 text-xs font-medium text-foreground">训练目标</div>
            <div className="flex flex-wrap gap-1">
              {GOALS.map((g) => (
                <button
                  key={g.key}
                  onClick={() => setGoal(g.key)}
                  aria-pressed={goal === g.key}
                  className={`cursor-pointer rounded-full px-3 py-1 text-xs ${
                    goal === g.key
                      ? 'bg-accent text-accent-foreground'
                      : 'bg-muted text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {g.label}
                </button>
              ))}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={() => void generate()}
              disabled={busy}
              className="cursor-pointer rounded bg-primary px-4 py-1.5 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-50"
            >
              {busy ? '生成中…' : '生成提示词'}
            </button>
            {meta && (
              <span className="text-xs text-muted-foreground">
                {meta.from} ~ {meta.to} · {meta.n} 次活动 · {markdown.length} 字符
              </span>
            )}
          </div>
        </div>
      </section>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {markdown && (
        <section aria-label="提示词" className="rounded-lg border border-border bg-card p-4">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-medium text-foreground">生成的提示词</h2>
            <span className="text-[11px] text-muted-foreground">
              可以直接改（比如补充近期伤病、比赛日期）
            </span>
            <button
              onClick={() => void copy()}
              className="ml-auto cursor-pointer rounded bg-accent px-3 py-1 text-xs font-medium text-accent-foreground hover:opacity-90"
            >
              {copied ? '✓ 已复制' : '复制到剪贴板'}
            </button>
          </div>
          <textarea
            ref={taRef}
            value={markdown}
            onChange={(e) => setMarkdown(e.target.value)}
            spellCheck={false}
            className="h-[28rem] w-full resize-y rounded border border-border bg-muted/40 p-3 font-mono text-xs leading-relaxed text-foreground"
            aria-label="提示词内容"
          />
          <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
            复制后粘贴到任意 AI 对话框即可。提示词里已包含：运动员档案（心率/阈值/CS）、
            训练负荷（TRIMP/rTSS + PMC 期末值）、强度分布（Z1–Z5 占比）、逐次活动明细、
            个人最佳、系统确定性计算（下周计划校验 / 训练配速表 / 全马预测——这些数值
            由系统按已发表公式算好，AI 只解读不重算），以及针对你选定目标的专项提问。
          </p>
        </section>
      )}
    </main>
  )
}
