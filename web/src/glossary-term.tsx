/**
 * 术语提示（glossary tooltip）。
 *
 * 用法：把含术语的文案交给 `<GT text={...} />`，它自动切出术语并挂上可弹出的简略说明。
 *
 * 为什么用 portal 而不是 CSS :hover：
 * - 卡片普遍带 overflow/圆角，就地渲染的 tooltip 会被裁掉；
 * - 全站只挂一个浮层，避免几十个 hover 规则互相打架。
 *
 * 交互约定（触屏/键盘/鼠标都要能用）：
 * - 鼠标 hover 显示、移开隐藏（`pointerType === 'mouse'` 才走 hover，否则触屏上会粘住）
 * - 点击/触摸切换显示
 * - 键盘 Tab 聚焦显示，Esc 关闭
 * - 滚动或窗口尺寸变化时直接关闭（浮层是 fixed 定位，不跟随重排）
 */

import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { createPortal } from "react-dom"
import { findTerms, type GlossaryEntry } from "./glossary"
import { useLocale } from "./i18n"

type Anchor = { left: number; top: number; bottom: number; width: number }

/** 浮层内容。术语与「ⓘ 富文案」共用同一个浮层，避免两套 tooltip 打架。 */
export type TipPayload = { id: string; title: string; subtitle?: string; body: string }
type Active = { payload: TipPayload; anchor: Anchor }

type GlossaryApi =
  | { show: (payload: TipPayload, el: Element) => void; hide: () => void }
  | null

const Ctx = createContext<GlossaryApi>(null)

export function GlossaryProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<Active | null>(null)
  const openKey = useRef<string | null>(null)

  const show = useCallback((payload: TipPayload, el: Element) => {
    const r = el.getBoundingClientRect()
    openKey.current = payload.id
    setActive({ payload, anchor: { left: r.left, top: r.top, bottom: r.bottom, width: r.width } })
  }, [])

  const hide = useCallback(() => {
    openKey.current = null
    setActive(null)
  }, [])

  // 浮层是 fixed 定位：滚动/缩放后位置就错了，直接关掉比试图跟随更可靠。
  useLayoutEffect(() => {
    if (!active) return
    const onMove = () => hide()
    window.addEventListener("scroll", onMove, true)
    window.addEventListener("resize", onMove)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") hide()
    }
    window.addEventListener("keydown", onKey)
    return () => {
      window.removeEventListener("scroll", onMove, true)
      window.removeEventListener("resize", onMove)
      window.removeEventListener("keydown", onKey)
    }
  }, [active, hide])

  const api = useMemo(() => ({ show, hide }), [show, hide])

  return (
    <Ctx.Provider value={api}>
      {children}
      {active ? <Tip active={active} onClose={hide} /> : null}
    </Ctx.Provider>
  )
}

function Tip({ active, onClose }: { active: Active; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ left: number; top: number; place: "top" | "bottom" } | null>(null)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const margin = 10
    const tw = el.offsetWidth
    const th = el.offsetHeight
    const { anchor } = active
    // 水平：以术语为中心，再夹到视口内
    const left = Math.min(
      Math.max(anchor.left + anchor.width / 2, margin + tw / 2),
      Math.max(margin + tw / 2, window.innerWidth - margin - tw / 2),
    )
    // 垂直：优先上方，上方放不下就翻到下方
    const place: "top" | "bottom" = anchor.top - th - 12 < 64 ? "bottom" : "top"
    const top = place === "top" ? anchor.top - 8 : anchor.bottom + 8
    setPos({ left, top, place })
  }, [active])


  return createPortal(
    <div
      ref={ref}
      role="tooltip"
      id={`glossary-${active.payload.id}`}
      className="pointer-events-none fixed z-[100] w-[min(21rem,calc(100vw-1.25rem))] rounded-lg border border-border bg-card px-3 py-2 text-xs leading-relaxed text-card-foreground shadow-xl"
      style={{
        left: pos ? pos.left : -9999,
        top: pos ? pos.top : -9999,
        transform: pos ? (pos.place === "top" ? "translate(-50%,-100%)" : "translate(-50%,0)") : undefined,
        visibility: pos ? "visible" : "hidden",
      }}
      // 关闭由外部触发（移开/点别处/Esc）；这里留一个空 onClick 防止事件冒泡到卡片
      onClick={(e) => {
        e.stopPropagation()
        onClose()
      }}
    >
      <div className="mb-0.5 font-semibold text-foreground">
        {active.payload.title}
        {active.payload.subtitle ? (
          <span className="ml-1.5 font-normal text-muted-foreground">{active.payload.subtitle}</span>
        ) : null}
      </div>
      <div className="text-muted-foreground">{active.payload.body}</div>
    </div>,
    document.body,
  )
}

