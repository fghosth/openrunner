import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import * as maplibregl from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { tracks as fetchTracks, type Track } from '../api'
import { readActiveAthlete } from '../App'
import { baseMaps, wgs84ToGcj02 } from '../coord'
import { sportNames } from '../format'
import { formatDistance, formatPace, paceUnit, useUnit } from '../units'
import { useLocale } from '../i18n'

/** 轨迹墙（对齐 running_page 的 TracksPage）。
 *
 * 核心不是地图，而是「把每条活动画成一张归一化小图」铺成一面墙 —— 一眼能看出
 * 跑过哪些路线、同一条路线重复了多少次。左栏 sticky 放统计 + 选中详情 + 小地图。
 *
 * 与 running_page 的差异：
 *  - 它的 polyline 是平台预计算的 summary_polyline；我们存的是完整坐标流，
 *    所以缩略图在前端做归一化（把 lat/lon 等比缩放到 80×80 的 SVG 里）。
 *  - 聚类不用 worker：323 条轨迹的起终点比对是毫秒级，同步算即可。
 */

const THUMB = 80

/** 把一条轨迹的坐标归一化成 80×80 的 SVG polyline 点串。 */
function thumbPoints(coords: [number, number][]): string {
  if (coords.length < 2) return ''
  let minLat = Infinity
  let maxLat = -Infinity
  let minLng = Infinity
  let maxLng = -Infinity
  for (const [lat, lng] of coords) {
    if (lat < minLat) minLat = lat
    if (lat > maxLat) maxLat = lat
    if (lng < minLng) minLng = lng
    if (lng > maxLng) maxLng = lng
  }
  const latRange = maxLat - minLat || 0.001
  const lngRange = maxLng - minLng || 0.001
  const pad = 6
  const scale = Math.min((THUMB - pad * 2) / lngRange, (THUMB - pad * 2) / latRange)
  const offX = (THUMB - lngRange * scale) / 2
  const offY = (THUMB - latRange * scale) / 2
  let out = ''
  for (const [lat, lng] of coords) {
    const x = (lng - minLng) * scale + offX
    const y = THUMB - ((lat - minLat) * scale + offY)
    out += `${x.toFixed(1)},${y.toFixed(1)} `
  }
  return out.trim()
}

/** 轨迹颜色：跑步按距离分色（≥20km 红），其他类型青色。 */
function trackColor(t: Track): string {
  if (t.sport === 'run') return t.distanceM >= 20000 ? '#ef4444' : '#f97316'
  return '#22d3ee'
}

type Cluster = { rep: Track; count: number }

/**
 * 同路线聚类：起点与终点都相近、且距离落在同一 2km 桶内的轨迹合并。
 * （与 running_page 的 clusterTracks.worker 同一判据，只是不放到 worker 里。）
 */
function clusterByRoute(tracks: Track[]): Cluster[] {
  const info = tracks.map((t) => {
    if (t.points.length < 2) return null
    const s = t.points[0]
    const e = t.points[t.points.length - 1]
    return { start: s, end: e, bucket: Math.round(t.distanceM / 2000) }
  })
  const used = new Array(tracks.length).fill(false)
  const out: Cluster[] = []
  for (let i = 0; i < tracks.length; i++) {
    if (used[i]) continue
    const a = info[i]
    if (!a) {
      used[i] = true
      continue
    }
    let count = 1
    for (let j = i + 1; j < tracks.length; j++) {
      if (used[j]) continue
      const b = info[j]
      if (!b || a.bucket !== b.bucket) continue
      if (
        Math.abs(a.start[0] - b.start[0]) < 0.005 &&
        Math.abs(a.start[1] - b.start[1]) < 0.005 &&
        Math.abs(a.end[0] - b.end[0]) < 0.005 &&
        Math.abs(a.end[1] - b.end[1]) < 0.005
      ) {
        used[j] = true
        count++
      }
    }
    used[i] = true
    out.push({ rep: tracks[i], count })
  }
  return out
}

