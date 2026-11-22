import { fitLabel, frameAlpha, frameColor, packageOf, shortName, type FlamePalette, type FrameRect, type SearchResult } from './flame'

/** Row height of a frame (CSS px). */
export const FLAME_ROW = 18

interface Theme extends FlamePalette {
  font: string
  text: string
  hover: string
  match: string
}

/** Colors come from Flint's CSS variables, so a theme switch reaches the canvas. */
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

/** Paints the frames and the hover outline; sizes the canvas to rows × row height at the device pixel ratio. */
export function drawFlame(
  c: HTMLCanvasElement,
  rects: readonly FrameRect[],
  width: number,
  rows: number,
  search: SearchResult,
  hover: FrameRect | null,
): void {
  const dpr = window.devicePixelRatio || 1
  const height = rows * FLAME_ROW
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
  // The color is worked out once per package, not once per frame.
  const colors = new Map<string, string>()
  const colorOf = (name: string) => {
    const pkg = packageOf(name)
    let color = colors.get(pkg)
    if (!color) {
      color = frameColor(name, theme)
      colors.set(pkg, color)
    }
    return color
  }
  const searching = search.matched.size > 0

  for (const r of rects) {
    const y = r.depth * FLAME_ROW
    const w = Math.max(r.w - 1, 0.5) // a 1px gap between neighbours
    const matched = search.matched.has(r.frame)
    ctx.globalAlpha = frameAlpha({ searching, matched, ancestor: r.ancestor })
    ctx.fillStyle = matched ? theme.match : colorOf(r.frame.name)
    ctx.fillRect(r.x, y, w, FLAME_ROW - 1)
    if (w > 24) {
      const label = fitLabel(shortName(r.frame.name), Math.floor((w - 8) / charW))
      if (label) {
        ctx.fillStyle = theme.text
        ctx.fillText(label, r.x + 4, y + FLAME_ROW / 2)
      }
    }
  }
  ctx.globalAlpha = 1
  if (hover) {
    ctx.strokeStyle = theme.hover
    ctx.lineWidth = 1
    ctx.strokeRect(hover.x + 0.5, hover.depth * FLAME_ROW + 0.5, Math.max(hover.w - 2, 1), FLAME_ROW - 2)
  }
}
