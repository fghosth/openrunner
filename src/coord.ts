/**
 * 底图与坐标工具。
 *
 * 底图：高德 raster（国内直连、免 token），需要 GCJ-02 坐标。
 *
 * 为什么只保留高德：曾尝试 CartoDB（矢量 + raster）作海外方案，但矢量瓦片的
 * Web Worker 解码在 Vite 生产构建下静默失败（瓦片 0 请求、无报错），而
 * cartocdn 在国内网络下不稳定（时通时不通）。个人跑步仪表盘的定位就是国内自托管，
 * 单底图更简单可靠 —— 少一个随时可能挂掉的选项。
 */

const PI = Math.PI
const A = 6378245.0
// 与 0.00669342162296594323 是同一个 double（最短往返表示），原字面量超出精度被静默舍入
const EE = 0.006693421622965943

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

export type BaseMapProvider = 'amap'

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

export type BaseMapConfig = {
  /** MapLibre style：自定义 raster style 对象 */
  style: string | object
  /** 轨迹是否需要 WGS-84 → GCJ-02 转换 */
  needsGcj: boolean
  label: string
}

export const baseMaps: Record<BaseMapProvider, BaseMapConfig> = {
  amap: { style: amapStyle, needsGcj: true, label: '高德地图' },
}
