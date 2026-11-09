import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import * as maplibregl from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { tracks as fetchTracks, type Track } from '../api'
import { readActiveAthlete } from '../athlete'
import { baseMaps, wgs84ToGcj02 } from '../coord'
import type { TKey } from '../i18n'
import { formatDistance, formatPace, paceUnit, useUnit } from '../units'
import { useLocale } from '../i18n'

const SPORT_KEYS: Record<string, TKey> = { run: 'sportRun', ride: 'sportRide', swim: 'sportSwim', other: 'sportOther' }

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

/** 把一条轨迹的坐标归一化成 80×80 的 SVG polyline 点串。 */
function thumbPoints(coords: [number, number][], size: number): string {
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
  const scale = Math.min((size - pad * 2) / lngRange, (size - pad * 2) / latRange)
  const offX = (size - lngRange * scale) / 2
  const offY = (size - latRange * scale) / 2
  let out = ''
  for (const [lat, lng] of coords) {
    const x = (lng - minLng) * scale + offX
    const y = size - ((lat - minLat) * scale + offY)
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
  size = 80,
  showCount,
}: {
  cluster: Cluster
  selected: boolean
  unit: 'metric' | 'imperial'
  onSelect: (t: Track) => void
  /** 缩略图边长（全屏浮层用更小的尺寸，而不是靠 CSS 缩放） */
  size?: number
  /** 是否显示 ×N 角标（小尺寸下可选隐藏） */
  showCount?: boolean
}) {
  const { t } = useLocale()
  const { rep, count } = cluster
  const pts = useMemo(() => thumbPoints(rep.points, size), [rep.points, size])
  if (!pts) return null
  const d = new Date(rep.startTime * 1000)
  const label = `${d.toLocaleDateString('zh-CN')} · ${SPORT_KEYS[rep.sport] ? t(SPORT_KEYS[rep.sport]) : rep.sport} · ${formatDistance(rep.distanceM, unit)}`
  return (
    <button
      type="button"
      onClick={() => onSelect(rep)}
      aria-pressed={selected}
      aria-label={label}
      title={count > 1 ? t('wallThumbTitle', { label, count }) : label}
      className={`group relative cursor-pointer rounded transition-all ${
        selected ? 'ring-2 ring-accent ring-offset-1 ring-offset-card' : ''
      }`}
    >
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
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
      {count > 1 && showCount !== false && (
        <span
          title={t('wallThumbCountTitle', { count })}
          className="absolute right-0.5 bottom-0.5 rounded bg-card/90 px-1 font-mono text-[9px] leading-3 text-accent ring-1 ring-accent/40"
        >
          ×{count}
        </span>
      )}
    </button>
  )
})

