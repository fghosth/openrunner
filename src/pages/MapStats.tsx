import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import * as L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { annualStats, downloadPoster, tracks, type AnnualStat, type Track } from '../api'
import { readActiveAthlete } from '../App'
import { sportNames } from '../format'

const sportColors: Record<string, string> = {
  run: '#3b82f6',
  ride: '#d97706',
  swim: '#22c55e',
  other: '#94a3b8',
}

/** 地图轨迹页（票 11）：全部活动轨迹（OSM 免 token 瓦片）+ 点击跳详情。 */
export default function MapView() {
  const mapRef = useRef<HTMLDivElement>(null)
  const mapObj = useRef<L.Map | null>(null)
  const layerGroup = useRef<L.LayerGroup | null>(null)
  const [trackList, setTrackList] = useState<Track[]>([])
  const [error, setError] = useState<string | null>(null)

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

  useEffect(() => {
    if (!mapRef.current || mapObj.current) return
    const map = L.map(mapRef.current, { worldCopyJump: true }).setView([31.23, 121.47], 12)
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; OpenStreetMap contributors',
    }).addTo(map)
    mapObj.current = map
    layerGroup.current = L.layerGroup().addTo(map)
    return () => {
      map.remove()
      mapObj.current = null
      layerGroup.current = null
    }
  }, [])

  useEffect(() => {
    if (!mapObj.current || !layerGroup.current) return
    layerGroup.current.clearLayers()
    const bounds: L.LatLngTuple[] = []
    for (const t of trackList) {
      const latlngs = t.points.map(([lat, lon]) => [lat, lon] as L.LatLngTuple)
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
  }, [trackList])

  return (
    <main className="mx-auto max-w-6xl px-6 py-10">
      <div className="mb-4 flex items-center gap-4">
        <h1 className="text-2xl font-semibold">轨迹地图</h1>
        <span className="text-sm text-muted-foreground">{trackList.length} 条有 GPS 的活动</span>
        <Link to="/stats" className="ml-auto text-sm text-secondary hover:underline">
          年度统计 →
        </Link>
      </div>

      {error && (
        <p role="alert" className="mb-4 rounded border border-destructive bg-card p-3 text-sm text-destructive">
          {error}
        </p>
      )}

      <div ref={mapRef} className="h-[32rem] w-full overflow-hidden rounded-lg border border-border" role="img" aria-label="活动轨迹地图" />

      <p className="mt-2 text-xs text-muted-foreground">
        瓦片：OpenStreetMap（免 token）。颜色：蓝=跑步 · 琥珀=骑行 · 绿=游泳 · 灰=其他。点击轨迹进入详情。
      </p>
    </main>
  )
}

/** 年度统计页（票 11）：总距离/次数/活跃天数/连续天数 + 月度柱图。 */
export function StatsView() {
  const [year, setYear] = useState(new Date().getFullYear())
  const [stat, setStat] = useState<(AnnualStat & { streaks: { longest: number; current: number } }) | null>(null)
  const [error, setError] = useState<string | null>(null)
  const chartRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const id = readActiveAthlete()
    if (!id) return
    annualStats(id, year)
      .then(setStat)
      .catch((err: Error) => setError(err.message))
  }, [year])

  const maxMonth = stat ? Math.max(1, ...stat.monthly.map((m) => m.distanceM)) : 1

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
              ['总距离', `${(stat.totalDistanceM / 1000).toFixed(1)} km`],
              ['活动次数', String(stat.activityCount)],
              ['活跃天数', String(stat.activeDays)],
              ['最长连续', `${stat.streaks.longest} 天`],
              ['当前连续', `${stat.streaks.current} 天`],
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
              {stat.monthly.map((m) => {
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
      <div ref={chartRef} className="hidden" />
    </main>
  )
}
