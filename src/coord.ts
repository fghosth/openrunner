/**
 * WGS-84 → GCJ-02 坐标转换。
 *
 * 为什么需要：GPS 设备（含 Garmin）记录的是 WGS-84 坐标，而国内地图服务
 * （高德/腾讯/百度）使用 GCJ-02（"火星坐标"，国家测绘局加密偏移）。
 * 直接把 WGS-84 点画在高德底图上会偏移 50–500 米，轨迹会"飘"到马路外。
 *
 * 算法来源：公开的 GCJ-02 变换公式（各家地图 SDK 通用实现），
 * 非本项目独有。仅在 outOfChina 之外应用偏移。
 */

const PI = Math.PI
const A = 6378245.0 // 克拉索夫斯基椭球长半轴
const EE = 0.00669342162296594323 // 偏心率平方

/** 中国境外不偏移（境外使用 WGS-84 原样）。 */
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

/** WGS-84（GPS）→ GCJ-02（高德/腾讯底图）。 */
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

/** 底图坐标系类型。 */
export type TileProvider = 'amap' | 'osm'

export const tileConfigs: Record<
  TileProvider,
  { url: string; attribution: string; needsGcj: boolean; subdomains?: string[]; maxZoom: number }
> = {
  // 高德：国内直连快（实测 0.07s），需 GCJ-02 坐标
  amap: {
    url: 'https://webrd0{s}.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
    subdomains: ['1', '2', '3', '4'],
    attribution: '© 高德地图',
    needsGcj: true,
    maxZoom: 18,
  },
  // OSM：海外可用，国内常超时；WGS-84 原样
  osm: {
    url: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
    attribution: '© OpenStreetMap contributors',
    needsGcj: false,
    maxZoom: 19,
  },
}
