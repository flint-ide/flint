// Flame graph math without the DOM: frame rectangles of an icicle layout (root on
// top) zoomed into a frame, hit testing, search, value formatting and colors. The
// canvas in components/profile draws what this computes; the call tree itself is
// aggregated and trimmed in Go (internal/profiling).

/** One frame of the call tree (profiling.Frame). total includes the children. */
export interface FlameFrame {
  name: string
  file?: string
  line?: number
  self: number
  total: number
  children?: FlameFrame[]
}

/** Child indexes from the root to a frame; [] is the root. */
export type FramePath = readonly number[]

/** A frame placed on the canvas (CSS pixels; rows are depths). */
export interface FrameRect {
  frame: FlameFrame
  path: number[]
  depth: number
  x: number
  w: number
  /** An ancestor of the zoomed frame: drawn full width, dimmed. */
  ancestor: boolean
}

/** Frames narrower than this (px) are not laid out, nor their children. */
export const MIN_FRAME_WIDTH = 0.5

/** The frame at path, or null when the path no longer exists. */
export function frameAt(root: FlameFrame, path: FramePath): FlameFrame | null {
  let f: FlameFrame | undefined = root
  for (const i of path) {
    f = f?.children?.[i]
    if (!f) return null
  }
  return f
}

/**
 * Lays out the tree zoomed into focus: the focused frame spans the width, its
 * ancestors sit above it at full width, and descendants get widths in proportion
 * to their totals, starting at their parent's left edge (children are sent
 * heaviest first, so the hot path reads along the left).
 */
export function layoutFlame(root: FlameFrame, focus: FramePath, width: number, minWidth = MIN_FRAME_WIDTH): FrameRect[] {
  const target = frameAt(root, focus) ? focus : []
  const rects: FrameRect[] = []
  let f = root
  for (let d = 0; d < target.length; d++) {
    rects.push({ frame: f, path: target.slice(0, d), depth: d, x: 0, w: width, ancestor: true })
    f = f.children![target[d]]
  }
  if (width <= 0 || f.total <= 0) return rects
  const scale = width / f.total
  const place = (frame: FlameFrame, path: number[], depth: number, x: number, w: number) => {
    rects.push({ frame, path, depth, x, w, ancestor: false })
    let cx = x
    frame.children?.forEach((c, i) => {
      const cw = c.total * scale
      if (cw >= minWidth) place(c, [...path, i], depth + 1, cx, cw)
      cx += cw
    })
  }
  place(f, [...target], target.length, 0, width)
  return rects
}

/** Number of rows the layout needs. */
export function layoutDepth(rects: readonly FrameRect[]): number {
  let max = -1
  for (const r of rects) if (r.depth > max) max = r.depth
  return max + 1
}

/** The frame under a point (CSS px), rows rowHeight tall. */
export function hitTest(rects: readonly FrameRect[], x: number, y: number, rowHeight: number): FrameRect | null {
  if (y < 0) return null
  const depth = Math.floor(y / rowHeight)
  for (const r of rects) if (r.depth === depth && x >= r.x && x < r.x + r.w) return r
  return null
}

export function samePath(a: FramePath, b: FramePath): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

export interface SearchResult {
  /** Frames whose name contains the query (case-insensitive). */
  matched: Set<FlameFrame>
  /** Value of matched frames, counted once where matches nest (e.g. recursion). */
  total: number
}

export function searchFrames(root: FlameFrame, query: string): SearchResult {
  const matched = new Set<FlameFrame>()
  const q = query.trim().toLowerCase()
  if (!q) return { matched, total: 0 }
  let total = 0
  const walk = (f: FlameFrame, inMatch: boolean) => {
    const hit = f.name.toLowerCase().includes(q)
    if (hit) {
      matched.add(f)
      if (!inMatch) total += f.total
    }
    f.children?.forEach((c) => walk(c, inMatch || hit))
  }
  walk(root, false)
  return { matched, total }
}

