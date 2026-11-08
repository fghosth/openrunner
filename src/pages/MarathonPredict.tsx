import { useEffect, useState } from 'react'
import { coach, type CoachPredict } from '../api'
import { readActiveAthlete } from '../athlete'

/** 全马多模型预测（coach-engine 票 05）：Riegel / Vickers 1·2 / Tanda，
 *  recommended 取最慢可信值（skill 纪律「Plan from the slowest credible estimate」）。 */

const fmt = (s?: number) => {
  if (!s || s <= 0) return '—'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.round(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}

const MODELS = [
  { key: 'riegel', name: 'Riegel 1.06', input: '仅需比赛成绩', src: 'S27' },
  { key: 'vickers1', name: 'Vickers 模型 1', input: '非全马成绩 + 周跑量', src: 'S25' },
  { key: 'vickers2', name: 'Vickers 模型 2', input: '两个不同距离成绩 + 周跑量', src: 'S25' },
  { key: 'tanda', name: 'Tanda', input: '周跑量 + 训练平均配速', src: 'S26' },
] as const

const TIP_KEY = 'openrunner.marathon-riegel-tip'

export default function MarathonPredict() {
  const [data, setData] = useState<CoachPredict | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [tipClosed, setTipClosed] = useState(() => localStorage.getItem(TIP_KEY) === '1')

  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    coach
      .predict(id)
      .then((d) => !cancelled && (setData(d), setErr(null)))
      .catch((e: Error) => !cancelled && setErr(e.message))
    return () => {
      cancelled = true
    }
  }, [])

  if (err) return null
  if (!data) return <div className="mt-4 text-xs text-muted-foreground">全马预测加载中…</div>
  if (data.error)
    return (
      <section aria-label="全马预测" className="mb-6 rounded-lg border border-border bg-card p-4 text-xs text-muted-foreground">
        全马预测：{data.error}
      </section>
    )

  const p = data.prediction
  if (!p) return null
  const baseKm = (data.base?.distanceM ?? 0) / 1000

  return (
    <section aria-label="全马预测" className="mb-6 rounded-lg border border-border bg-card p-4">
      <div className="mb-2 flex flex-wrap items-baseline gap-2">
        <h3 className="text-xs font-medium text-foreground">全马预测（多模型对照）</h3>
        <span className="text-[11px] text-muted-foreground">
          基准 {baseKm.toFixed(baseKm % 1 ? 2 : 0)}km · {fmt(data.base?.timeSecs)}
          （{data.base?.source === 'race' ? '比赛成绩' : '手工输入'}）· 周跑量{' '}
          {data.weekly_km?.toFixed(1) ?? '—'}km/{data.weeks_observed ?? 0} 周样本
        </span>
      </div>

      {!tipClosed && (
        <div className="mb-2 flex items-start gap-2 rounded border border-border bg-card p-2 text-[11px] text-muted-foreground">
          <span className="flex-1">
            Riegel 幂律对全马系统性乐观（Vickers 2016：常快 10 分钟以上）。有训练量数据时，
            推荐值取各模型<b className="text-foreground">最慢</b>的那个——按最保守口径配速，比预测惊喜更可靠。
          </span>
          <button
            type="button"
            className="shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => {
              localStorage.setItem(TIP_KEY, '1')
              setTipClosed(true)
            }}
            aria-label="关闭说明"
          >
            ✕
          </button>
        </div>
      )}

      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
            <th className="py-1 pr-3">模型</th>
            <th className="py-1 pr-3">预测全马</th>
            <th className="py-1 pr-3">所需输入</th>
            <th className="py-1">出处</th>
          </tr>
        </thead>
        <tbody>
          {MODELS.map((m) => {
            const v = p[m.key]
            if (!v) return null
            const isRec = Math.abs(v - p.recommended) < 0.5
            return (
              <tr key={m.key} className="border-b border-border/50">
                <td className="py-1.5 pr-3 font-medium">
                  {m.name}
                  {isRec && (
                    <span className="ml-2 rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] text-emerald-500">
                      推荐
                    </span>
                  )}
                </td>
                <td className={`py-1.5 pr-3 font-mono ${isRec ? 'text-emerald-500' : ''}`}>{fmt(v)}</td>
                <td className="py-1.5 pr-3 text-[11px] text-muted-foreground">{m.input}</td>
                <td className="py-1.5 text-[11px] text-muted-foreground">{m.src}</td>
              </tr>
            )
          })}
        </tbody>
      </table>

      {p.lowConfidence && (
        <p className="mt-2 text-[11px] text-accent">⚠ 周跑量样本不足 4 周，Vickers/Tanda 代表性有限。</p>
      )}
      {p.notes?.map((n) => (
        <p key={n} className="mt-1 text-[11px] text-muted-foreground">
          · {n}
        </p>
      ))}
      <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">
        系数取论文 Additional file（Vickers &amp; Vertosick 2013/2016、Tanda 2011），与 running-coach
        金标准逐模型对照（make coach-check）。Tanda 样本验证范围约 2:47–3:36，超出请谨慎。
      </p>
    </section>
  )
}
