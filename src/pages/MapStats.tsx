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

/**
 * style 就绪后执行 fn。
 *
 * 不能用 `map.once('load', fn)`：style 数据到达与 `isStyleLoaded()` 转真之间存在窗口，
 * 若注册时 load 已经触发过，回调永远不会执行（实测表现为「初始选中的轨迹不缩放、
 * 不高亮，但手动点列表项却正常」）。这里改为短轮询，最多等 5 秒。
 */
function whenStyleReady(map: maplibregl.Map, fn: () => void, tries = 0) {
  if (map.isStyleLoaded()) {
    fn()
    return
  }
  if (tries >= 50) return
  setTimeout(() => whenStyleReady(map, fn, tries + 1), 100)
}

/** 高亮选中轨迹：亮橙、加粗；其余轨迹压淡作背景（提示「别的日子也在这附近跑」）。 */
function applyHighlight(map: maplibregl.Map, id: number | null) {
  if (!map.getLayer(LINE_LAYER)) return
  const key = id === null ? '' : String(id)
  map.setPaintProperty(LINE_LAYER, 'line-width', ['case', ['==', ['get', 'id'], key], 4, 1.5])
  map.setPaintProperty(LINE_LAYER, 'line-opacity', ['case', ['==', ['get', 'id'], key], 1, 0.15])
  map.setPaintProperty(LINE_LAYER, 'line-color', [
    'case', ['==', ['get', 'id'], key], '#f97316', ['get', 'color'],
  ])
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

    const apply = () => {
      const src = map.getSource(SOURCE_ID) as maplibregl.GeoJSONSource | undefined
      if (src) {
        src.setData(data)
        return
      }
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
      // 图层是这一刻才有的：把已经选中的轨迹高亮补上
      applyHighlight(map, selectedRef.current)
    }

    whenStyleReady(map, apply)
  }, [gcjTracks])

  // 选中态：缩放到该轨迹 + 高亮（fitBounds 不依赖图层，避免被 load 时序吃掉）
  useEffect(() => {
    selectedRef.current = selectedId
    const map = mapRef.current
    if (!map || selectedId === null) return
    const chosen = gcjTracks.find((t) => t.id === selectedId)
    if (!chosen || chosen.coords.length < 2) return

    const fit = () => {
      const bounds = new maplibregl.LngLatBounds()
      for (const c of chosen.coords) bounds.extend(c as maplibregl.LngLatLike)
      map.fitBounds(bounds, { padding: 60, maxZoom: 16, duration: 600 })
      applyHighlight(map, selectedId)

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

    whenStyleReady(map, fit)
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
        <Link to="/stats" className="ml-auto text-sm text-secondary hover:underline">
          年度统计 →
        </Link>
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
