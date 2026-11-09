import { useEffect, useMemo, useState } from 'react'
import type { TrackCenter } from '../api'
import { formatDistance, useUnit } from '../units'
import { useLocale } from '../i18n'

/** 中国省级行政区活动热力图（对齐 running_page 的 ChinaMap）。
 *
 * 实现要点：
 *  - 省份边界用一份静态 GeoJSON（35 个省级要素），按动态 import 拆成独立 chunk，
 *    不拖慢首页首屏。
 *  - equirectangular 投影直接铺到 SVG（经度 73~136E、纬度 15~54N），
 *    跑者数据都在国内，不需要墨卡托。
 *  - 活动本身没有「省份」字段，用轨迹代表点做 point-in-polygon 归属。
 */

type Ring = number[][] // [[lng,lat], ...]
type GeoFeature = {
  type: 'Feature'
  properties: { name: string; adcode: number }
  geometry: { type: 'Polygon' | 'MultiPolygon'; coordinates: unknown }
}

const VIEW_W = 760
const VIEW_H = 620
// 留一点边距，避免新疆/海南贴边被裁
const BOUNDS = { minLng: 72, maxLng: 137, minLat: 14, maxLat: 55 }

function project(lng: number, lat: number): [number, number] {
  const x = ((lng - BOUNDS.minLng) / (BOUNDS.maxLng - BOUNDS.minLng)) * VIEW_W
  const y = VIEW_H - ((lat - BOUNDS.minLat) / (BOUNDS.maxLat - BOUNDS.minLat)) * VIEW_H
  return [x, y]
}

function ringToPath(ring: Ring): string {
  let d = ''
  for (let i = 0; i < ring.length; i++) {
    const [lng, lat] = ring[i]
    const [x, y] = project(lng, lat)
    d += (i === 0 ? 'M' : 'L') + x.toFixed(1) + ',' + y.toFixed(1)
  }
  return d + 'Z'
}

function featureToPath(f: GeoFeature): string {
  const polys =
    f.geometry.type === 'Polygon'
      ? [f.geometry.coordinates as unknown as Ring[]]
      : (f.geometry.coordinates as unknown as Ring[][])
  return polys.map((poly) => poly.map(ringToPath).join('')).join('')
}

/** 射线法判断点是否在环内。 */
function pointInRing(lng: number, lat: number, ring: Ring): boolean {
  let inside = false
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const [xi, yi] = ring[i]
    const [xj, yj] = ring[j]
    if (yi > lat !== yj > lat && lng < ((xj - xi) * (lat - yi)) / (yj - yi) + xi) {
      inside = !inside
    }
  }
  return inside
}

/** 多边形（外环 + 洞）判定。 */
function pointInPolygon(lng: number, lat: number, poly: Ring[]): boolean {
  if (poly.length === 0) return false
  if (!pointInRing(lng, lat, poly[0])) return false
  // 内环（洞）命中则视为不在该省
  for (let i = 1; i < poly.length; i++) {
    if (pointInRing(lng, lat, poly[i])) return false
  }
  return true
}

/** 判断坐标落在哪个省。先按外接矩形粗筛，再做精确判定。 */
function provinceOf(lng: number, lat: number, features: GeoFeature[]): string | null {
  for (const f of features) {
    const polys =
      f.geometry.type === 'Polygon'
        ? [f.geometry.coordinates as unknown as Ring[]]
        : (f.geometry.coordinates as unknown as Ring[][])
    for (const poly of polys) {
      if (!poly.length) continue
      // 外接矩形粗筛（省界点数多，先排除绝大部分）
      let minLng = Infinity
      let maxLng = -Infinity
      let minLat = Infinity
      let maxLat = -Infinity
      for (const [x, y] of poly[0]) {
        if (x < minLng) minLng = x
        if (x > maxLng) maxLng = x
        if (y < minLat) minLat = y
        if (y > maxLat) maxLat = y
      }
      if (lng < minLng || lng > maxLng || lat < minLat || lat > maxLat) continue
      if (pointInPolygon(lng, lat, poly)) return f.properties.name
    }
  }
  return null
}

/** 省份简称（地图上标签用，避免「内蒙古自治区」这种长名挤在一起）。 */
function shortName(name: string): string {
  return name
    .replace(/维吾尔自治区|回族自治区|壮族自治区|自治区|特别行政区|省|市/g, '')
    .slice(0, 4)
}

const LEVEL_COLORS = [
  'fill-muted/40', // 0 次
  'fill-secondary/25',
  'fill-secondary/45',
  'fill-secondary/70',
  'fill-secondary',
]

function levelOf(count: number): number {
  if (count <= 0) return 0
  if (count < 5) return 1
  if (count < 20) return 2
  if (count < 100) return 3
  return 4
}

