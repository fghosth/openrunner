/**
 * 底图与坐标工具。
 *
 * 两个底图源：
 *  - amap（国内，默认）：高德 raster 瓦片，实测国内直连 0.07s；需要 GCJ-02 坐标
 *  - carto（海外）：CartoDB 矢量瓦片（running_page 无 token 时的同类方案），WGS-84 原样
 *
 * 渲染器：MapLibre GL —— 既能加载 raster 瓦片（高德），也能加载矢量 style（CartoDB），
 * 一套渲染器覆盖两种底图，避免 Leaflet + Mapbox GL 双引擎。
 */

const PI = Math.PI
const A = 6378245.0
const EE = 0.00669342162296594323

function outOfChina(lat: number, lon: number): boolean {
  return lon < 72.004 || lon > 137.8347 || lat < 0.8293 || lat > 55.8271
}

function transformLat(x: number, y: number): number {
  let ret = -100.0 + 2.0 * x + 3.0 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x))
  ret += ((20.0 * Math.sin(6.0 * x * PI) + 20.0 * Math.sin(2.0 * x * PI)) * 2.0) / 3.0
  ret += ((20.0 * Math.sin(y * PI) + 40.0 * Math.sin((y / 3.0) * PI)) * 2.0) / 3.0
  ret += ((160.0 * Math.sin((y / 12.0) * PI) + 320 * Math.sin((y * PI) / 30.0)) * 2.0) / 3.0
  return ret
}

function transformLon(x: number, y: number): number {
  let ret = 300.0 + x + 2.0 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x))
  ret += ((20.0 * Math.sin(6.0 * x * PI) + 20.0 * Math.sin(2.0 * x * PI)) * 2.0) / 3.0
  ret += ((20.0 * Math.sin(x * PI) + 40.0 * Math.sin((x / 3.0) * PI)) * 2.0) / 3.0
  ret += ((150.0 * Math.sin((x / 12.0) * PI) + 300.0 * Math.sin((x / 30.0) * PI)) * 2.0) / 3.0
  return ret
}

/** WGS-84（GPS）→ GCJ-02（高德/腾讯底图）。境外原样返回。 */
export function wgs84ToGcj02(lat: number, lon: number): [number, number] {
  if (outOfChina(lat, lon)) return [lat, lon]
  let dLat = transformLat(lon - 105.0, lat - 35.0)
  let dLon = transformLon(lon - 105.0, lat - 35.0)
  const radLat = (lat / 180.0) * PI
  let magic = Math.sin(radLat)
  magic = 1 - EE * magic * magic
  const sqrtMagic = Math.sqrt(magic)
  dLat = (dLat * 180.0) / (((A * (1 - EE)) / (magic * sqrtMagic)) * PI)
  dLon = (dLon * 180.0) / ((A / sqrtMagic) * Math.cos(radLat) * PI)
  return [lat + dLat, lon + dLon]
}

export type BaseMapProvider = 'amap' | 'carto'

/** 高德 raster 的 MapLibre style（自定义 raster source）。 */
export const amapStyle = {
  version: 8 as const,
  sources: {
    amap: {
      type: 'raster' as const,
      tiles: [
        'https://webrd01.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd02.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd03.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
        'https://webrd04.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
      ],
      tileSize: 256,
      attribution: '© 高德地图',
    },
  },
  layers: [{ id: 'amap-layer', type: 'raster' as const, source: 'amap' }],
}

/**
 * CartoDB 矢量样式（running_page 无 Mapbox token 时的同类方案）。
 * 免 token、无需注册；WGS-84 坐标系，轨迹不做偏移。
 */
export const cartoStyleURL = 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json'

export type BaseMapConfig = {
  /** MapLibre style：对象（高德自定义）或 URL（CartoDB 矢量） */
  style: string | object
  /** 轨迹是否需要 WGS-84 → GCJ-02 转换 */
  needsGcj: boolean
  label: string
}

export const baseMaps: Record<BaseMapProvider, BaseMapConfig> = {
  amap: { style: amapStyle, needsGcj: true, label: '高德（国内推荐）' },
  carto: { style: cartoStyleURL, needsGcj: false, label: 'CartoDB 矢量（海外）' },
}
