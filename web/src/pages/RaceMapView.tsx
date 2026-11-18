import { useMemo, useState } from 'react'
import {
  featureBBox,
  featureToPath,
  labelPoint,
  LEVEL_BG,
  LEVEL_COLORS,
  levelOf,
  shortName,
  VIEW_H,
  VIEW_W,
  type GeoFeature,
} from '../china-geo'
import { useLocale } from '../i18n'

/** 省级分级图：按场次着色 + 省名标签 + 悬停读数 + 图例。
 *
 * 从 RaceCalendar 抽出来是因为页面原本同时管着取数、筛选、标签避让、右栏下钻四件事
 * （Divergent Change）—— 地图的几何与避让逻辑跟那些没有耦合，分开才各自只为一件事改。
 *
 * 分级图没有标签就读不出来（官方制图指南也是这条），所以省名 + 场次直接画在图上，
 * 并按「场次多的先落位、撞了就不画」避让；没标签的省靠悬停读数与右侧榜单兜底。
 *
 * 地图只到省级是数据决定的：上游给的是行政区**名称**而不是经纬度，仓库里也只有省级
 * 边界，所以市/区用列表下钻表达，不假装有矢量边界。
 */

/** 省名标签字号（SVG 用户坐标；viewBox 宽 760，渲染约 900px 时约合 18px 屏幕字）。 */
const LABEL_FS = 15

type Label = { name: string; x: number; y: number; count: number; fs: number }
/** 悬停读数只要省名与场次，不需要落点 */
type Readout = { name: string; count: number }
type Box = { x0: number; y0: number; x1: number; y1: number }

function overlaps(a: Box, b: Box): boolean {
  return !(a.x1 < b.x0 || a.x0 > b.x1 || a.y1 < b.y0 || a.y0 > b.y1)
}

export default function RaceMapView({
  features,
  counts,
  thresholds,
  maxCount,
  province,
  onPickProvince,
}: {
  features: GeoFeature[]
  counts: Map<string, number>
  thresholds: number[]
  maxCount: number
  province: string | null
  onPickProvince: (name: string) => void
}) {
  const { t } = useLocale()
  const [hover, setHover] = useState<Readout | null>(null)

  const paths = useMemo(
    () => features.map((f) => ({ name: f.properties.name, d: featureToPath(f) })),
    [features],
  )

  // 省名标签：场次多的先落位，和已放下的标签撞了就跳过。
  // 东部沿海省份挤在一起，全画会糊成一团，所以是「尽量画」而不是「必须画全」。
  const labels = useMemo(() => {
    const cands = features
      .map((f) => ({ f, count: counts.get(f.properties.name) ?? 0 }))
      .filter((c) => c.count > 0)
      .sort((a, b) => b.count - a.count)
    const placed: Box[] = []
    const out: Label[] = []
    for (const c of cands) {
      const pt = labelPoint(c.f)
      if (!pt) continue
      // 字号跟着省的宽度走：东部那些小省份用统一字号会溢出省界压到邻省
      const bb = featureBBox(c.f)
      const fs = bb ? Math.max(10, Math.min(LABEL_FS, bb.w / 5)) : LABEL_FS
      const label = shortName(c.f.properties.name)
      const w = (label.length + String(c.count).length * 0.62 + 1) * fs
      const h = fs * 1.3
      const box: Box = { x0: pt[0] - w / 2, y0: pt[1] - h / 2, x1: pt[0] + w / 2, y1: pt[1] + h / 2 }
      if (placed.some((p) => overlaps(box, p))) continue
      placed.push(box)
      out.push({ name: c.f.properties.name, x: pt[0], y: pt[1], count: c.count, fs })
    }
    return out
  }, [features, counts])

  // 悬停/选中的省一定要有读数，哪怕它的标签被避让掉了
  const readout: Readout | null =
    hover ?? (province ? { name: province, count: counts.get(province) ?? 0 } : null)

  return (
    <div className="relative rounded-lg border border-border bg-muted/30 p-2">
      <svg
        viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
        className="h-auto w-full"
        role="img"
        aria-label={t('racesMapAria')}
      >
        {paths.map((p) => {
          const n = counts.get(p.name) ?? 0
          const lvl = levelOf(n, thresholds)
          const isSel = province === p.name
          return (
            <path
              key={p.name}
              d={p.d}
              onClick={() => onPickProvince(p.name)}
              onMouseEnter={() => setHover({ name: p.name, count: n })}
              onMouseLeave={() => setHover(null)}
              className={`${LEVEL_COLORS[lvl]} cursor-pointer stroke-border transition-colors ${
                isSel ? 'stroke-secondary stroke-2' : 'stroke-[0.5]'
              } ${province && !isSel ? 'opacity-40' : ''}`}
            >
              <title>
                {p.name}
                {n > 0 ? ` · ${n}` : ''}
              </title>
            </path>
          )
        })}
        {labels.map((l) => (
          <text
            key={l.name}
            x={l.x}
            y={l.y}
            textAnchor="middle"
            dominantBaseline="middle"
            fontSize={l.fs}
            // 描边光晕（paint-order: stroke）：深色省份上纯前景色文字读不出来，
            // 勾一圈背景色就两头都成立 —— 浅色底深字、深色底浅字都清楚。
            stroke="var(--color-background)"
            strokeWidth={3.5}
            strokeLinejoin="round"
            style={{ paintOrder: 'stroke' }}
            className="pointer-events-none fill-foreground select-none"
          >
            {shortName(l.name)}
            <tspan className="fill-secondary" fontWeight={600}>
              {' '}
              {l.count}
            </tspan>
          </text>
        ))}
      </svg>

      {/* 悬停读数：被避让掉标签的省靠它兜底 */}
      <div className="pointer-events-none absolute left-4 top-4 rounded-md border border-border bg-card/90 px-3 py-1.5 text-sm shadow-sm">
        {readout ? (
          <>
            <span className="text-foreground">{readout.name}</span>
            <span className="ml-2 font-mono text-secondary">{readout.count}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{t('racesHoverHint')}</span>
        )}
      </div>

      <div className="mt-1 flex flex-wrap items-center gap-1.5 px-2 pb-1 text-xs text-muted-foreground">
        <span className="mr-1">{t('less')}</span>
        {LEVEL_BG.map((c, i) => (
          <span key={i} className={`h-3.5 w-3.5 rounded-sm border border-border ${c}`} />
        ))}
        <span className="ml-1">{t('more')}</span>
        {maxCount > 0 && (
          // 档位是跟着当前筛选算的，所以把分界值写出来，深浅才有意义
          <span className="ml-2 font-mono">
            {thresholds.map((x) => `<${x}`).join(' ')} ≥{thresholds[2]}
          </span>
        )}
      </div>
    </div>
  )
}
