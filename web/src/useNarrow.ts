import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * 观测某个元素的宽度是否低于阈值，供图表在窄屏换一套边距。
 *
 * 返回 `[narrow, attach]`，`attach` 是 **callback ref**：元素挂载与卸载都会被调用，
 * 所以 `{data ? <div ref={attach} /> : null}` 这种条件渲染也能挂上 ResizeObserver。
 *
 * 为什么不用 `useEffect(() => { ro.observe(ref.current) }, [])`：effect 跑的时候
 * 条件渲染的元素还不存在，`ref.current` 是 null，RO 静默地永远挂不上，
 * 表现是「改了窄屏逻辑但完全没生效」。 Trends.tsx 里踩过。
 *
 * 只关心「是否低于阈值」而不是具体像素：把布尔值放进 setOption 依赖，
 * 跨阈值才重渲染一次，比把宽度塞进依赖省掉大量无谓的 setOption。
 */
export function useNarrowWidth(
  threshold = 640,
  onResize?: (el: HTMLDivElement) => void,
): [boolean, (el: HTMLDivElement | null) => void] {
  const [narrow, setNarrow] = useState(false)
  const ro = useRef<ResizeObserver | null>(null)
  // 回调放 ref 里，避免调用方每次渲染换函数就重挂一次 RO
  const onResizeRef = useRef(onResize)
  // 在 effect 里同步而不是 render 期赋值：render 期写 ref 会被 oxlint 的
  // react(refs) 拦下（ref 的值不参与渲染，render 期写会让更新不如预期）
  useEffect(() => {
    onResizeRef.current = onResize
  }, [onResize])

  const attach = useCallback(
    (el: HTMLDivElement | null) => {
      ro.current?.disconnect()
      ro.current = null
      if (!el || typeof ResizeObserver === 'undefined') return
      const update = () => {
        setNarrow(el.getBoundingClientRect().width < threshold)
        onResizeRef.current?.(el)
      }
      update()
      const obs = new ResizeObserver(update)
      obs.observe(el)
      ro.current = obs
    },
    [threshold],
  )

  useEffect(() => () => ro.current?.disconnect(), [])

  return [narrow, attach]
}

/** 把 callback ref 与已有的对象 ref 合成一个稳定引用。
 * echarts 需要对象 ref 拿 DOM，窄屏观测需要 callback ref，两者不能各占一个 ref 属性。 */
export function joinRefs<T>(objectRef: { current: T | null }, cb: (el: T | null) => void) {
  return (el: T | null) => {
    objectRef.current = el
    cb(el)
  }
}
