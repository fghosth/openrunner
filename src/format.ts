// 展示层共享格式化（Home/ActivityDetail 共用，消除重复）。
export const sportNames: Record<string, string> = {
  run: '跑步',
  ride: '骑行',
  swim: '游泳',
  other: '其他',
}

export function fmtDuration(secs: number): string {
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  const s = secs % 60
  return h > 0 ? `${h}h${String(m).padStart(2, '0')}m` : `${m}m${String(s).padStart(2, '0')}s`
}

export function fmtDateTime(unixSecs: number): string {
  return new Date(unixSecs * 1000).toLocaleString('zh-CN', { hour12: false })
}
