import { useEffect, useMemo, useState } from 'react'
import type { TrackCenter } from '../api'
import {
  featureToPath,
  LEVEL_BG,
  LEVEL_COLORS,
  levelOf,
  loadProvinces,
  provinceOf,
  shortName,
  VIEW_H,
  VIEW_W,
  type GeoFeature,
} from '../china-geo'
import { formatDistance, useUnit } from '../units'
import { useLocale } from '../i18n'

/** 中国省级行政区活动热力图（对齐 running_page 的 ChinaMap）。
 *
 * 几何、投影、配色都在 `china-geo.ts`，与「赛事地图」共用一份 ——
 * 同一份边界、同一个投影，两页的深浅才有可比性。
 *
 * 活动本身没有「省份」字段，用轨迹代表点做 point-in-polygon 归属。
 */

/** 活动量的分档阈值。场次和公里数不在一个量级，所以阈值由各页自己给。 */
const ACTIVITY_THRESHOLDS = [5, 20, 100]

export default function ChinaMap({ centers }: { centers: TrackCenter[] | null }) {
  const { unit } = useUnit()
  const { t } = useLocale()
  const [features, setFeatures] = useState<GeoFeature[] | null>(null)
  const [selected, setSelected] = useState<string | null>(null)

  // 省份边界较大（约 560KB），动态加载成独立 chunk
  useEffect(() => {
    let cancelled = false
    loadProvinces()
      .then((f) => {
        if (!cancelled) setFeatures(f)
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
        // 地图吸收余量、列表固定一列；余量实在用不完时整组居中，
        // 免得右侧空出一大块看着像坏了
        <div className="flex flex-wrap items-start justify-center gap-3">
          <svg
            viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
            className="h-auto w-full min-w-[280px] max-w-[820px] flex-1"
            role="img"
            aria-label="中国省份活动热力图"
          >
            {paths.map((p) => {
              const stat = byProvince.get(p.name)
              const lvl = levelOf(stat?.count ?? 0, ACTIVITY_THRESHOLDS)
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

          {/* 列表固定成一列：省份只有个位数，铺成多列会被拉成一条横贯面板的横条 */}
          <div className="w-[300px] shrink-0 space-y-1">
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
                <span className="w-14 shrink-0 truncate text-muted-foreground">{shortName(name)}</span>
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
        {LEVEL_BG.map((c, i) => (
          <span key={i} className={`h-3 w-3 rounded-sm border border-border ${c}`} />
        ))}
        <span className="ml-1">{t('more')}</span>
      </div>
    </section>
  )
}
