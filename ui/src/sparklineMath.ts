/** Geometry and text for `Sparkline`. Pure and DOM-free, so it is tested with node:test. */

export type SparkPoint = { readonly x: number; readonly y: number }

export type SparklineBox = {
  width: number
  height: number
  /** Pinned bottom of the scale; default is the smallest value. */
  min?: number
  /** Pinned top of the scale; default is the largest value. */
  max?: number
  /** Space kept free on every side, so the stroke and the dot are not clipped. Default 2. */
  padding?: number
}

const isReal = (v: number | null | undefined): v is number => typeof v === 'number' && Number.isFinite(v)

/** The real (finite, non-null) values, in order. */
function realValues(values: readonly (number | null)[]): number[] {
  return values.filter(isReal)
}

/** Smallest and largest of the real values, by loop (spreading a long array into Math.min overflows the stack). */
function dataRange(values: readonly (number | null)[]): { min: number; max: number } | undefined {
  let min = Number.POSITIVE_INFINITY
  let max = Number.NEGATIVE_INFINITY
  for (const v of values) {
    if (!isReal(v)) continue
    if (v < min) min = v
    if (v > max) max = v
  }
  return min <= max ? { min, max } : undefined
}

/**
 * The scale the points are drawn on: pinned ends win over the data's range. An unpinned end
 * never crosses a pinned one, so data entirely outside the pins clips to an edge.
 */
function scaleOf(range: { min: number; max: number }, box: SparklineBox): { lo: number; hi: number; degenerate: boolean } {
  const lo = box.min ?? Math.min(range.min, box.max ?? range.min)
  const hi = box.max ?? Math.max(range.max, box.min ?? range.max)
  return { lo, hi, degenerate: box.min !== undefined && box.max !== undefined && !(box.max > box.min) }
}

/**
 * Points for a polyline, one array per run of consecutive values (a `null` breaks the line).
 * Values outside a pinned min/max are clipped to the edge. A constant series (or a pinned
 * scale with no height) is a flat line in the middle; a single value is a point in the middle.
 */
export function sparklinePoints(values: readonly (number | null)[], box: SparklineBox): SparkPoint[][] {
  const range = dataRange(values)
  if (!range) return []
  const pad = box.padding ?? 2
  const { lo, hi, degenerate } = scaleOf(range, box)
  const innerW = Math.max(0, box.width - 2 * pad)
  const innerH = Math.max(0, box.height - 2 * pad)
  const xAt = (i: number) => (values.length < 2 ? box.width / 2 : pad + (innerW * i) / (values.length - 1))
  const yAt = (v: number) => {
    if (degenerate) return box.height / 2
    if (hi > lo) return pad + innerH * (1 - (Math.min(Math.max(v, lo), hi) - lo) / (hi - lo))
    if (v < lo) return pad + innerH
    return v > hi ? pad : box.height / 2
  }
  const segments: SparkPoint[][] = []
  let current: SparkPoint[] = []
  values.forEach((v, i) => {
    if (isReal(v)) {
      current.push({ x: xAt(i), y: yAt(v) })
    } else if (current.length > 0) {
      segments.push(current)
      current = []
    }
  })
  if (current.length > 0) segments.push(current)
  return segments
}

/** The point the "last value" dot sits on. */
export function lastPoint(segments: readonly (readonly SparkPoint[])[]): SparkPoint | undefined {
  const last = segments[segments.length - 1]
  return last?.[last.length - 1]
}

const num = (n: number): string => String(Math.round(n * 100) / 100)

/** SVG path data: one `M … L …` run per segment of two or more points; lone points are drawn by `sparklineDots`. */
export function sparklinePath(segments: readonly (readonly SparkPoint[])[]): string {
  return segments
    .filter((s) => s.length > 1)
    .map((s) => s.map((p, i) => `${i === 0 ? 'M' : 'L'}${num(p.x)},${num(p.y)}`).join(' '))
    .join(' ')
}

/** Where dots go: every isolated point (a line cannot show it) and the last point. */
export function sparklineDots(segments: readonly (readonly SparkPoint[])[]): SparkPoint[] {
  const isolated = segments.filter((s) => s.length === 1).map((s) => s[0])
  const last = lastPoint(segments)
  return last && !isolated.includes(last) ? [...isolated, last] : isolated
}

/** SVG path data for the soft fill: each segment closed down to `baseline`. */
export function sparklineAreaPath(segments: readonly (readonly SparkPoint[])[], baseline: number): string {
  return segments
    .filter((s) => s.length > 1)
    .map((s) => {
      const line = s.map((p, i) => `${i === 0 ? 'M' : 'L'}${num(p.x)},${num(p.y)}`)
      const first = s[0]
      const last = s[s.length - 1]
      return [...line, `L${num(last.x)},${num(baseline)}`, `L${num(first.x)},${num(baseline)}`, 'Z'].join(' ')
    })
    .join(' ')
}

/** 1200 → "1.2k", 41000000 → "41M"; small numbers keep up to two decimals. */
export function formatCompact(n: number): string {
  return n < 0 ? `-${compactAbs(-n)}` : compactAbs(n)
}

const UNITS: readonly (readonly [number, string])[] = [
  [1e3, 'k'],
  [1e6, 'M'],
  [1e9, 'B'],
]

/** `formatCompact` for a non-negative number; a value that rounds up to 1000 moves to the next unit. */
function compactAbs(abs: number): string {
  const start = UNITS.reduce((found, [size], i) => (abs >= size ? i : found), -1)
  if (start < 0) {
    const small = Math.round(abs * 100) / 100
    return small >= 1000 ? compactAbs(small) : String(small)
  }
  const scaled = Math.round((abs / UNITS[start][0]) * 10) / 10
  if (scaled >= 1000 && start < UNITS.length - 1) return `${Math.round((scaled / 1000) * 10) / 10}${UNITS[start + 1][1]}`
  return `${scaled}${UNITS[start][1]}`
}

/** "Tokens per day: 7 values, last 1.2k, min 300, max 1.9k" for the accessible name. */
export function sparklineSummary(label: string, values: readonly (number | null)[], format: (n: number) => string = formatCompact): string {
  const real = realValues(values)
  const range = dataRange(real)
  if (!range) return `${label}: no data`
  const count = `${real.length} ${real.length === 1 ? 'value' : 'values'}`
  return `${label}: ${count}, last ${format(real[real.length - 1])}, min ${format(range.min)}, max ${format(range.max)}`
}
