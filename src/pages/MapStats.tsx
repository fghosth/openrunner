import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import * as maplibregl from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
// 固定使用 maplibre-gl 5.x：其瓦片解码 worker 以内联 blob 方式创建
// （dist 单文件 + URL.createObjectURL），不受 Vite 打包影响。
// 6.x 改为 `new Worker(url, {type:'module'})` 外部 worker，在 Vite 生产构建下
// 握手消息发不出来 —— 表现为「瓦片 0 请求、控制台无报错」。
import { annualStats, tracks, type AnnualStat, type Track } from '../api'
import { readActiveAthlete } from '../App'
import { sportNames } from '../format'
import { baseMaps, wgs84ToGcj02 } from '../coord'

const sportColors: Record<string, string> = {
  run: '#3b82f6',
  ride: '#d97706',
  swim: '#22c55e',
  other: '#94a3b8',
}

const SOURCE_ID = 'tracks'
const LINE_LAYER = 'tracks-line'
const CLUSTER_SOURCE = 'track-clusters'
const CLUSTER_LAYER = 'track-clusters-circle'
const CLUSTER_LABEL = 'track-clusters-label'

/**
 * 网格聚类：把相近的轨迹归成一簇。
 *
 * 为什么不用 worker：running_page 用 `clusterTracks.worker.ts` 是因为它要处理
 * 完整轨迹点（几十万点）。这里只对**每条轨迹一个代表点**做聚类（323 个点），
 * 纯前端同步计算只需几毫秒，没必要引入 worker 的复杂度。
 */
function clusterTracks(
  tracks: { id: number; coords: [number, number][]; sport: string }[],
  gridDeg: number,
) {
  type Cluster = {
    key: string
    lat: number
    lng: number
    count: number
    ids: number[]
    minLat: number
    maxLat: number
    minLng: number
    maxLng: number
  }
  const buckets = new Map<string, Cluster>()
  for (const t of tracks) {
    if (t.coords.length === 0) continue
    const mid = t.coords[Math.floor(t.coords.length / 2)]
    const [lng, lat] = mid
    const key = `${Math.round(lat / gridDeg)}:${Math.round(lng / gridDeg)}`
    const cur = buckets.get(key)
    if (cur) {
      cur.count++
      cur.ids.push(t.id)
      cur.lat = (cur.lat * (cur.count - 1) + lat) / cur.count
      cur.lng = (cur.lng * (cur.count - 1) + lng) / cur.count
      cur.minLat = Math.min(cur.minLat, lat)
      cur.maxLat = Math.max(cur.maxLat, lat)
      cur.minLng = Math.min(cur.minLng, lng)
      cur.maxLng = Math.max(cur.maxLng, lng)
    } else {
      buckets.set(key, {
        key, lat, lng, count: 1, ids: [t.id],
        minLat: lat, maxLat: lat, minLng: lng, maxLng: lng,
      })
    }
  }
  return [...buckets.values()]
}

/**
 * 重试执行 fn 直到它返回 true（最多约 6 秒）。
 *
 * 为什么不用 `map.isStyleLoaded()` / `map.once('load')`：
 *  - `once('load')` 若在 load 已触发后注册则永不执行；
 *  - `isStyleLoaded()` 在**任何 source 正在加载瓦片**时同样返回 false（并非只表示
 *    style 未解析），本项目高德 raster 一直在拉瓦片，于是判断永远为假 ——
 *    实测表现为「初始选中不缩放、无弹层，手动点列表项却正常」。
 * 所以改成「直接试着做，失败就稍后重试」，供 addSource/addLayer/高亮这类
 * 依赖图层存在但时机不确定的操作使用。
 */
function whenReady(fn: () => boolean, tries = 0) {
  if (fn()) return
  if (tries >= 60) return
  setTimeout(() => whenReady(fn, tries + 1), 100)
}

