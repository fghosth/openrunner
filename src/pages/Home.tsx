import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { activities, type Activity, type Athlete } from '../api'
import DashboardPanel from './Dashboard'
import GoalCards from './GoalCards'
import { readActiveAthlete } from '../App'
import { fmtDateTime, fmtDuration, sportNames } from '../format'

type Props = {
  athleteList: Athlete[]
  onError: (msg: string | null) => void
}

/** 首页：概览 + 活动列表。
 *
 * 档案、导入、平台连接、区间参数等配置项已移到「设置」页（右上角齿轮）。
 * 首页只负责看数据 —— 一进来看得到「跑了多少、哪天跑的、每条多长」。
 */
export default function Home({ athleteList, onError }: Props) {
  const [list, setList] = useState<Activity[]>([])
  // 活动列表筛选：年份 + 距离档
  const [yearFilter, setYearFilter] = useState<number | 'all'>('all')
  const [distFilter, setDistFilter] = useState<number>(0)

  const active = readActiveAthlete()
  const activeAthlete = athleteList.find((a) => a.id === active) ?? null

  const refreshList = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) {
      setList([])
      return
    }
    try {
      setList(await activities.list(id))
    } catch (err) {
      onError(err instanceof Error ? err.message : '加载活动失败')
    }
  }, [onError])

  // athleteList 变化（切换/新建档案后）也重新拉列表
  useEffect(() => {
    void refreshList()
  }, [refreshList, athleteList])

  // 从已有活动里提取年份（倒序），供筛选标签用
  const years = useMemo(() => {
    const set = new Set<number>()
    for (const a of list) set.add(new Date(a.startTime * 1000).getFullYear())
    return [...set].sort((x, y) => y - x)
  }, [list])

  const filtered = useMemo(
    () =>
      list.filter(
        (a) =>
          (yearFilter === 'all' || new Date(a.startTime * 1000).getFullYear() === yearFilter) &&
          a.distanceM >= distFilter,
      ),
    [list, yearFilter, distFilter],
  )

  // 距离档（米）：0 = 全部
  const DIST_BUCKETS: { label: string; min: number }[] = [
    { label: '全部', min: 0 },
    { label: '10 km+', min: 10000 },
    { label: '20 km+', min: 20000 },
    { label: '40 km+', min: 40000 },
  ]

  return (
    <main className="mx-auto max-w-6xl px-6 py-8">
      {!activeAthlete && (
        <p role="alert" className="mb-4 rounded border border-accent bg-card p-3 text-sm text-accent">
          ⚠️ 还没有 Athlete 档案 —— 先去{' '}
          <Link to="/settings" className="font-medium underline">
            设置
          </Link>{' '}
          创建一个（例如「我」），然后导入或同步活动。
        </p>
      )}

      {active && list.length > 0 && (
        <div className="mb-6">
          <GoalCards activities={list} athlete={activeAthlete} />
        </div>
      )}

      {active && <DashboardPanel athlete={activeAthlete} />}

      <p className="mb-3 text-xs text-muted-foreground">
        需要导入文件、配置心率/配速区间、连接 Garmin？都在{' '}
        <Link to="/settings" className="text-secondary hover:underline">
          设置 ⚙
        </Link>
      </p>

      <section aria-label="活动列表" className="rounded-lg border border-border bg-card">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-border px-4 py-3">
          <span className="text-xs text-muted-foreground">筛选</span>

          <div className="flex flex-wrap items-center gap-1">
            {(['all', ...years] as (number | 'all')[]).map((y) => (
              <button
                key={String(y)}
                onClick={() => setYearFilter(y)}
                className={`cursor-pointer rounded-full px-2.5 py-0.5 text-xs ${
                  yearFilter === y
                    ? 'bg-secondary text-secondary-foreground'
                    : 'bg-muted text-muted-foreground hover:text-foreground'
                }`}
              >
                {y === 'all' ? '全部年份' : `${y} 年`}
              </button>
            ))}
          </div>

          <span className="text-border">|</span>

          <div className="flex flex-wrap items-center gap-1">
            {DIST_BUCKETS.map((b) => (
              <button
                key={b.min}
                onClick={() => setDistFilter(b.min)}
                className={`cursor-pointer rounded-full px-2.5 py-0.5 text-xs ${
                  distFilter === b.min
                    ? 'bg-secondary text-secondary-foreground'
                    : 'bg-muted text-muted-foreground hover:text-foreground'
                }`}
              >
                {b.label}
              </button>
            ))}
          </div>

          <span className="ml-auto text-xs text-muted-foreground">
            {filtered.length} / {list.length} 条
          </span>
          {(yearFilter !== 'all' || distFilter !== 0) && (
            <button
              onClick={() => {
                setYearFilter('all')
                setDistFilter(0)
              }}
              className="cursor-pointer text-xs text-secondary hover:underline"
            >
              重置
            </button>
          )}
        </div>

        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="px-4 py-3">日期</th>
              <th className="px-4 py-3">类型</th>
              <th className="px-4 py-3">距离</th>
              <th className="px-4 py-3">时长</th>
              <th className="px-4 py-3">爬升</th>
              <th className="px-4 py-3">来源</th>
              <th className="px-4 py-3">地图</th>
            </tr>
          </thead>
          <tbody>
            {list.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-muted-foreground">
                  还没有活动，先导入一个 FIT 文件
                </td>
              </tr>
            )}
            {list.length > 0 && filtered.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-muted-foreground">
                  没有符合筛选条件的活动
                </td>
              </tr>
            )}
            {filtered.map((a) => (
              <tr key={a.id} className="border-b border-border/50 hover:bg-muted/50">
                <td className="px-4 py-2">
                  <Link className="text-secondary hover:underline" to={`/activities/${a.id}`}>
                    {fmtDateTime(a.startTime)}
                  </Link>
                </td>
                <td className="px-4 py-2">
                  {sportNames[a.sport] ?? a.sport}
                  {a.isPlanned && (
                    <span className="ml-1 rounded bg-muted px-1 text-xs text-accent">计划</span>
                  )}
                </td>
                <td className="px-4 py-2 font-mono">{(a.distanceM / 1000).toFixed(2)} km</td>
                <td className="px-4 py-2 font-mono">{fmtDuration(a.durationSecs)}</td>
                <td className="px-4 py-2 font-mono">{a.elevationGainM.toFixed(0)} m</td>
                <td className="px-4 py-2 text-xs text-muted-foreground">{a.source}</td>
                <td className="px-4 py-2">
                  {a.hasGps ? (
                    <Link
                      to={`/map?activity=${a.id}`}
                      title="在地图上查看这条轨迹"
                      className="text-secondary hover:underline"
                    >
                      地图 ↗
                    </Link>
                  ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </main>
  )
}