/** Formats a sample value in the profile's unit. */
export function formatValue(v: number, unit: string): string {
  switch (unit) {
    case 'nanoseconds': {
      const abs = Math.abs(v)
      if (abs >= 1e9) return `${trim(v / 1e9)} s`
      if (abs >= 1e6) return `${trim(v / 1e6)} ms`
      if (abs >= 1e3) return `${trim(v / 1e3)} µs`
      return `${v} ns`
    }
    case 'bytes': {
      const units = ['B', 'KB', 'MB', 'GB', 'TB']
      let x = v
      let i = 0
      while (Math.abs(x) >= 1024 && i < units.length - 1) {
        x /= 1024
        i++
      }
      return i === 0 ? `${v} B` : `${trim(x)} ${units[i]}`
    }
    default: {
      const abs = Math.abs(v)
      if (abs >= 1e9) return `${trim(v / 1e9)}G`
      if (abs >= 1e6) return `${trim(v / 1e6)}M`
      if (abs >= 1e4) return `${trim(v / 1e3)}k`
      return String(v)
    }
  }
}

/** Up to 3 significant digits: 1.23, 12.3, 123. */
function trim(x: number): string {
  const abs = Math.abs(x)
  const digits = abs >= 100 ? 0 : abs >= 10 ? 1 : 2
  const fixed = x.toFixed(digits)
  return digits ? fixed.replace(/\.?0+$/, '') : fixed // only fractional zeros go
}

export function percent(v: number, total: number): string {
  if (total <= 0) return '0%'
  const p = (v / total) * 100
  return `${p >= 10 || p === 0 ? p.toFixed(1) : p.toFixed(2)}%`
}

/** A Go function name without its import path: "flint/internal/x.(*T).M" → "x.(*T).M". */
export function shortName(name: string): string {
  const paren = name.indexOf('(')
  const head = paren >= 0 ? name.slice(0, paren) : name
  const slash = head.lastIndexOf('/')
  return slash >= 0 ? name.slice(slash + 1) : name
}

/** The package part of a Go function name ("flint/internal/x" of "flint/internal/x.F"). */
export function packageOf(name: string): string {
  const short = shortName(name)
  const prefix = name.slice(0, name.length - short.length)
  const dot = short.indexOf('.')
  return prefix + (dot >= 0 ? short.slice(0, dot) : short)
}

/** Shortens text to at most chars characters, ending in "…". */
export function fitLabel(text: string, chars: number): string {
  if (chars <= 1) return ''
  return text.length <= chars ? text : `${text.slice(0, chars - 1)}…`
}

/** Stable 0..1 value of a string (FNV-1a), so a package keeps its color. */
export function hash01(s: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 0x01000193)
  }
  return (h >>> 0) / 0xffffffff
}

/** Mixes two #rrggbb colors; t = 0 gives a, 1 gives b. Invalid input yields a. */
export function mixHex(a: string, b: string, t: number): string {
  const pa = parseHex(a)
  const pb = parseHex(b)
  if (!pa || !pb) return a
  const k = Math.min(1, Math.max(0, t))
  const c = pa.map((v, i) => Math.round(v + (pb[i] - v) * k))
  return `#${c.map((v) => v.toString(16).padStart(2, '0')).join('')}`
}

function parseHex(s: string): number[] | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(s.trim())
  if (!m) return null
  const n = parseInt(m[1], 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

/** Go runtime and system frames, drawn in their own color. */
export function isRuntimeFrame(name: string): boolean {
  return /^(?:(?:runtime|syscall|reflect|sync)[./]|internal\/)/.test(name)
}

export type TopSortKey = 'flat' | 'cum' | 'name'

export interface TopRow {
  name: string
  file?: string
  line?: number
  flat: number
  cum: number
}

/** Sorts the top table (descending values, ascending names); returns a new array. */
export function sortTop<T extends TopRow>(rows: readonly T[], key: TopSortKey): T[] {
  const byName = (a: T, b: T) => a.name.localeCompare(b.name)
  const cmp =
    key === 'name'
      ? byName
      : key === 'cum'
        ? (a: T, b: T) => b.cum - a.cum || b.flat - a.flat || byName(a, b)
        : (a: T, b: T) => b.flat - a.flat || b.cum - a.cum || byName(a, b)
  return [...rows].sort(cmp)
}
