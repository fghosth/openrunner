/** 分享卡片导出（对齐 running_page 的 exportCard）。
 *
 * 把「年度统计 + 训练热力图」渲染成一张 1080×1080 的 PNG 便于分享。
 * 直接构造 SVG 再经 canvas 转 PNG —— 不引入 html2canvas 之类的重依赖，
 * 也不受页面 DOM 样式影响（导出结果稳定可控）。
 */

export type CardInput = {
  title: string
  athleteName: string
  year: number
  distanceLabel: string // 已带单位，如 "2266 km"
  countLabel: string
  durationLabel: string
  elevationLabel: string
  /** date(YYYY-MM-DD) → 当日距离（米），用于画热力图 */
  daily: Map<string, number>
}

const SIZE = 1080
const PAD = 64
const HEAT_CELL = 14
const HEAT_GAP = 3

function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/** 热力图分档颜色（与页面一致，但这里是固定色值，避免依赖 CSS 变量）。 */
const HEAT = ['#1e293b', '#1e3a8a', '#1d4ed8', '#3b82f6', '#93c5fd']

function levelOf(m: number): number {
  if (m <= 0) return 0
  if (m < 5000) return 1
  if (m < 10000) return 2
  if (m < 20000) return 3
  return 4
}

function buildSvg(input: CardInput): string {
  const { title, athleteName, year, daily } = input

  // 热力图：53 列 × 7 行，从该年 1 月 1 日所在周的周一起铺
  const first = new Date(year, 0, 1)
  const start = new Date(first)
  start.setDate(start.getDate() - ((start.getDay() + 6) % 7))
  const cells: { x: number; y: number; lvl: number }[] = []
  const cursor = new Date(start)
  for (let col = 0; cursor.getFullYear() <= year; col++) {
    for (let row = 0; row < 7; row++) {
      if (cursor.getFullYear() === year) {
        const key = `${cursor.getFullYear()}-${String(cursor.getMonth() + 1).padStart(2, '0')}-${String(cursor.getDate()).padStart(2, '0')}`
        cells.push({
          x: PAD + col * (HEAT_CELL + HEAT_GAP),
          y: PAD + 250 + row * (HEAT_CELL + HEAT_GAP),
          lvl: levelOf(daily.get(key) ?? 0),
        })
      }
      cursor.setDate(cursor.getDate() + 1)
    }
  }

  const rects = cells
    .map(
      (c) =>
        `<rect x="${c.x}" y="${c.y}" width="${HEAT_CELL}" height="${HEAT_CELL}" rx="3" fill="${HEAT[c.lvl]}"/>`,
    )
    .join('')

  const stats: [string, string][] = [
    ['距离', input.distanceLabel],
    ['次数', input.countLabel],
    ['时间', input.durationLabel],
    ['爬升', input.elevationLabel],
  ]
  const statBlocks = stats
    .map(([label, value], i) => {
      const x = PAD + i * 245
      return `
        <text x="${x}" y="120" fill="#94a3b8" font-size="24" font-family="sans-serif">${esc(label)}</text>
        <text x="${x}" y="180" fill="#f8fafc" font-size="46" font-weight="700" font-family="monospace">${esc(value)}</text>`
    })
    .join('')

  return `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
    <rect width="${SIZE}" height="${SIZE}" fill="#0f172a"/>
    <text x="${PAD}" y="56" fill="#f8fafc" font-size="30" font-weight="700" font-family="sans-serif">${esc(title)}</text>
    <text x="${SIZE - PAD}" y="56" text-anchor="end" fill="#64748b" font-size="22" font-family="sans-serif">${esc(athleteName)} · ${year}</text>
    ${statBlocks}
    <text x="${PAD}" y="230" fill="#94a3b8" font-size="22" font-family="sans-serif">训练日历</text>
    ${rects}
    <text x="${PAD}" y="${SIZE - 40}" fill="#475569" font-size="20" font-family="monospace">openrunner</text>
  </svg>`
}

/** 生成并下载 PNG。返回 Promise 便于调用方显示 loading / 错误。 */
export async function exportCardPng(input: CardInput): Promise<void> {
  const svg = buildSvg(input)
  const blob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  try {
    const img = new Image()
    img.width = SIZE
    img.height = SIZE
    await new Promise<void>((resolve, reject) => {
      img.onload = () => resolve()
      img.onerror = () => reject(new Error('渲染卡片失败'))
      img.src = url
    })
    const canvas = document.createElement('canvas')
    canvas.width = SIZE
    canvas.height = SIZE
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('浏览器不支持 canvas')
    ctx.drawImage(img, 0, 0, SIZE, SIZE)
    const pngUrl = canvas.toDataURL('image/png')
    const a = document.createElement('a')
    a.href = pngUrl
    a.download = `openrunner-${input.year}.png`
    a.click()
  } finally {
    URL.revokeObjectURL(url)
  }
}