export default function ChinaMap({ centers }: { centers: TrackCenter[] | null }) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const [features, setFeatures] = useState<GeoFeature[] | null>(null)
  const [selected, setSelected] = useState<string | null>(null)

  // 省份边界较大（约 560KB），动态加载成独立 chunk
  useEffect(() => {
    let cancelled = false
    import('../assets/china-provinces.json')
      .then((mod) => {
        if (cancelled) return
        const data = (mod.default ?? mod) as unknown as { features: GeoFeature[] }
        setFeatures(data.features ?? [])
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  // 把活动代表点归属到省份
  const byProvince = useMemo(() => {
    const m = new Map<string, { count: number; distanceM: number }>()
    if (!features || !centers) return m
    for (const c of centers) {
      const name = provinceOf(c.lng, c.lat, features)
      if (!name) continue
      const cur = m.get(name) ?? { count: 0, distanceM: 0 }
      cur.count++
      cur.distanceM += c.distanceM
      m.set(name, cur)
    }
    return m
  }, [features, centers])

  const paths = useMemo(() => {
    if (!features) return []
    return features.map((f) => ({
      name: f.properties.name,
      d: featureToPath(f),
    }))
  }, [features])

  const ranked = useMemo(
    () => [...byProvince.entries()].sort((a, b) => b[1].count - a[1].count),
    [byProvince],
  )

  const sel = selected ? byProvince.get(selected) : null

  return (
    <section aria-label="省份热力地图" className="rounded-lg border border-border bg-card p-4">
      <div className="mb-2 flex flex-wrap items-baseline gap-2">
        <h2 className="text-sm font-medium text-muted-foreground">{t('footprintMap')}</h2>
        <span className="text-xs text-muted-foreground">
          {byProvince.size} {t('provincesCovered')}
        </span>
        {selected && (
          <button
            onClick={() => setSelected(null)}
            className="ml-auto cursor-pointer text-xs text-secondary hover:underline"
          >
            {t('clearSelection')}
          </button>
        )}
      </div>

      {!features && <p className="text-xs text-muted-foreground">地图数据加载中…</p>}

      {features && (
        <div className="flex flex-wrap items-start gap-3">
          <svg
            viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
            className="h-auto w-full max-w-[420px] shrink-0"
            role="img"
            aria-label="中国省份活动热力图"
          >
            {paths.map((p) => {
              const stat = byProvince.get(p.name)
              const lvl = levelOf(stat?.count ?? 0)
              const isSel = selected === p.name
              return (
                <path
                  key={p.name}
                  d={p.d}
                  className={`${LEVEL_COLORS[lvl]} cursor-pointer stroke-border transition-colors ${
                    isSel ? 'stroke-secondary stroke-2' : 'stroke-[0.5]'
                  } ${selected && !isSel ? 'opacity-40' : ''}`}
                  onClick={() => setSelected(isSel ? null : p.name)}
                >
                  <title>
                    {p.name}
                    {stat ? ` · ${stat.count} · ${formatDistance(stat.distanceM, unit)}` : ` · ${t('noActivity')}`}
                  </title>
                </path>
              )
            })}
          </svg>

          <div className="min-w-[140px] flex-1 space-y-1">
            {selected && (
              <div className="mb-2 rounded border border-border bg-muted/50 p-2 text-xs">
                <div className="font-medium text-foreground">{selected}</div>
                {sel ? (
                  <div className="text-muted-foreground">
                    {sel.count} · {formatDistance(sel.distanceM, unit)}
                  </div>
                ) : (
                  <div className="text-muted-foreground">{t('noActivity')}</div>
                )}
              </div>
            )}
            {ranked.slice(0, 8).map(([name, stat]) => (
              <button
                key={name}
                onClick={() => setSelected(selected === name ? null : name)}
                className={`flex w-full cursor-pointer items-baseline gap-2 rounded px-1 py-0.5 text-left text-xs hover:bg-muted ${
                  selected === name ? 'bg-muted' : ''
                }`}
              >
                <span className="w-14 shrink-0 truncate text-muted-foreground">
                  {shortName(name)}
                </span>
                <span className="font-mono text-foreground">{stat.count}</span>
                <span className="ml-auto font-mono text-muted-foreground">
                  {formatDistance(stat.distanceM, unit, 0)}
                </span>
              </button>
            ))}
            {ranked.length === 0 && (
              <p className="text-xs text-muted-foreground">{t('noGpsYet')}</p>
            )}
          </div>
        </div>
      )}

      <div className="mt-2 flex items-center gap-1 text-[10px] text-muted-foreground">
        <span className="mr-1">{t('less')}</span>
        {LEVEL_COLORS.map((c, i) => (
          <span key={i} className={`h-3 w-3 rounded-sm border border-border ${c}`} />
        ))}
        <span className="ml-1">{t('more')}</span>
      </div>
    </section>
  )
}