const TrackThumb = memo(function TrackThumb({
  cluster,
  selected,
  unit,
  onSelect,
}: {
  cluster: Cluster
  selected: boolean
  unit: 'metric' | 'imperial'
  onSelect: (t: Track) => void
}) {
  const { rep, count } = cluster
  const pts = useMemo(() => thumbPoints(rep.points), [rep.points])
  if (!pts) return null
  const d = new Date(rep.startTime * 1000)
  const label = `${d.toLocaleDateString('zh-CN')} · ${sportNames[rep.sport] ?? rep.sport} · ${formatDistance(rep.distanceM, unit)}`
  return (
    <button
      type="button"
      onClick={() => onSelect(rep)}
      aria-pressed={selected}
      aria-label={label}
      title={count > 1 ? `${label}（同路线 ${count} 次）` : label}
      className={`group relative cursor-pointer rounded transition-all ${
        selected ? 'ring-2 ring-accent ring-offset-1 ring-offset-card' : ''
      }`}
    >
      <svg
        width={THUMB}
        height={THUMB}
        viewBox={`0 0 ${THUMB} ${THUMB}`}
        className={`block transition-opacity ${selected ? 'opacity-100' : 'opacity-55 group-hover:opacity-100'}`}
      >
        <polyline
          points={pts}
          fill="none"
          stroke={trackColor(rep)}
          strokeWidth={selected ? 2 : 1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      {count > 1 && (
        <span className="absolute right-0.5 bottom-0.5 rounded bg-muted px-1 font-mono text-[9px] leading-3 text-muted-foreground">
          ×{count}
        </span>
      )}
    </button>
  )
})

export default function TracksWall() {
  const { unit } = useUnit()
  const { locale } = useLocale()
  const [searchParams] = useSearchParams()

  const [list, setList] = useState<Track[]>([])
  const [error, setError] = useState<string | null>(null)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [year, setYear] = useState<number | 'all'>('all')
  const [distMin, setDistMin] = useState(0)
  const [sortBy, setSortBy] = useState<'date' | 'distance'>('date')

  // 加载轨迹
  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    let cancelled = false
    fetchTracks(id, 200)
      .then((ts) => {
        if (!cancelled) setList(ts)
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const years = useMemo(() => {
    const s = new Set<number>()
    for (const t2 of list) s.add(new Date(t2.startTime * 1000).getFullYear())
    return [...s].sort((a, b) => b - a)
  }, [list])

  const DIST_BUCKETS = [
    { label: locale === 'zh' ? '全部' : 'All', min: 0 },
    { label: '10 km+', min: 10000 },
    { label: '20 km+', min: 20000 },
    { label: '40 km+', min: 40000 },
  ]

  // 年份 + 距离过滤
  const base = useMemo(
    () =>
      list.filter(
        (t2) =>
          (year === 'all' || new Date(t2.startTime * 1000).getFullYear() === year) &&
          t2.distanceM >= distMin,
      ),
    [list, year, distMin],
  )

  const { totalDist, totalTime, avgPace } = useMemo(() => {
    let dist = 0
    let time = 0
    let speed = 0
    let runs = 0
    for (const t2 of base) {
      dist += t2.distanceM
      time += t2.durationSecs
      if (t2.sport === 'run' && t2.distanceM > 0) {
        speed += t2.distanceM / t2.durationSecs // m/s
        runs++
      }
    }
    // 均配速：平均速度的倒数（secs/km）
    const avg = runs > 0 ? 1000 / (speed / runs) : 0
    return { totalDist: dist, totalTime: time, avgPace: avg }
  }, [base])

  const clusters = useMemo(() => clusterByRoute(base), [base])

  const sorted = useMemo(
    () =>
      [...clusters].sort((a, b) =>
        sortBy === 'distance'
          ? b.rep.distanceM - a.rep.distanceM
          : b.rep.startTime - a.rep.startTime,
      ),
    [clusters, sortBy],
  )

  // URL ?activity=ID 时自动选中（来自首页/详情页的「地图 ↗」）
  useEffect(() => {
    if (selectedId !== null || list.length === 0) return
    const fromUrl = Number(searchParams.get('activity'))
    if (fromUrl && list.some((t2) => t2.id === fromUrl)) setSelectedId(fromUrl)
  }, [list, selectedId, searchParams])

  const selected = useMemo(
    () => list.find((t2) => t2.id === selectedId) ?? null,
    [list, selectedId],
  )

  // ---------- 左栏小地图 ----------
  const mapBox = useRef<HTMLDivElement>(null)
  const mapObj = useRef<maplibregl.Map | null>(null)

  useEffect(() => {
    if (!mapBox.current) return
    const map = new maplibregl.Map({
      container: mapBox.current,
      style: baseMaps.amap.style as maplibregl.StyleSpecification,
      center: [121.47, 31.23],
      zoom: 11,
      attributionControl: { compact: true },
    })
    mapObj.current = map
    return () => {
      map.remove()
      mapObj.current = null
    }
  }, [])

  // 选中轨迹 → 画线并缩放到它
  useEffect(() => {
    const map = mapObj.current
    if (!map || !selected || selected.points.length < 2) return

    const coords = selected.points
      .map(([lat, lon]) => {
        const [la, lo] = wgs84ToGcj02(lat, lon)
        return [lo, la] as [number, number]
      })
      .filter(([lo, la]) => Number.isFinite(lo) && Number.isFinite(la))

    const apply = (): boolean => {
      const data = {
        type: 'FeatureCollection' as const,
        features: [
          {
            type: 'Feature' as const,
            properties: {},
            geometry: { type: 'LineString' as const, coordinates: coords },
          },
        ],
      }
      const src = map.getSource('wall-line') as maplibregl.GeoJSONSource | undefined
      if (src) {
        src.setData(data)
      } else {
        try {
          map.addSource('wall-line', { type: 'geojson', data })
          map.addLayer({
            id: 'wall-line-layer',
            type: 'line',
            source: 'wall-line',
            layout: { 'line-join': 'round', 'line-cap': 'round' },
            paint: { 'line-color': '#f97316', 'line-width': 4, 'line-opacity': 0.95 },
          })
        } catch {
          return false
        }
      }
      const bounds = new maplibregl.LngLatBounds()
      for (const c of coords) bounds.extend(c as maplibregl.LngLatLike)
      map.fitBounds(bounds, { padding: 30, maxZoom: 16, duration: 500 })
      return true
    }

    // 与 MapStats 同样的坑：style 就绪判断不可靠，直接试、失败重试
    let tries = 0
    const run = () => {
      if (apply()) return
      if (++tries < 60) setTimeout(run, 100)
    }
    run()
  }, [selected])

  const selectTrack = useCallback((t2: Track) => {
    setSelectedId((cur) => (cur === t2.id ? null : t2.id))
  }, [])

  const selSecs = selected ? selected.durationSecs : 0
  const selPace = selected && selected.distanceM > 0 ? selSecs / (selected.distanceM / 1000) : 0

  return (
    <main className="mx-auto max-w-[1400px] px-4 py-6 sm:px-6">
      <div className="mb-5 flex items-center gap-4">
        <h1 className="text-lg font-bold">{locale === 'zh' ? '轨迹墙' : 'Track Wall'}</h1>
        <span className="text-xs text-muted-foreground">
          {sorted.length} {locale === 'zh' ? '条路线' : 'routes'} · {base.length}{' '}
          {locale === 'zh' ? '次活动' : 'activities'}
        </span>
        <Link to="/stats" className="ml-auto text-sm text-secondary hover:underline">
          {locale === 'zh' ? '年度统计 →' : 'Yearly →'}
        </Link>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      <div className="grid grid-cols-1 items-start gap-5 lg:grid-cols-[320px_1fr]">
        {/* 左栏：统计 + 选中详情 + 小地图（sticky） */}
        <div className="flex flex-col gap-4 lg:sticky lg:top-20">
          <div className="rounded-xl border border-border bg-card p-4">
            <p className="mb-3 text-[10px] tracking-wider text-muted-foreground uppercase">
              {year === 'all' ? (locale === 'zh' ? '全部' : 'All') : year}
            </p>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                  {locale === 'zh' ? '活动' : 'Activities'}
                </p>
                <p className="font-mono text-2xl font-bold text-accent">{base.length}</p>
              </div>
              <div>
                <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                  {locale === 'zh' ? '距离' : 'Distance'}
                </p>
                <p className="font-mono text-2xl font-bold">
                  {formatDistance(totalDist, unit)}
                </p>
              </div>
              <div>
                <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                  {locale === 'zh' ? '时间' : 'Time'}
                </p>
                <p className="font-mono text-lg font-bold">
                  {Math.floor(totalTime / 3600)}h {Math.floor((totalTime % 3600) / 60)}m
                </p>
              </div>
              {avgPace > 0 && (
                <div>
                  <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                    {locale === 'zh' ? '均配速' : 'Avg Pace'}
                  </p>
                  <p className="font-mono text-lg font-bold">
                    {formatPace(avgPace, unit)}
                    {paceUnit(unit)}
                  </p>
                </div>
              )}
            </div>
          </div>

          {selected && (
            <div className="rounded-xl border border-border bg-card px-4 py-3">
              <div className="mb-2 flex items-center justify-between">
                <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                  {locale === 'zh' ? '已选记录' : 'Selected'}
                </p>
                <button
                  onClick={() => setSelectedId(null)}
                  aria-label={locale === 'zh' ? '清除选中' : 'Clear'}
                  className="cursor-pointer text-muted-foreground hover:text-foreground"
                >
                  ✕
                </button>
              </div>
              <p className="mb-0.5 text-xs font-semibold">
                {sportNames[selected.sport] ?? selected.sport} ·{' '}
                {formatDistance(selected.distanceM, unit)}
              </p>
              <p className="mb-2 text-[10px] text-muted-foreground">
                {new Date(selected.startTime * 1000).toLocaleString('zh-CN', {
                  hour12: false,
                })}
              </p>
              <div className="grid grid-cols-3 gap-2">
                <div>
                  <p className="text-[9px] tracking-wider text-muted-foreground uppercase">
                    {locale === 'zh' ? '距离' : 'Dist'}
                  </p>
                  <p className="font-mono text-sm font-bold">
                    {formatDistance(selected.distanceM, unit, 2)}
                  </p>
                </div>
                <div>
                  <p className="text-[9px] tracking-wider text-muted-foreground uppercase">
                    {locale === 'zh' ? '时间' : 'Time'}
                  </p>
                  <p className="font-mono text-sm font-bold">
                    {Math.floor(selSecs / 3600) > 0
                      ? `${Math.floor(selSecs / 3600)}h ${Math.floor((selSecs % 3600) / 60)}m`
                      : `${Math.floor(selSecs / 60)}m`}
                  </p>
                </div>
                {selPace > 0 && (
                  <div>
                    <p className="text-[9px] tracking-wider text-muted-foreground uppercase">
                      {locale === 'zh' ? '配速' : 'Pace'}
                    </p>
                    <p className="font-mono text-sm font-bold">
                      {formatPace(selPace, unit)}
                    </p>
                  </div>
                )}
              </div>
              <div className="mt-2 flex items-center gap-2 text-[10px] text-muted-foreground">
                {selected.avgHr > 0 && <span>♥ {Math.round(selected.avgHr)} bpm</span>}
                <Link
                  to={`/activities/${selected.id}`}
                  className="ml-auto text-secondary hover:underline"
                >
                  {locale === 'zh' ? '查看详情 →' : 'Details →'}
                </Link>
              </div>
            </div>
          )}

          <div className="overflow-hidden rounded-xl border border-border bg-card">
            <div ref={mapBox} className="h-64 w-full" aria-label="选中轨迹地图" />
          </div>
        </div>

        {/* 右栏：筛选 + 轨迹墙 */}
        <div className="min-w-0 rounded-xl border border-border bg-card p-4">
          <div className="mb-4 flex flex-wrap items-center gap-1.5 border-b border-border pb-3">
            {(['all', ...years] as (number | 'all')[]).map((y) => (
              <button
                key={String(y)}
                onClick={() => setYear(y)}
                aria-pressed={year === y}
                className={`cursor-pointer rounded-full px-3 py-1 text-xs font-medium ${
                  year === y
                    ? 'bg-accent text-accent-foreground'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {y === 'all' ? (locale === 'zh' ? '全部' : 'All') : y}
              </button>
            ))}

            <span className="mx-1 text-border">|</span>

            {DIST_BUCKETS.map((b) => (
              <button
                key={b.min}
                onClick={() => setDistMin(b.min)}
                aria-pressed={distMin === b.min}
                className={`cursor-pointer rounded-full px-3 py-1 text-xs font-medium ${
                  distMin === b.min
                    ? 'bg-accent text-accent-foreground'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {b.label}
              </button>
            ))}

            <select
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value as 'date' | 'distance')}
              className="ml-auto rounded border border-border bg-muted px-2 py-1 text-xs"
              aria-label="排序"
            >
              <option value="date">{locale === 'zh' ? '按日期' : 'By date'}</option>
              <option value="distance">{locale === 'zh' ? '按距离' : 'By distance'}</option>
            </select>
          </div>

          {sorted.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {locale === 'zh' ? '没有符合条件的轨迹' : 'No tracks match the filters'}
            </p>
          ) : (
            <div className="flex flex-wrap gap-1">
              {sorted.map((c) => (
                <TrackThumb
                  key={c.rep.id}
                  cluster={c}
                  unit={unit}
                  selected={c.rep.id === selectedId}
                  onSelect={selectTrack}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </main>
  )
}
