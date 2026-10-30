// Sizes an xterm.js terminal to its container (what @xterm/addon-fit did).
import type { Terminal } from '@xterm/xterm'

const MIN_COLS = 2
const MIN_ROWS = 1
const SCROLLBAR_WIDTH = 14 // xterm's default viewport scrollbar

// xterm exposes cell metrics only on its private render service, like the addon used.
interface CoreTerminal {
  _core?: { _renderService?: { dimensions?: { css: { cell: { width: number; height: number } } }; clear(): void } }
}

export interface Geometry {
  cols: number
  rows: number
}

/** Rows and columns that fit `available` px for the given cell size. */
export function geometryFor(width: number, height: number, cellWidth: number, cellHeight: number, scrollbar: number): Geometry | null {
  if (cellWidth <= 0 || cellHeight <= 0 || !Number.isFinite(width) || !Number.isFinite(height)) return null
  return {
    cols: Math.max(MIN_COLS, Math.floor((Math.max(0, width) - scrollbar) / cellWidth)),
    rows: Math.max(MIN_ROWS, Math.floor(height / cellHeight)),
  }
}

/** Resizes the terminal to fill its parent element; no-op while hidden or unmeasured. */
export function fitTerminal(term: Terminal): void {
  const el = term.element
  const parent = el?.parentElement
  const render = (term as unknown as CoreTerminal)._core?._renderService
  const cell = render?.dimensions?.css.cell
  if (!el || !parent || !render || !cell) return
  const outer = getComputedStyle(parent)
  const inner = getComputedStyle(el)
  const px = (s: CSSStyleDeclaration, prop: string) => parseInt(s.getPropertyValue(prop)) || 0
  const scrollbar = term.options.scrollback === 0 ? 0 : term.options.overviewRuler?.width || SCROLLBAR_WIDTH
  const geometry = geometryFor(
    px(outer, 'width') - px(inner, 'padding-left') - px(inner, 'padding-right'),
    px(outer, 'height') - px(inner, 'padding-top') - px(inner, 'padding-bottom'),
    cell.width,
    cell.height,
    scrollbar,
  )
  if (!geometry || (geometry.cols === term.cols && geometry.rows === term.rows)) return
  render.clear() // force a full redraw at the new size
  term.resize(geometry.cols, geometry.rows)
}
