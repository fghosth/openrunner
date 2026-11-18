/** 中国省级地图的共用几何与配色。
 *
 * 从 `ChinaMap.tsx` 抽出来，供「活动足迹」与「赛事地图」两页共用 ——
 * 同一份边界、同一个投影，两页的颜色才有可比性。
 *
 * 投影是 equirectangular 直接铺到 SVG（经度 73~136E、纬度 15~54N）：
 * 数据都在国内，不需要墨卡托，省一层变换也少一类出错来源。
 */

export type Ring = number[][] // [[lng,lat], ...]

export type GeoFeature = {
  type: 'Feature'
  properties: { name: string; adcode: number }
  geometry: { type: 'Polygon' | 'MultiPolygon'; coordinates: unknown }
}

export const VIEW_W = 760
export const VIEW_H = 620
// 留一点边距，避免新疆/海南贴边被裁
export const BOUNDS = { minLng: 72, maxLng: 137, minLat: 14, maxLat: 55 }

export function project(lng: number, lat: number): [number, number] {
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

export function featureToPath(f: GeoFeature): string {
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
export function provinceOf(lng: number, lat: number, features: GeoFeature[]): string | null {
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
export function shortName(name: string): string {
  return name
    .replace(/维吾尔自治区|回族自治区|壮族自治区|自治区|特别行政区|省|市/g, '')
    .slice(0, 4)
}

/** 分级着色（5 档）。两页共用色阶，深浅才代表同一件事：值越大越深。 */
/** 图例色块用的背景版：`fill-*` 只对 SVG 生效，HTML 小方块得用 `bg-*`。 */
export const LEVEL_BG = [
  'bg-muted/40',
  'bg-secondary/25',
  'bg-secondary/45',
  'bg-secondary/70',
  'bg-secondary',
]

export const LEVEL_COLORS = [
  'fill-muted/40', // 0
  'fill-secondary/25',
  'fill-secondary/45',
  'fill-secondary/70',
  'fill-secondary',
]

/** 按阈值表把值映射到 0~4 档。阈值各页自己给 —— 场次和公里数不是一个量级。 */
export function levelOf(value: number, thresholds: number[]): number {
  if (value <= 0) return 0
  for (let i = 0; i < thresholds.length; i++) {
    if (value < thresholds[i]) return i + 1
  }
  return LEVEL_COLORS.length - 1
}

/** 把要素拆成「多边形数组」（每个多边形 = 外环 + 若干洞）。 */
function polygonsOf(f: GeoFeature): Ring[][] {
  return f.geometry.type === 'Polygon'
    ? [f.geometry.coordinates as unknown as Ring[]]
    : (f.geometry.coordinates as unknown as Ring[][])
}

function ringBBox(ring: Ring) {
  let x0 = Infinity
  let y0 = Infinity
  let x1 = -Infinity
  let y1 = -Infinity
  for (const [x, y] of ring) {
    if (x < x0) x0 = x
    if (x > x1) x1 = x
    if (y < y0) y0 = y
    if (y > y1) y1 = y
  }
  return { x0, y0, x1, y1 }
}

/** 标签落点：最大那块陆地的包围盒中心，再在附近找一个**真正落在省界内**的点。
 *
 * 直接用中心点会翻车 —— 甘肃是弯月形、内蒙古是长条，几何中心落在省界外面，
 * 标签就会飘到邻省去。所以中心不命中时按半径由小到大向外搜。
 */
export function labelPoint(f: GeoFeature): [number, number] | null {
  const polys = polygonsOf(f)
  let best: Ring[] | null = null
  let bestArea = -1
  for (const poly of polys) {
    if (!poly.length) continue
    const b = ringBBox(poly[0])
    const area = (b.x1 - b.x0) * (b.y1 - b.y0)
    if (area > bestArea) {
      bestArea = area
      best = poly
    }
  }
  if (!best) return null
  const b = ringBBox(best[0])
  const cx = (b.x0 + b.x1) / 2
  const cy = (b.y0 + b.y1) / 2
  if (pointInPolygon(cx, cy, best)) return project(cx, cy)

  const rx = (b.x1 - b.x0) / 2
  const ry = (b.y1 - b.y0) / 2
  let found: [number, number] | null = null
  let bestDist = Infinity
  for (let i = 1; i <= 12; i++) {
    for (let j = 0; j < 8; j++) {
      const a = (j / 8) * Math.PI * 2
      const px = cx + Math.cos(a) * rx * (i / 12)
      const py = cy + Math.sin(a) * ry * (i / 12)
      if (!pointInPolygon(px, py, best)) continue
      const d = (px - cx) ** 2 + (py - cy) ** 2
      if (d < bestDist) {
        bestDist = d
        found = project(px, py)
      }
    }
    if (found) return found // 同一圈里取最近的，圈层由内向外
  }
  const first = best[0][0]
  return first ? project(first[0], first[1]) : null
}

/** 标签避让用的包围盒（SVG 用户坐标），用来估标签面积。 */
export function featureBBox(f: GeoFeature): { x: number; y: number; w: number; h: number } | null {
  const polys = polygonsOf(f)
  let x0 = Infinity
  let y0 = Infinity
  let x1 = -Infinity
  let y1 = -Infinity
  for (const poly of polys) {
    if (!poly.length) continue
    const b = ringBBox(poly[0])
    if (b.x0 < x0) x0 = b.x0
    if (b.y0 < y0) y0 = b.y0
    if (b.x1 > x1) x1 = b.x1
    if (b.y1 > y1) y1 = b.y1
  }
  if (x0 > x1 || y0 > y1) return null
  const [ax, ay] = project(x0, y1)
  const [bx, by] = project(x1, y0)
  return { x: Math.min(ax, bx), y: Math.min(ay, by), w: Math.abs(bx - ax), h: Math.abs(by - ay) }
}

/** 省份边界约 560KB，动态加载成独立 chunk，别拖慢首屏。 */
export async function loadProvinces(): Promise<GeoFeature[]> {
  const mod = (await import('./assets/china-provinces.json')) as unknown as {
    default?: { features: GeoFeature[] }
    features?: GeoFeature[]
  }
  const data = (mod.default ?? mod) as unknown as { features: GeoFeature[] }
  return data.features ?? []
}