export default function TracksWall() {
  const { unit } = useUnit()
  const { t } = useLocale()
  const [searchParams] = useSearchParams()

  const [list, setList] = useState<Track[]>([])
  const [error, setError] = useState<string | null>(null)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [year, setYear] = useState<number | 'all'>('all')
  const [distMin, setDistMin] = useState(0)
  const [sortBy, setSortBy] = useState<'date' | 'distance'>('date')
  /** 地图全屏（覆盖层，Esc 或点 × 退出） */
  const [mapFullscreen, setMapFullscreen] = useState(false)

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
    { label: t('wallAll'), min: 0 },
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
  if (selectedId === null && list.length > 0) {
    const fromUrl = Number(searchParams.get('activity'))
    if (fromUrl && list.some((t2) => t2.id === fromUrl)) setSelectedId(fromUrl)
  }

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

  // 容器尺寸变化（进入/退出全屏）后必须让 MapLibre 重新测量，否则画面会糊/留白
  useEffect(() => {
    const map = mapObj.current
    if (!map) return
    const t = setTimeout(() => map.resize(), 120) // 等 DOM 布局稳定
    return () => clearTimeout(t)
  }, [mapFullscreen])

  useEffect(() => {
    if (!mapFullscreen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMapFullscreen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [mapFullscreen])

  const selectTrack = useCallback((t2: Track) => {
    setSelectedId((cur) => (cur === t2.id ? null : t2.id))
  }, [])

  // 缩略图网格（右栏与全屏浮层共用同一份渲染，避免两处 JSX 漂移）
  const trackGrid = (
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
  )


  const selSecs = selected ? selected.durationSecs : 0
  const selPace = selected && selected.distanceM > 0 ? selSecs / (selected.distanceM / 1000) : 0

  return (
    <main className="mx-auto max-w-[1400px] px-4 py-6 sm:px-6">
      <div className="mb-5 flex items-center gap-4">
        <h1 className="text-lg font-bold">{t('wallTitle')}</h1>
        <span className="text-xs text-muted-foreground">
          {t('wallCount', { routes: sorted.length, acts: base.length })}
        </span>
        <Link to="/stats" className="ml-auto text-sm text-secondary hover:underline">
          {t('wallYearlyLink')}
        </Link>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      <div className="grid grid-cols-1 items-start gap-5 lg:grid-cols-[320px_1fr]">
        {/* 左栏：统计 + 选中详情 + 小地图（sticky） */}
        <div
          className={
            // 全屏时不能 sticky：position: sticky 会为本栏建立 stacking context，
            // 里面的 fixed 地图的 z-index 就被限制在本栏内，盖不住页面其他部分。
            mapFullscreen ? 'flex flex-col gap-4' : 'flex flex-col gap-4 lg:sticky lg:top-20'
          }
        >
          <div className="rounded-xl border border-border bg-card p-4">
            <p className="mb-3 text-[10px] tracking-wider text-muted-foreground uppercase">
              {year === 'all' ? t('wallAll') : year}
            </p>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                  {t('wallStatActivities')}
                </p>
                <p className="font-mono text-2xl font-bold text-accent">{base.length}</p>
              </div>
              <div>
                <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                  {t('wallStatDistance')}
                </p>
                <p className="font-mono text-2xl font-bold">
                  {formatDistance(totalDist, unit)}
                </p>
              </div>
              <div>
                <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                  {t('wallStatTime')}
                </p>
                <p className="font-mono text-lg font-bold">
                  {Math.floor(totalTime / 3600)}h {Math.floor((totalTime % 3600) / 60)}m
                </p>
              </div>
              {avgPace > 0 && (
                <div>
                  <p className="text-[10px] tracking-wider text-muted-foreground uppercase">
                    {t('wallStatAvgPace')}
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
                  {t('wallSelected')}
                </p>
                <button
                  onClick={() => setSelectedId(null)}
                  aria-label={t('wallClear')}
                  className="cursor-pointer text-muted-foreground hover:text-foreground"
                >
                  ✕
                </button>
              </div>
              <p className="mb-0.5 text-xs font-semibold">
                {SPORT_KEYS[selected.sport] ? t(SPORT_KEYS[selected.sport]) : selected.sport} ·{' '}
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
                    {t('wallDist')}
                  </p>
                  <p className="font-mono text-sm font-bold">
                    {formatDistance(selected.distanceM, unit, 2)}
                  </p>
                </div>
                <div>
                  <p className="text-[9px] tracking-wider text-muted-foreground uppercase">
                    {t('wallTime')}
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
                      {t('wallPace')}
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
                  {t('wallDetails')}
                </Link>
              </div>
            </div>
          )}

          <div
            className={
              mapFullscreen
                ? 'fixed inset-0 z-50 flex bg-background'
                : 'relative overflow-hidden rounded-xl border border-border bg-card'
            }
          >
            {/* 全屏时左侧列表与地图并排（不再用叠加浮层：
                普通 DOM 压在 MapLibre 的 WebGL canvas 上时，在本项目的构建/渲染路径下
                只有边缘一小条能被绘制出来，实测浮层 288px 宽只画出 48px） */}
            {mapFullscreen && (
              <aside className="flex w-56 shrink-0 flex-col border-r border-border bg-card p-2">
                <div className="mb-1.5 flex items-baseline gap-2 border-b border-border pb-1 text-xs">
                  <span className="font-medium text-foreground">
                    {t('wallTracks')}
                  </span>
                  <span className="text-muted-foreground">{sorted.length}</span>
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto">
                  <div className="flex flex-wrap gap-0.5">
                    {sorted.map((c) => (
                      <TrackThumb
                        key={c.rep.id}
                        cluster={c}
                        unit={unit}
                        selected={c.rep.id === selectedId}
                        onSelect={selectTrack}
                        size={40}
                        showCount={false}
                      />
                    ))}
                  </div>
                </div>
              </aside>
            )}
            <div className={mapFullscreen ? 'relative min-w-0 flex-1' : 'contents'}>
              <div
                ref={mapBox}
                className={mapFullscreen ? 'h-full w-full' : 'h-64 w-full'}
                aria-label={t('wallSelectedMapAria')}
              />
            {/* 全屏时的缩略图浮层：贴在左上角，等比缩到 40%
                （transform 不改变布局尺寸，所以外层用 wrapper 按 0.4 倍占位） */}
              <button
                onClick={() => setMapFullscreen((v) => !v)}
                title={mapFullscreen ? t('wallExitFullscreenTitle') : t('wallFullscreenTitle')}
                aria-label={mapFullscreen ? t('wallExitFullscreenTitle') : t('wallFullscreenTitle')}
                className="absolute top-2 right-2 z-10 cursor-pointer rounded border border-border bg-card px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
              >
                {mapFullscreen ? t('wallExitFullscreen') : t('wallFullscreen')}
              </button>
            </div>
          </div>
        </div>

        {/* 右栏：筛选 + 轨迹墙。全屏时不渲染 —— 否则它的缩略图墙会盖在 fixed 地图上面
            （左栏的 lg:sticky 建立了 stacking context，地图的 z-50 只在其内部有效） */}
        {!mapFullscreen && (
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
                {y === 'all' ? t('wallAll') : y}
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
              aria-label={t('wallSortAria')}
            >
              <option value="date">{t('wallSortDate')}</option>
              <option value="distance">{t('wallSortDistance')}</option>
            </select>
          </div>

          {sorted.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t('wallNoTracks')}
            </p>
          ) : (
            <>
              {trackGrid}
              <p className="mt-3 border-t border-border pt-2 text-[11px] leading-relaxed text-muted-foreground">
                <span className="rounded bg-card px-1 font-mono text-accent ring-1 ring-accent/40">×N</span>{' '}
                {t('wallNoteA')}<b>{t('wallNoteBold')}</b>{t('wallNoteTail')}
              </p>
            </>
          )}
        </div>
        )}
      </div>
    </main>
  )
}
