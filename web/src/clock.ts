/**
 * 页面加载时刻的统一时间基准。
 *
 * 各处渲染期直接 new Date() 会被 react(purity) 判为不纯调用——并发渲染下
 * 两次 render 拿到的「今天」可能不一致（撕裂）。模块级常量在页面加载时计算
 * 一次、渲染期只读，从根上消除该问题。
 *
 * 代价：页面跨午夜/跨年挂着不刷新时基准会滞后一天——个人仪表盘可接受，
 * 刷新即恢复。真需要实时滚动的场景请自行 new Date() 并处理撕裂。
 */
export const NOW = new Date()

export const CURRENT_YEAR = NOW.getFullYear()

/** 本地时区的 YYYY-MM-DD（与后端 timeToDateString 同口径，非 UTC） */
export const TODAY_STR = `${CURRENT_YEAR}-${String(NOW.getMonth() + 1).padStart(2, '0')}-${String(
  NOW.getDate(),
).padStart(2, '0')}`
