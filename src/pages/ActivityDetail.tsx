import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { activities, type Activity } from '../api'
import { readActiveAthlete } from '../App'

const sportNames: Record<string, string> = { run: '跑步', ride: '骑行', swim: '游泳', other: '其他' }

/** 活动详情页：summary 全量展示（票 03）；曲线随票 08/09 挂载。 */
export default function ActivityDetail() {
  const { id } = useParams()
  const [activity, setActivity] = useState<Activity | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const athleteId = readActiveAthlete()
    if (!athleteId || !id) {
      setError('缺少档案或活动 id')
      return
    }
    activities
      .detail(athleteId, Number(id))
      .then(setActivity)
      .catch((err: Error) => setError(err.message))
  }, [id])

  if (error) {
    return (
      <main className="mx-auto max-w-4xl px-6 py-10">
        <p role="alert" className="rounded border border-destructive bg-card p-4 text-sm text-destructive">
          {error}
        </p>
        <Link to="/" className="mt-4 inline-block text-sm text-secondary hover:underline">
          ← 返回列表
        </Link>
      </main>
    )
  }
  if (!activity) {
    return <div className="grid min-h-screen place-items-center bg-background text-muted-foreground">加载中…</div>
  }

  const rows: [string, string][] = [
    ['类型', sportNames[activity.sport] ?? activity.sport],
    ['开始时间', new Date(activity.startTime * 1000).toLocaleString('zh-CN', { hour12: false })],
    ['时长', `${Math.floor(activity.durationSecs / 60)} 分 ${activity.durationSecs % 60} 秒`],
    ['距离', `${(activity.distanceM / 1000).toFixed(2)} km`],
    ['爬升', `${activity.elevationGainM.toFixed(0)} m`],
    ['平均配速', activity.distanceM > 0 ? `${(activity.durationSecs / (activity.distanceM / 1000)).toFixed(0)} s/km` : '—'],
    ['来源', activity.source],
    ['Stream', activity.hasStream ? '有' : '无'],
  ]

  return (
    <main className="mx-auto max-w-4xl px-6 py-10">
      <Link to="/" className="text-sm text-secondary hover:underline">
        ← 返回列表
      </Link>
      <h1 className="my-4 text-2xl font-semibold">
        活动 #{activity.id} · {sportNames[activity.sport] ?? activity.sport}
      </h1>

      <dl className="grid grid-cols-1 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-2">
        {rows.map(([k, v]) => (
          <div key={k} className="bg-card px-4 py-3">
            <dt className="text-xs text-muted-foreground">{k}</dt>
            <dd className="mt-1 font-mono text-sm">{v}</dd>
          </div>
        ))}
      </dl>

      <p className="mt-6 text-xs text-muted-foreground">
        逐秒曲线（D&apos;bal / W&apos;bal / TiZ）随票 08、09 挂载；训练负荷指标随票 05、06 挂载。
      </p>
    </main>
  )
}
