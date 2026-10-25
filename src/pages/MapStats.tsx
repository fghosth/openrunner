import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import * as L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { annualStats, tracks, type AnnualStat, type Track } from '../api'
import { readActiveAthlete } from '../App'
import { sportNames } from '../format'
import { tileConfigs, wgs84ToGcj02, type TileProvider } from '../coord'

const sportColors: Record<string, string> = {
  run: '#3b82f6',
  ride: '#d97706',
  swim: '#22c55e',
  other: '#94a3b8',
}

const PROVIDER_KEY = 'openrunner.mapTiles'

/** 地图轨迹页（票 11）：全部活动轨迹 + 底图源切换（高德 GCJ-02 / OSM WGS-84）。 */
export default function MapView() {
  const mapRef = useRef<HTMLDivElement>(null)
  const mapObj = useRef<L.Map | null>(null)
  const tileLayer = useRef<L.TileLayer | null>(null)
  const layerGroup = useRef<L.LayerGroup | null>(null)
  const [trackList, setTrackList] = useState<Track[]>([])
  const [error, setError] = useState<string | null>(null)
  const [provider, setProvider] = useState<TileProvider>(() => {
    const saved = localStorage.getItem(PROVIDER_KEY)
    return saved === 'osm' ? 'osm' : 'amap' // 默认高德：国内可直连
  })
  const [tilesLoaded, setTilesLoaded] = useState(0)
  const [mapReady, setMapReady] = useState(false)

  const refresh = useCallback(async () => {
    const id = readActiveAthlete()
    if (!id) return
    try {
      setTrackList(await tracks(id))
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // 按 provider 建/换底图（依赖 mapReady：确保 map 实例先就绪，
  // 否则 effect 先于 map 初始化执行会永远跳过瓦片层创建——曾导致 tiles=0 空白底图）
  useEffect(() => {
    if (!mapRef.current || !mapObj.current || !mapReady) return
    const cfg = tileConfigs[provider]
    if (tileLayer.current) {
      mapObj.current.removeLayer(tileLayer.current)
      tileLayer.current = null
    }
    setTilesLoaded(0)
    const layer = L.tileLayer(cfg.url, {
      maxZoom: cfg.maxZoom,
      attribution: cfg.attribution,
      subdomains: cfg.subdomains ?? 'abc',
    })
    layer.on('load', () => setTilesLoaded((n) => n + 1))
    layer.addTo(mapObj.current)
    tileLayer.current = layer
    localStorage.setItem(PROVIDER_KEY, provider)
  }, [provider, mapReady])

  useEffect(() => {
    if (!mapRef.current || mapObj.current) return
    const map = L.map(mapRef.current, { worldCopyJump: true }).setView([31.23, 121.47], 12)
    mapObj.current = map
    layerGroup.current = L.layerGroup().addTo(map)
    setMapReady(true) // 触发瓦片层创建
    return () => {
      setMapReady(false)
      map.remove()
      mapObj.current = null
      layerGroup.current = null
      tileLayer.current = null
    }
  }, [])

  // 画轨迹（按底图坐标系决定是否做 WGS-84 → GCJ-02 转换）
  useEffect(() => {
    if (!mapObj.current || !layerGroup.current) return
    layerGroup.current.clearLayers()
    const cfg = tileConfigs[provider]
    const bounds: L.LatLngTuple[] = []

    for (const t of trackList) {
      const latlngs = t.points.map(([lat, lon]) => {
        const [la, lo] = cfg.needsGcj ? wgs84ToGcj02(lat, lon) : [lat, lon]
        return [la, lo] as L.LatLngTuple
      })
      if (latlngs.length < 2) continue
      bounds.push(...latlngs)
      const color = sportColors[t.sport] ?? sportColors.other
      L.polyline(latlngs, { color, weight: 2, opacity: 0.75 })
        .bindPopup(
          `<a href="/activities/${t.id}" style="color:${color}">${sportNames[t.sport] ?? t.sport} · ${(
            t.distanceM / 1000
          ).toFixed(2)} km</a><br/><span style="font-size:11px;opacity:.7">${new Date(
            t.startTime * 1000,
          ).toLocaleDateString('zh-CN')}</span>`,
        )
        .addTo(layerGroup.current!)
    }
    if (bounds.length > 0) {
      mapObj.current.fitBounds(L.latLngBounds(bounds).pad(0.1))
    }
  }, [trackList, provider])

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-semibold">轨迹地图</h1>
        <span className="text-sm text-muted-foreground">{trackList.length} 条有 GPS 的活动</span>
        <div className="ml-auto flex items-center gap-2 text-sm">
          <label className="text-muted-foreground" htmlFor="tiles">
            底图
          </label>
          <select
            id="tiles"
            value={provider}
            onChange={(e) => setProvider(e.target.value as TileProvider)}
            className="rounded border border-border bg-muted px-2 py-1"
          >
            <option value="amap">高德（国内推荐）</option>
            <option value="osm">OpenStreetMap（海外）</option>
          </select>
          <Link to="/stats" className="text-secondary hover:underline">
            年度统计 →
          </Link>
        </div>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      {tilesLoaded === 0 && (
        <p role="status" className="mb-2 rounded border border-accent bg-card p-2 text-xs text-accent">
          底图加载中… 若长时间空白，说明当前底图源不可达（国内建议选「高德」，海外选 OSM）。
          轨迹线仍会正常显示。
        </p>
      )}

      <div
        ref={mapRef}
        className="h-[32rem] w-full overflow-hidden rounded-lg border border-border"
        role="img"
        aria-label="活动轨迹地图"
      />

      <p className="mt-2 text-xs text-muted-foreground">
        颜色：蓝=跑步 · 琥珀=骑行 · 绿=游泳 · 灰=其他。点击轨迹进入详情。
        {provider === 'amap' && '高德底图为 GCJ-02 坐标系，轨迹已自动做 WGS-84 → GCJ-02 转换。'}
      </p>
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
