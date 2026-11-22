import { Tooltip, TooltipContent, TooltipTrigger } from '@wunderui/react'
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent } from 'react'
import { formatValue, hitTest, layoutDepth, layoutFlame, percent, samePath, type FlameFrame, type FramePath, type FrameRect, type SearchResult } from './flame'
import { drawFlame, FLAME_ROW } from './flameDraw'

export type FlameGraphProps = {
  /** The call tree; each frame's total includes its children. */
  root: FlameFrame
  /** Unit of the values: 'nanoseconds', 'bytes' or a plain count. */
  unit: string
  /** Child indexes from the root to the zoomed frame; [] shows the whole tree. */
  focus: FramePath
  /** Click on a frame (zoom in) or Escape ([]). */
  onZoom: (path: number[]) => void
  /** Frames to highlight; the rest fades. */
  search: SearchResult
  /** Opens the frame's source (modifier-click, double-click). */
  onOpen: (frame: FlameFrame) => void
  /** Which click opens the source instead of zooming. Default: Cmd or Ctrl held. */
  isOpenClick?: (e: MouseEvent) => boolean
  /** Last line of the tooltip. */
  openHint?: string
}

const defaultOpenClick = (e: MouseEvent) => e.metaKey || e.ctrlKey

/** Re-renders when Flint's theme changes on <html>: the canvas reads its colors from CSS variables. */
function useThemeVersion(): number {
  const [version, setVersion] = useState(0)
  useEffect(() => {
    const observer = new MutationObserver(() => setVersion((v) => v + 1))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'data-theme'] })
    return () => observer.disconnect()
  }, [])
  return version
}

/**
 * Icicle flame graph on one <canvas>: the root on top, callees below, widths in proportion to
 * cumulative value. Click zooms into a frame (ancestors stay on top and zoom back out), Escape resets,
 * a modifier-click or double-click opens the source. A canvas rather than a div per frame keeps big
 * profiles smooth; the frame under the pointer is found by hit testing, and the details show in the
 * kit's Tooltip anchored on an invisible box over that frame.
 */
export function FlameGraph({ root, unit, focus, onZoom, search, onOpen, isOpenClick = defaultOpenClick, openHint }: FlameGraphProps) {
  const scroller = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [width, setWidth] = useState(0)
  const [hovered, setHovered] = useState<FrameRect | null>(null)
  const theme = useThemeVersion()

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

  // Redrawn when the hovered frame changes, not on every mouse move.
  useEffect(() => {
    const c = canvas.current
    if (!c) return
    const frame = requestAnimationFrame(() => drawFlame(c, rects, width, rows, search, hovered))
    return () => cancelAnimationFrame(frame)
  }, [rects, width, rows, search, hovered, theme])

  const at = (e: MouseEvent): FrameRect | null => {
    const box = canvas.current?.getBoundingClientRect()
    return box ? hitTest(rects, e.clientX - box.left, e.clientY - box.top, FLAME_ROW) : null
  }
  const focused = rects.find((r) => !r.ancestor)?.frame ?? root

  return (
    <div
      ref={scroller}
      className="fui-flame"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && focus.length > 0) {
          e.preventDefault()
          onZoom([])
        }
      }}
    >
      <div className="fui-flame-stage">
        <canvas
          ref={canvas}
          className="fui-flame-canvas"
          onMouseMove={(e) => {
            const rect = at(e)
            // Same frame: keep the state, so nothing re-renders while the pointer moves inside it.
            setHovered((prev) => (prev && rect && prev.frame === rect.frame && prev.depth === rect.depth ? prev : rect))
          }}
          onMouseLeave={() => setHovered(null)}
          onClick={(e) => {
            const rect = at(e)
            if (!rect) return
            if (isOpenClick(e)) onOpen(rect.frame)
            else if (!samePath(rect.path, focus)) onZoom(rect.path)
          }}
          onDoubleClick={(e) => {
            const rect = at(e)
            if (rect) onOpen(rect.frame)
          }}
        />
        <Tooltip open={hovered !== null}>
          <TooltipTrigger
            render={<span aria-hidden className="fui-flame-anchor" />}
            style={
              hovered
                ? { left: hovered.x, top: hovered.depth * FLAME_ROW, width: Math.max(hovered.w - 1, 0.5), height: FLAME_ROW - 1 }
                : undefined
            }
          />
          <TooltipContent side="bottom" align="start" sideOffset={4} maxWidth={320}>
            {hovered && <FrameDetails frame={hovered.frame} rootTotal={root.total} focusTotal={focused.total} zoomed={focus.length > 0} unit={unit} hint={openHint} />}
          </TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
}

function FrameDetails(props: { frame: FlameFrame; rootTotal: number; focusTotal: number; zoomed: boolean; unit: string; hint?: string }) {
  const { frame, rootTotal, focusTotal, zoomed, unit, hint } = props
  return (
    <div className="fui-flame-tip">
      <div className="fui-flame-tip-name">{frame.name}</div>
      {frame.file && (
        <div className="fui-flame-tip-file">
          {frame.file}
          {frame.line ? `:${frame.line}` : ''}
        </div>
      )}
      <div className="fui-flame-tip-values">
        <span>Total</span>
        <b>{formatValue(frame.total, unit)}</b>
        <span>
          {percent(frame.total, rootTotal)}
          {zoomed && ` · ${percent(frame.total, focusTotal)} of zoom`}
        </span>
        <span>Self</span>
        <b>{formatValue(frame.self, unit)}</b>
        <span>{percent(frame.self, rootTotal)}</span>
      </div>
      {hint && <div className="fui-flame-tip-hint">{hint}</div>}
    </div>
  )
}
