import { useEffect, useState } from 'react'

/**
 * 图表与导出卡的取色入口 —— **唯一来源是 `index.css` 的 `@theme` 变量**。
 *
 * 为什么要单独一层：ECharts、MapLibre、导出分享卡这些都要在 JS 里拿颜色。
 * 旧实现是在各组件里硬抄一份 hex，结果是「换了 token 图表不变」，
 * 而且亮色主题下图表仍然是深色配色（tooltip 白底黑字、坐标轴灰字飘在白底上）。
 * 这里统一读 CSS 变量，并在 `data-theme` 变化时重算，token 只有一份。
 *
 * 注意：导出卡走 `Blob → <img> → canvas` 光栅化，那是**独立 SVG 文档**，
 * 里面的 `var(--x)` 不会解析 —— 所以必须在生成 SVG 字符串之前就把变量解析成具体色值。
 */

/** 读一个 CSS 自定义属性的当前值（已解析，非 `var(...)` 字面量）。 */
export function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

export interface Palette {
  /** 墨色层 */
  bg: string
  card: string
  muted: string
  hair: string
  hair2: string
  text: string
  text2: string
  text3: string
  /** 交互与信号 */
  brand: string
  brandTx: string
  accent: string
  /** 数据语义色 */
  ctl: string
  atl: string
  tsb: string
  load: string
  bike: string
  good: string
  warn: string
  bad: string
}

/** 按当前主题解析一遍全部色值。SSR / 无 DOM 环境返回空串，调用方自行兜底。 */
export function readPalette(): Palette {
  const v = (n: string) => (typeof document === 'undefined' ? '' : cssVar(`--color-${n}`))
  return {
    bg: v('background'),
    card: v('card'),
    muted: v('muted'),
    hair: v('border'),
    hair2: v('hair-2'),
    text: v('foreground'),
    text2: v('muted-foreground'),
    text3: v('faint'),
    brand: v('brand'),
    brandTx: v('brand-tx'),
    accent: v('accent'),
    ctl: v('ctl'),
    atl: v('atl'),
    tsb: v('tsb'),
    load: v('load'),
    bike: v('bike'),
    good: v('good'),
    warn: v('warn'),
    bad: v('bad'),
  }
}

/**
 * React hook：返回当前主题的色板，主题切换时自动重渲染。
 *
 * 用 MutationObserver 盯 `data-theme` 而不是让 ThemeToggle 广播事件 ——
 * 取色方与切换方保持解耦，新增主题入口也不用改这里。
 */
export function usePalette(): Palette {
  const [p, setP] = useState<Palette>(readPalette)
  useEffect(() => {
    const obs = new MutationObserver(() => setP(readPalette()))
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => obs.disconnect()
  }, [])
  return p
}

function hexRgb(hex: string): [number, number, number] {
  let h = hex.replace('#', '')
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  const n = parseInt(h || '000000', 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

/** 两个 hex 线性混合，t=0 返回 a。用于热力图/区间条的分档色。 */
export function mix(a: string, b: string, t: number): string {
  const [r1, g1, b1] = hexRgb(a)
  const [r2, g2, b2] = hexRgb(b)
  const k = Math.max(0, Math.min(1, t))
  const to = (x: number, y: number) => Math.round(x + (y - x) * k).toString(16).padStart(2, '0')
  return `#${to(r1, r2)}${to(g1, g2)}${to(b1, b2)}`
}

/** hex → rgba(...)，给 ECharts 的 areaStyle / markArea 用。 */
export function alpha(hex: string, a: number): string {
  const [r, g, b] = hexRgb(hex)
  return `rgba(${r},${g},${b},${a})`
}

/**
 * 从 base 到 top 的 n 档分色（第 0 档通常是「无数据」的底色）。
 * 例：`ramp(p.muted, p.accent, 5)` → 热力图五级。
 */
export function ramp(base: string, top: string, n: number): string[] {
  return Array.from({ length: n }, (_, i) => (i === 0 ? base : mix(base, top, i / (n - 1))))
}
