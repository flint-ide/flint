import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent } from 'react'
import {
  fitLabel, formatValue, hash01, hitTest, isRuntimeFrame, layoutDepth, layoutFlame, mixHex, packageOf, percent, samePath, shortName,
  type FlameFrame, type FramePath, type FrameRect, type SearchResult,
} from '../../lib/flame'
import { useSettings } from '../../store/settings'
import { modKey, modifierName } from '../../lib/platform'

/** Row height of a frame (CSS px). */
const ROW = 18

interface Props {
  root: FlameFrame
  unit: string
  focus: FramePath
  onFocus: (path: number[]) => void
  search: SearchResult
  /** Opens the frame's source (⌘-click, double-click). */
  onOpen: (frame: FlameFrame) => void
}

interface Hover {
  rect: FrameRect
  clientX: number
  clientY: number
}

/**
 * Icicle flame graph on one <canvas>: the root on top, callees below, widths in
 * proportion to cumulative value. Click zooms into a frame (ancestors stay on top
 * and zoom back out), Escape resets, ⌘-click or double-click opens the source.
 */
export function FlameGraph({ root, unit, focus, onFocus, search, onOpen }: Props) {
  const scroller = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [width, setWidth] = useState(0)
  const [hover, setHover] = useState<Hover | null>(null)

  // Hidden tabs measure 0 wide; the observer fires again once the tab shows.
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    const measure = () => setWidth(el.clientWidth)
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const rects = useMemo(() => layoutFlame(root, focus, width), [root, focus, width])
  const rows = layoutDepth(rects)

  // Redrawn when the hovered frame changes, not on every mouse move, and when the
  // theme does (the canvas reads its colors from CSS variables while drawing).
  const hovered = hover?.rect ?? null
  const theme = useSettings((s) => s.theme)
  useEffect(() => {
    const c = canvas.current
    if (!c) return
    const frame = requestAnimationFrame(() => draw(c, rects, width, rows, search, hovered))
    return () => cancelAnimationFrame(frame)
  }, [rects, width, rows, search, hovered, theme])

  const at = (e: MouseEvent): FrameRect | null => {
    const box = canvas.current?.getBoundingClientRect()
    return box ? hitTest(rects, e.clientX - box.left, e.clientY - box.top, ROW) : null
  }
  const focused = rects.find((r) => !r.ancestor) ?? null

  return (
    <div
      ref={scroller}
      className="flame-scroll"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && focus.length > 0) {
          e.preventDefault()
          onFocus([])
        }
      }}
    >
      <canvas
        ref={canvas}
        className="flame-canvas"
        onMouseMove={(e) => {
          const rect = at(e)
          setHover(rect ? { rect, clientX: e.clientX, clientY: e.clientY } : null)
        }}
        onMouseLeave={() => setHover(null)}
        onClick={(e) => {
          const rect = at(e)
          if (!rect) return
          if (modKey(e)) onOpen(rect.frame)
          else if (!samePath(rect.path, focus)) onFocus(rect.path)
        }}
        onDoubleClick={(e) => {
          const rect = at(e)
          if (rect) onOpen(rect.frame)
        }}
      />
      {hover && <Tooltip hover={hover} rootTotal={root.total} focusTotal={focused?.frame.total ?? root.total} zoomed={focus.length > 0} unit={unit} />}
    </div>
  )
}

function Tooltip({ hover, rootTotal, focusTotal, zoomed, unit }: { hover: Hover; rootTotal: number; focusTotal: number; zoomed: boolean; unit: string }) {
  const { frame } = hover.rect
  // Fixed to the viewport; flipped left near the right edge.
  const left = hover.clientX + 300 > window.innerWidth ? hover.clientX - 312 : hover.clientX + 12
  return (
    <div className="flame-tooltip" style={{ left: Math.max(4, left), top: hover.clientY + 14 }}>
      <div className="flame-tooltip-name">{frame.name}</div>
      {frame.file && (
        <div className="muted truncate">
          {frame.file}
          {frame.line ? `:${frame.line}` : ''}
        </div>
      )}
      <div className="flame-tooltip-values">
        <span>Total</span>
        <b>{formatValue(frame.total, unit)}</b>
        <span>{percent(frame.total, rootTotal)}{zoomed && ` · ${percent(frame.total, focusTotal)} of zoom`}</span>
        <span>Self</span>
        <b>{formatValue(frame.self, unit)}</b>
        <span>{percent(frame.self, rootTotal)}</span>
      </div>
      <div className="muted flame-tooltip-hint">Click to zoom · {modifierName('Mod')}-click or double-click opens the source</div>
    </div>
  )
}

interface Theme {
  font: string
  text: string
  hover: string
  from: string
  to: string
  runtimeFrom: string
  runtimeTo: string
  match: string
}

/** Flame colors come from CSS variables (style.css), like the rest of the app. */
function readTheme(el: Element): Theme {
  const css = getComputedStyle(el)
  const v = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback
  return {
    font: v('--font-ui', 'sans-serif'),
    text: v('--flame-text', '#f4f4f5'),
    hover: v('--fg', '#d4d4d8'),
    from: v('--flame-from', '#8f3a1a'),
    to: v('--flame-to', '#a8741a'),
    runtimeFrom: v('--flame-runtime-from', '#34435a'),
    runtimeTo: v('--flame-runtime-to', '#4a5a74'),
    match: v('--flame-match', '#7c5cc4'),
  }
}

function draw(c: HTMLCanvasElement, rects: FrameRect[], width: number, rows: number, search: SearchResult, hover: FrameRect | null): void {
  const dpr = window.devicePixelRatio || 1
  const height = rows * ROW
  const pw = Math.round(width * dpr)
  const ph = Math.round(height * dpr)
  if (c.width !== pw || c.height !== ph) {
    c.width = pw
    c.height = ph
    c.style.width = `${width}px`
    c.style.height = `${height}px`
  }
  const ctx = c.getContext('2d')
  if (!ctx) return
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, width, height)
  const theme = readTheme(c)
  ctx.font = `11px ${theme.font}`
  ctx.textBaseline = 'middle'
  const charW = ctx.measureText('abcdefghijklmnopqrstuvwxyz').width / 26 || 6
  const colors = new Map<string, string>()
  const colorOf = (name: string) => {
    const pkg = packageOf(name)
    let color = colors.get(pkg)
    if (!color) {
      const t = hash01(pkg)
      color = isRuntimeFrame(name) ? mixHex(theme.runtimeFrom, theme.runtimeTo, t) : mixHex(theme.from, theme.to, t)
      colors.set(pkg, color)
    }
    return color
  }
  const searching = search.matched.size > 0

  for (const r of rects) {
    const y = r.depth * ROW
    const w = Math.max(r.w - 1, 0.5) // a 1px gap between neighbours
    const matched = search.matched.has(r.frame)
    ctx.globalAlpha = searching && !matched ? 0.3 : r.ancestor ? 0.55 : 1
    ctx.fillStyle = matched ? theme.match : colorOf(r.frame.name)
    ctx.fillRect(r.x, y, w, ROW - 1)
    if (w > 24) {
      const label = fitLabel(shortName(r.frame.name), Math.floor((w - 8) / charW))
      if (label) {
        ctx.fillStyle = theme.text
        ctx.fillText(label, r.x + 4, y + ROW / 2)
      }
    }
  }
  ctx.globalAlpha = 1
  if (hover) {
    ctx.strokeStyle = theme.hover
    ctx.lineWidth = 1
    ctx.strokeRect(hover.x + 0.5, hover.depth * ROW + 0.5, Math.max(hover.w - 2, 1), ROW - 2)
  }
}
