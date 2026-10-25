import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import * as maplibregl from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { annualStats, tracks, type AnnualStat, type Track } from '../api'
import { readActiveAthlete } from '../App'
import { sportNames } from '../format'
import { baseMaps, wgs84ToGcj02, type BaseMapProvider } from '../coord'

const sportColors: Record<string, string> = {
  run: '#3b82f6',
  ride: '#d97706',
  swim: '#22c55e',
  other: '#94a3b8',
}

const PROVIDER_KEY = 'openrunner.mapProvider'
const SOURCE_ID = 'tracks'
const LINE_LAYER = 'tracks-line'

/** 地图轨迹页（票 11）：MapLibre GL 渲染，底图可选 高德(国内) / CartoDB 矢量(海外)。 */
export default function MapView() {
  const containerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<maplibregl.Map | null>(null)
  const [trackList, setTrackList] = useState<Track[]>([])
  const [error, setError] = useState<string | null>(null)
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')
  const [provider, setProvider] = useState<BaseMapProvider>(() => {
    const saved = localStorage.getItem(PROVIDER_KEY)
    return saved === 'carto' ? 'carto' : 'amap'
  })

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

  // 建图（provider 变化时重建：两种底图 style 结构不同，重建最可靠）
  useEffect(() => {
    if (!containerRef.current) return
    const cfg = baseMaps[provider]
    localStorage.setItem(PROVIDER_KEY, provider)
    setStatus('loading')

    const map = new maplibregl.Map({
      container: containerRef.current,
      style: cfg.style as maplibregl.StyleSpecification,
      center: [121.47, 31.23],
      zoom: 11,
      attributionControl: { compact: true },
    })
    mapRef.current = map

    map.on('load', () => setStatus('ready'))
    map.on('error', (e: { error?: { message?: string } }) => {
      // 底图源不可达（如海外网络访问高德、国内网络访问 CartoDB）
      console.warn('map error', e?.error?.message ?? e)
      setStatus((s) => (s === 'ready' ? s : 'error'))
    })

    const popup = new maplibregl.Popup({ closeButton: false, offset: 8 })

    map.on('mouseenter', LINE_LAYER, () => {
      map.getCanvas().style.cursor = 'pointer'
    })
    map.on('mouseleave', LINE_LAYER, () => {
      map.getCanvas().style.cursor = ''
    })
    map.on('click', LINE_LAYER, (e) => {
      const f = e.features?.[0]
      if (!f) return
      const p = f.properties as { id: string; label: string; date: string; color: string }
      popup
        .setLngLat(e.lngLat)
        .setHTML(
          `<a href="/activities/${p.id}" style="color:${p.color}">${p.label}</a><br/>` +
            `<span style="font-size:11px;opacity:.7">${p.date}</span>`,
        )
        .addTo(map)
    })

    return () => {
      popup.remove()
      map.remove()
      mapRef.current = null
    }
  }, [provider])

  // 轨迹数据 → GeoJSON（按底图决定是否 WGS-84 → GCJ-02）
  useEffect(() => {
    const map = mapRef.current
    if (!map) return
    const cfg = baseMaps[provider]

    type LineFeature = {
      type: 'Feature'
      properties: { id: string; color: string; label: string; date: string }
      geometry: { type: 'LineString'; coordinates: [number, number][] }
    }
    const features: LineFeature[] = []
    const bounds = new maplibregl.LngLatBounds()
    let hasBounds = false

    for (const t of trackList) {
      const coords = t.points
        .map(([lat, lon]) => {
          const [la, lo] = cfg.needsGcj ? wgs84ToGcj02(lat, lon) : [lat, lon]
          return [lo, la] as [number, number]
        })
        .filter(([lo, la]) => Number.isFinite(lo) && Number.isFinite(la))
      if (coords.length < 2) continue
      for (const c of coords) {
        bounds.extend(c as maplibregl.LngLatLike)
        hasBounds = true
      }
      const color = sportColors[t.sport] ?? sportColors.other
      features.push({
        type: 'Feature',
        properties: {
          id: String(t.id),
          color,
          label: `${sportNames[t.sport] ?? t.sport} · ${(t.distanceM / 1000).toFixed(2)} km`,
          date: new Date(t.startTime * 1000).toLocaleDateString('zh-CN'),
        },
        geometry: { type: 'LineString' as const, coordinates: coords },
      })
    }

    const data = { type: 'FeatureCollection' as const, features }

    const apply = () => {
      const src = map.getSource(SOURCE_ID) as maplibregl.GeoJSONSource | undefined
      if (src) {
        src.setData(data)
      } else {
        map.addSource(SOURCE_ID, { type: 'geojson', data })
        map.addLayer({
          id: LINE_LAYER,
          type: 'line',
          source: SOURCE_ID,
          layout: { 'line-join': 'round', 'line-cap': 'round' },
          paint: {
            'line-color': ['get', 'color'],
            'line-width': 2,
            'line-opacity': 0.8,
          },
        })
      }
      if (hasBounds) {
        map.fitBounds(bounds, { padding: 40, duration: 0 })
      }
    }

    // 地图 style 未就绪时等 load（首次挂载 vs provider 切换）
    if (map.isStyleLoaded()) {
      apply()
    } else {
      map.once('load', apply)
    }
  }, [trackList, provider, status])

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-semibold">轨迹地图</h1>
        <span className="text-sm text-muted-foreground">{trackList.length} 条有 GPS 的活动</span>
        <div className="ml-auto flex items-center gap-2 text-sm">
          <label className="text-muted-foreground" htmlFor="provider">
            底图
          </label>
          <select
            id="provider"
            value={provider}
            onChange={(e) => setProvider(e.target.value as BaseMapProvider)}
            className="rounded border border-border bg-muted px-2 py-1"
          >
            <option value="amap">{baseMaps.amap.label}</option>
            <option value="carto">{baseMaps.carto.label}</option>
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

      {status === 'error' && (
        <p role="status" className="mb-2 rounded border border-accent bg-card p-2 text-xs text-accent">
          底图加载失败 —— 当前底图源在此网络下不可达（国内选「高德」，海外选「CartoDB」）。轨迹线仍会显示。
        </p>
      )}

      <div
        ref={containerRef}
        className="h-[32rem] w-full overflow-hidden rounded-lg border border-border"
        role="img"
        aria-label="活动轨迹地图"
      />

      <p className="mt-2 text-xs text-muted-foreground">
        颜色：蓝=跑步 · 琥珀=骑行 · 绿=游泳 · 灰=其他。点击轨迹进入详情。
        {provider === 'amap'
          ? '高德底图为 GCJ-02，轨迹已自动做 WGS-84 → GCJ-02 转换。'
          : 'CartoDB 矢量为 WGS-84，轨迹按原始 GPS 绘制。'}
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