/** 高亮选中轨迹：亮橙、加粗；其余轨迹压淡作背景（提示「别的日子也在这附近跑」）。 */
function applyHighlight(map: maplibregl.Map, id: number | null): boolean {
  if (!map.getLayer(LINE_LAYER)) return false
  const key = id === null ? '' : String(id)
  map.setPaintProperty(LINE_LAYER, 'line-width', ['case', ['==', ['get', 'id'], key], 4, 1.5])
  map.setPaintProperty(LINE_LAYER, 'line-opacity', ['case', ['==', ['get', 'id'], key], 1, 0.15])
  map.setPaintProperty(LINE_LAYER, 'line-color', [
    'case', ['==', ['get', 'id'], key], '#f97316', ['get', 'color'],
  ])
  return true
}

/** 地图轨迹页（票 11）：左侧活动列表 + 右侧单条轨迹聚焦。
 *
 * 为什么不是「把所有轨迹叠在一张图」：497 次活动里有 323 条 GPS 轨迹，全部 fitBounds
 * 会把上海缩成一小块，线糊成一团，根本看不出「哪天在哪儿跑的」。所以改成列表驱动：
 * 默认聚焦最新一条，点列表/点地图切换，其余轨迹淡化作背景。
 */
export default function MapView() {
  const containerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<maplibregl.Map | null>(null)
  const popupRef = useRef<maplibregl.Popup | null>(null)
  const listRef = useRef<HTMLUListElement>(null)

  const [searchParams, setSearchParams] = useSearchParams()
  const [trackList, setTrackList] = useState<Track[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [sportFilter, setSportFilter] = useState<string>('run')
  /** 视图模式：focus=单条聚焦（默认），cluster=聚合看分布 */
  const [viewMode, setViewMode] = useState<'focus' | 'cluster'>('focus')
  const [query, setQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')
  // 图层可能晚于选中态就绪（style 未 load 时 addLayer 要等 load 事件），
  // 用 ref 记住当前选中值，在图层加好后重放一次高亮。
  const selectedRef = useRef<number | null>(null)

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) return
    try {
      setTrackList(await tracks(id, 800))
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // 轨迹坐标预转 GCJ-02（323 条 × 最多 800 点，缓存避免每次选中都重算）
  const gcjTracks = useMemo(() => {
    return trackList.map((t) => ({
      ...t,
      coords: t.points
        .map(([lat, lon]) => {
          const [la, lo] = wgs84ToGcj02(lat, lon)
          return [lo, la] as [number, number]
        })
        .filter(([lo, la]) => Number.isFinite(lo) && Number.isFinite(la)),
    }))
  }, [trackList])

  // 列表（筛选 + 搜索 + 时间倒序）
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    return gcjTracks
      .filter((t) => (sportFilter === 'all' ? true : t.sport === sportFilter))
      .filter((t) => {
        if (!q) return true
        const d = new Date(t.startTime * 1000).toLocaleDateString('zh-CN')
        const km = (t.distanceM / 1000).toFixed(2)
        return d.includes(q) || km.includes(q) || (sportNames[t.sport] ?? '').includes(q)
      })
      .sort((a, b) => b.startTime - a.startTime)
  }, [gcjTracks, sportFilter, query])

  // 初始选中：URL ?activity=ID 优先，否则最新一条
  useEffect(() => {
    if (selectedId !== null || visible.length === 0) return
    const fromUrl = Number(searchParams.get('activity'))
    const found = visible.find((t) => t.id === fromUrl)
    setSelectedId(found ? found.id : visible[0].id)
  }, [visible, selectedId, searchParams])

  // 建图（仅一次）
  useEffect(() => {
    if (!containerRef.current) return
    setStatus('loading')

    const map = new maplibregl.Map({
      container: containerRef.current,
      style: baseMaps.amap.style as maplibregl.StyleSpecification,
      center: [121.47, 31.23],
      zoom: 11,
      attributionControl: { compact: true },
    })
    mapRef.current = map
    ;(window as unknown as { __map?: maplibregl.Map }).__map = map

    const popup = new maplibregl.Popup({ closeButton: false, offset: 10 })
    popupRef.current = popup

    map.on('load', () => setStatus('ready'))
    map.on('error', (e: { error?: { message?: string } }) => {
      const msg = e?.error?.message ?? String(e)
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const w = window as any
      w.__maperr = (w.__maperr ?? []).concat(msg)
      console.warn('map error', msg)
      setStatus((st) => (st === 'ready' ? st : 'error'))
    })

    map.on('mouseenter', LINE_LAYER, () => {
      map.getCanvas().style.cursor = 'pointer'
    })
    map.on('mouseleave', LINE_LAYER, () => {
      map.getCanvas().style.cursor = ''
    })
    // 点地图上的轨迹 → 选中它（与列表联动）
    map.on('click', LINE_LAYER, (e) => {
      const id = Number(e.features?.[0]?.properties?.id)
      if (Number.isFinite(id)) setSelectedId(id)
    })

    return () => {
      popup.remove()
      popupRef.current = null
      map.remove()
      mapRef.current = null
    }
  }, [])

  // 轨迹数据 → GeoJSON（source 只建一次，后续 setData）
  useEffect(() => {
    const map = mapRef.current
    if (!map || gcjTracks.length === 0) return

    const data = {
      type: 'FeatureCollection',
      features: gcjTracks
        .filter((t) => t.coords.length >= 2)
        .map((t) => ({
          type: 'Feature' as const,
          properties: {
            id: String(t.id),
            color: sportColors[t.sport] ?? sportColors.other,
          },
          geometry: { type: 'LineString' as const, coordinates: t.coords },
        })),
    }

    const apply = (): boolean => {
      const src = map.getSource(SOURCE_ID) as maplibregl.GeoJSONSource | undefined
      if (src) {
        src.setData(data)
        return true
      }
      try {
        map.addSource(SOURCE_ID, { type: 'geojson', data })
        map.addLayer({
          id: LINE_LAYER,
          type: 'line',
          source: SOURCE_ID,
          layout: { 'line-join': 'round', 'line-cap': 'round' },
          paint: {
            'line-color': ['get', 'color'],
            'line-width': 1.5,
            'line-opacity': 0.15,
          },
        })
      } catch {
        return false // style 未就绪，稍后重试（addSource 是幂等的，上面已判断）
      }
      // 图层是这一刻才有的：把已经选中的轨迹高亮补上
      applyHighlight(map, selectedRef.current)
      return true
    }

    whenReady(apply)
  }, [gcjTracks])

  // 聚合视图：按网格聚类后画圆点（半径随数量），点击簇缩放到该区域
  useEffect(() => {
    const map = mapRef.current
    if (!map) return
    const isCluster = viewMode === 'cluster'

    const applyCluster = (): boolean => {
      if (!map.getLayer(CLUSTER_LAYER)) {
        try {
          map.addSource(CLUSTER_SOURCE, {
            type: 'geojson',
            data: { type: 'FeatureCollection', features: [] },
          })
        } catch {
          return false // style 未就绪，稍后重试
        }
        map.addLayer({
          id: CLUSTER_LAYER,
          type: 'circle',
          source: CLUSTER_SOURCE,
          paint: {
            'circle-radius': ['interpolate', ['linear'], ['get', 'count'], 1, 6, 50, 14, 300, 24],
            'circle-color': '#f97316',
            'circle-opacity': 0.55,
            'circle-stroke-color': '#fb923c',
            'circle-stroke-width': 1.5,
          },
        })
        map.addLayer({
          id: CLUSTER_LABEL,
          type: 'symbol',
          source: CLUSTER_SOURCE,
          layout: {
            'text-field': ['get', 'label'],
            'text-size': 11,
          },
          paint: { 'text-color': '#0f172a' },
        })
      }
      const src = map.getSource(CLUSTER_SOURCE) as maplibregl.GeoJSONSource | undefined
      if (!src) return false

      if (!isCluster) {
        src.setData({ type: 'FeatureCollection', features: [] })
        return true
      }

      // 网格大小按当前缩放自适应：放大后格子更密，能看到更细的分布
      const z = map.getZoom()
      const gridDeg = z >= 14 ? 0.002 : z >= 12 ? 0.008 : z >= 10 ? 0.03 : 0.15
      const clusters = clusterTracks(
        gcjTracks.filter((t) => (sportFilter === 'all' ? true : t.sport === sportFilter)),
        gridDeg,
      )
      src.setData({
        type: 'FeatureCollection',
        features: clusters.map((c) => ({
          type: 'Feature' as const,
          properties: {
            label: String(c.count),
            count: c.count,
            midLat: (c.minLat + c.maxLat) / 2,
            midLng: (c.minLng + c.maxLng) / 2,
            spanLat: c.maxLat - c.minLat,
            spanLng: c.maxLng - c.minLng,
          },
          geometry: { type: 'Point' as const, coordinates: [c.lng, c.lat] },
        })),
      })
      return true
    }

    whenReady(applyCluster)

    // 聚合模式下淡化单条轨迹线，突出簇
    whenReady(() => {
      if (!map.getLayer(LINE_LAYER)) return false
      map.setPaintProperty(LINE_LAYER, 'line-opacity', isCluster ? 0.05 : 0.15)
      return true
    })

    const onClick = (e: maplibregl.MapLayerMouseEvent) => {
      const f = e.features?.[0]
      if (!f) return
      const pr = f.properties as {
        midLat: number; midLng: number; spanLat: number; spanLng: number
      }
      const pad = 0.01
      map.fitBounds(
        [
          [pr.midLng - pr.spanLng / 2 - pad, pr.midLat - pr.spanLat / 2 - pad],
          [pr.midLng + pr.spanLng / 2 + pad, pr.midLat + pr.spanLat / 2 + pad],
        ],
        { padding: 60, duration: 500, maxZoom: 15 },
      )
    }
    map.on('click', CLUSTER_LAYER, onClick)
    return () => {
      map.off('click', CLUSTER_LAYER, onClick)
    }
  }, [viewMode, gcjTracks, sportFilter])

  // 选中态：缩放到该轨迹 + 高亮（fitBounds 不依赖图层，避免被 load 时序吃掉）
  useEffect(() => {
    selectedRef.current = selectedId
    const map = mapRef.current
    if (!map || selectedId === null) return
    const chosen = gcjTracks.find((t) => t.id === selectedId)
    if (!chosen || chosen.coords.length < 2) return

    // fitBounds 与 popup 不依赖 style 是否就绪，直接执行；只有高亮要等图层。
    const fit = () => {
      const bounds = new maplibregl.LngLatBounds()
      for (const c of chosen.coords) bounds.extend(c as maplibregl.LngLatLike)
      map.fitBounds(bounds, { padding: 60, maxZoom: 16, duration: 600 })

      const popup = popupRef.current
      const mid = chosen.coords[Math.floor(chosen.coords.length / 2)]
      if (popup && mid) {
        popup
          .setLngLat(mid as maplibregl.LngLatLike)
          .setHTML(
            `<a href="/activities/${chosen.id}" style="font-weight:600">` +
              `${sportNames[chosen.sport] ?? chosen.sport} · ${(chosen.distanceM / 1000).toFixed(2)} km</a><br/>` +
              `<span style="font-size:11px;opacity:.7">` +
              `${new Date(chosen.startTime * 1000).toLocaleString('zh-CN', { hour12: false })}</span>`,
          )
          .addTo(map)
      }
    }

    fit()
    whenReady(() => applyHighlight(map, selectedId))
  }, [selectedId, gcjTracks])

  // 选中项变化时同步 URL（可直接分享 / 从活动列表跳转）
  function select(track: Track) {
    setSelectedId(track.id)
    setSearchParams({ activity: String(track.id) }, { replace: true })
    // 列表滚动到可见
    requestAnimationFrame(() => {
      listRef.current?.querySelector(`[data-id="${track.id}"]`)?.scrollIntoView({ block: 'nearest' })
    })
  }

  const selected = gcjTracks.find((t) => t.id === selectedId)

  return (
    <main className="mx-auto max-w-6xl px-6 py-8">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-semibold">轨迹地图</h1>
        <span className="text-sm text-muted-foreground">
          {visible.length} 条轨迹{sportFilter !== 'all' && `（${sportNames[sportFilter] ?? sportFilter}）`}
        </span>
        <div className="ml-auto flex items-center gap-1 text-xs">
          {(['focus', 'cluster'] as const).map((m) => (
            <button
              key={m}
              onClick={() => setViewMode(m)}
              className={`cursor-pointer rounded px-2 py-1 ${
                viewMode === m
                  ? 'bg-primary text-primary-foreground'
                  : 'border border-border text-muted-foreground hover:text-foreground'
              }`}
            >
              {m === 'focus' ? '单条聚焦' : '聚合分布'}
            </button>
          ))}
          <Link to="/stats" className="ml-2 text-secondary hover:underline">
            年度统计 →
          </Link>
        </div>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}
      {status === 'error' && (
        <p role="status" className="mb-2 rounded border border-accent bg-card p-2 text-xs text-accent">
          底图加载失败 —— 高德瓦片请求失败（检查网络/代理）。轨迹线仍会显示。
        </p>
      )}

      <div className="grid gap-4 lg:grid-cols-[300px_1fr]">
        {/* 左：活动列表（筛选/搜索/点选） */}
        <aside className="flex max-h-[34rem] flex-col rounded-lg border border-border bg-card">
          <div className="space-y-2 border-b border-border p-3">
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索日期或距离…"
              className="w-full rounded border border-border bg-muted px-2 py-1 text-sm"
            />
            <div className="flex gap-1 text-xs">
              {[
                ['run', '跑步'],
                ['ride', '骑行'],
                ['all', '全部'],
              ].map(([key, label]) => (
                <button
                  key={key}
                  onClick={() => setSportFilter(key)}
                  className={`cursor-pointer rounded px-2 py-1 ${
                    sportFilter === key
                      ? 'bg-primary text-primary-foreground'
                      : 'border border-border text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>

          <ul ref={listRef} className="min-h-0 flex-1 overflow-y-auto">
            {visible.length === 0 && (
              <li className="p-3 text-sm text-muted-foreground">没有符合条件的轨迹</li>
            )}
            {visible.map((t) => {
              const active = t.id === selectedId
              const km = t.distanceM / 1000
              const pace = km > 0.05 ? t.durationSecs / km : 0
              return (
                <li key={t.id} data-id={t.id}>
                  <button
                    onClick={() => select(t)}
                    className={`flex w-full cursor-pointer items-baseline gap-2 border-b border-border/60 px-3 py-2 text-left text-sm hover:bg-muted ${
                      active ? 'bg-muted' : ''
                    }`}
                  >
                    <span
                      className="mt-0.5 inline-block h-2 w-2 shrink-0 rounded-full"
                      style={{ background: active ? '#f97316' : sportColors[t.sport] ?? sportColors.other }}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-mono text-xs text-muted-foreground">
                        {new Date(t.startTime * 1000).toLocaleString('zh-CN', {
                          month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
                        })}
                      </span>
                      <span className={`block ${active ? 'font-semibold text-foreground' : ''}`}>
                        {km.toFixed(2)} km
                        {pace > 0 && (
                          <span className="ml-2 text-xs text-muted-foreground">
                            {Math.floor(pace / 60)}:{String(Math.round(pace % 60)).padStart(2, '0')}/km
                          </span>
                        )}
                      </span>
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
        </aside>

        {/* 右：地图 */}
        <div className="space-y-2">
          <div
            ref={containerRef}
            className="h-[30rem] w-full overflow-hidden rounded-lg border border-border"
            role="img"
            aria-label="活动轨迹地图"
          />
          <p className="text-xs text-muted-foreground">
            {selected ? (
              <>
                <span className="text-foreground">
                  {sportNames[selected.sport] ?? selected.sport} · {(selected.distanceM / 1000).toFixed(2)} km
                </span>
                {' · '}
                {new Date(selected.startTime * 1000).toLocaleString('zh-CN', { hour12: false })}
                {' · '}
                <Link to={`/activities/${selected.id}`} className="text-secondary hover:underline">
                  查看详情 →
                </Link>
              </>
            ) : (
              '从左侧选一条轨迹查看它跑在哪里。'
            )}
          </p>
        </div>
      </div>
    </main>
  )
}

/** 年度统计页（票 11）：总距离/次数/活跃天数/连续天数 + 月度柱图。 */
export function StatsView() {
  const [year, setYear] = useState(new Date().getFullYear())
  const [stat, setStat] = useState<(AnnualStat & { streaks: { longest: number; current: number } }) | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    annualStats(id, year)
      .then(setStat)
      .catch((err: Error) => setError(err.message))
  }, [year])

  const maxMonth = stat ? Math.max(1, ...(stat.monthly ?? []).map((m) => m.distanceM)) : 1

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <div className="mb-6 flex items-center gap-4">
        <h1 className="text-2xl font-semibold">年度统计</h1>
        <div className="ml-auto flex items-center gap-2 text-sm">
          <button
            onClick={() => setYear((y) => y - 1)}
            className="cursor-pointer rounded border border-border px-2 py-1 text-muted-foreground hover:text-foreground"
          >
            ←
          </button>
          <span className="font-mono">{year}</span>
          <button
            onClick={() => setYear((y) => y + 1)}
            className="cursor-pointer rounded border border-border px-2 py-1 text-muted-foreground hover:text-foreground"
          >
            →
          </button>
          <button
            onClick={async () => {
              const id = readActiveAthlete()
              if (!id) return
              try {
                const { downloadPoster } = await import('../api')
                await downloadPoster(id, year)
              } catch (err) {
                setError(err instanceof Error ? err.message : '生成海报失败')
              }
            }}
            className="ml-2 cursor-pointer rounded border border-border px-3 py-1 text-muted-foreground hover:text-foreground"
            title="下载年度 SVG 海报（可分享）"
          >
            下载海报 SVG
          </button>
          <Link to="/map" className="ml-2 text-secondary hover:underline">
            地图 →
          </Link>
        </div>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {stat && (
        <>
          <dl className="mb-6 grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-5">
            {[
              ['总距离', `${((stat.totalDistanceM ?? 0) / 1000).toFixed(1)} km`],
              ['活动次数', String(stat.activityCount ?? 0)],
              ['活跃天数', String(stat.activeDays ?? 0)],
              ['最长连续', `${stat.streaks?.longest ?? 0} 天`],
              ['当前连续', `${stat.streaks?.current ?? 0} 天`],
            ].map(([k, v]) => (
              <div key={k} className="bg-card px-4 py-3">
                <dt className="text-xs text-muted-foreground">{k}</dt>
                <dd className="mt-1 font-mono text-sm">{v}</dd>
              </div>
            ))}
          </dl>

          <section aria-label="月度距离" className="rounded-lg border border-border bg-card p-4">
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">月度距离（km）</h2>
            <div className="flex h-40 items-end gap-1">
              {(stat.monthly ?? []).map((m) => {
                const pct = (m.distanceM / maxMonth) * 100
                return (
                  <div key={m.month} className="flex flex-1 flex-col items-center gap-1">
                    <div className="flex h-32 w-full items-end">
                      <div
                        className="w-full rounded-t bg-secondary/70"
                        style={{ height: `${Math.max(pct, m.distanceM > 0 ? 3 : 0)}%` }}
                        title={`${m.month} 月：${(m.distanceM / 1000).toFixed(1)} km · ${m.count} 次`}
                      />
                    </div>
                    <span className="text-xs text-muted-foreground">{m.month}</span>
                  </div>
                )
              })}
            </div>
          </section>
        </>
      )}
    </main>
  )
}