/** 单个术语的触发元素 */
function Term({ text, entry }: { text: string; entry: GlossaryEntry }) {
  const api = useContext(Ctx)
  const { locale } = useLocale()
  const ref = useRef<HTMLSpanElement>(null)
  const [open, setOpen] = useState(false)

  const show = () => {
    if (!api || !ref.current || open) return
    setOpen(true)
    api.show(
      {
        id: entry.key,
        title: entry.term,
        subtitle: locale === "zh" ? entry.fullZh : entry.fullEn,
        body: locale === "zh" ? entry.zh : entry.en,
      },
      ref.current,
    )
  }
  const hide = () => {
    if (!api || !open) return
    setOpen(false)
    api.hide()
  }

  return (
    <span
      ref={ref}
      tabIndex={0}
      aria-describedby={open ? `glossary-${entry.key}` : undefined}
      className="cursor-help underline decoration-dotted decoration-muted-foreground/70 underline-offset-2 outline-none focus-visible:ring-1 focus-visible:ring-ring"
      onPointerEnter={(e) => {
        // 只有真鼠标才走 hover；触屏 hover 是假的，会粘住不放
        if (e.pointerType === "mouse") show()
      }}
      onPointerLeave={(e) => {
        if (e.pointerType === "mouse") hide()
      }}
      onPointerDown={(e) => {
        // 触屏/手写笔：点一下展开、再点收起，并阻止冒泡到外层可点卡片
        if (e.pointerType === "mouse") return
        e.stopPropagation()
        if (open) hide()
        else show()
      }}
      onFocus={show}
      onBlur={hide}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault()
          if (open) hide()
          else show()
        }
      }}
    >
      {text}
    </span>
  )
}

/**
 * 把一段文案里的术语自动挂上说明。
 *
 * `once`（默认 true）：同一个 GT 里同一个术语只标第一次出现，
 * 否则一个指标块里三处 TSS 会 dotted 三遍，很吵。
 */
export function GT({ text, className, once = true }: { text: string; className?: string; once?: boolean }) {
  const pieces = useMemo(() => findTerms(text), [text])
  const seen = new Set<string>()
  if (!pieces.some((p) => p.kind === "term")) {
    return className ? <span className={className}>{text}</span> : <>{text}</>
  }
  return (
    <span className={className}>
      {pieces.map((p, i) => {
        if (p.kind === "text") return <span key={i}>{p.text}</span>
        if (once && seen.has(p.entry.key)) return <span key={i}>{p.text}</span>
        seen.add(p.entry.key)
        return <Term key={i} text={p.text} entry={p.entry} />
      })}
    </span>
  )
}

/**
 * ⓘ 富文案提示（活动详情的指标说明用这个）。
 * 与术语共用同一个浮层：两套 tooltip 实现迟早长得不一样、行为也不一样。
 */
export function InfoTip({ label, text }: { label?: string; text: string }) {
  const api = useContext(Ctx)
  const uid = useId()
  const ref = useRef<HTMLSpanElement>(null)
  const [open, setOpen] = useState(false)

  const show = () => {
    if (!api || !ref.current || open) return
    setOpen(true)
    api.show({ id: uid, title: label ?? "", body: text }, ref.current)
  }
  const hide = () => {
    if (!api || !open) return
    setOpen(false)
    api.hide()
  }

  return (
    <span
      ref={ref}
      tabIndex={0}
      role="button"
      aria-label={text}
      aria-describedby={open ? `glossary-${uid}` : undefined}
      className="ml-1 inline-flex h-3.5 w-3.5 cursor-help items-center justify-center rounded-full border border-border align-middle text-[9px] leading-none text-muted-foreground outline-none focus-visible:ring-1 focus-visible:ring-ring"
      onPointerEnter={(e) => {
        if (e.pointerType === "mouse") show()
      }}
      onPointerLeave={(e) => {
        if (e.pointerType === "mouse") hide()
      }}
      onPointerDown={(e) => {
        if (e.pointerType === "mouse") return
        e.stopPropagation()
        if (open) hide()
        else show()
      }}
      onFocus={show}
      onBlur={hide}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault()
          if (open) hide()
          else show()
        }
      }}
    >
      i
    </span>
  )
}
