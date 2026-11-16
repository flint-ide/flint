// A chart view of an extension (Go: internal/extensions/host/richviews.go)
// turned into what the SVG draws: axis ticks, a path per line, a box per
// bar. Go typed and bounded the points (x: a number, a time in ms for a
// date axis, or a category's text; y: a finite number). Framework-free.
import { formatNumber } from './viewTable'

export interface ChartPoint {
  x: unknown
  y: number
}

export interface ChartSeries {
  id: string
  label: string
  points: readonly ChartPoint[]
}

export interface ChartData {
  /** line, bar or area */
  type: string
  /** number, date or category */
  x: { type: string; label?: string }
  y: { label?: string; min?: number | null; max?: number | null }
  series: readonly ChartSeries[]
}

export interface Tick {
  /** Position in px along its axis. */
  at: number
  label: string
}

export interface Mark {
  x: number
  y: number
  width: number
  height: number
  /** What a hover or a screen reader says: "Open, 2026-10-01: 14". */
  title: string
}

export interface SeriesGeometry {
  id: string
  label: string
  /** The line of a line or area chart ('' for bars or no points). */
  line: string
  /** The filled shape of an area chart. */
  area: string
  /** Bars, or the dots of a line with few points. */
  marks: Mark[]
}

export interface ChartGeometry {
  width: number
  height: number
  plot: { left: number; top: number; right: number; bottom: number }
  yTicks: Tick[]
  xTicks: Tick[]
  series: SeriesGeometry[]
  /** No point to draw. */
  empty: boolean
}

/** The most categories (or distinct bar positions) a chart draws. */
export const MAX_CATEGORIES = 500
/** A line with at most this many points shows a dot on each. */
const MAX_DOTS = 60

/** A step of 1, 2 or 5 times a power of ten that splits span into about target parts. */
export function niceStep(span: number, target: number): number {
  if (!(span > 0) || !Number.isFinite(span)) return 1
  const raw = span / Math.max(1, target)
  const pow = 10 ** Math.floor(Math.log10(raw))
  const f = raw / pow
  return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10) * pow
}

/** Round tick values covering [min, max], about target of them: the axis runs from the first to the last. */
export function niceTicks(min: number, max: number, target = 5): number[] {
  if (!Number.isFinite(min) || !Number.isFinite(max)) return [0, 1]
  if (min > max) [min, max] = [max, min]
  if (min === max) {
    const pad = min === 0 ? 1 : Math.abs(min) * 0.1
    min -= pad
    max += pad
  }
  const step = niceStep(max - min, target)
  const first = Math.floor(min / step) * step
  const last = Math.ceil(max / step) * step
  const out: number[] = []
  // Multiples of the step, not a running sum: 0.1 + 0.2 never shows as 0.30000000000000004.
  for (let i = 0; i <= Math.round((last - first) / step) && out.length < 50; i++) {
    out.push(Number((first + i * step).toPrecision(12)))
  }
  return out
}

/** A short axis label: 950, 1.2k, 3.4M, 0.25. */
export function formatTick(n: number): string {
  const abs = Math.abs(n)
  for (const [limit, suffix] of [
    [1e12, 'T'],
    [1e9, 'B'],
    [1e6, 'M'],
    [1e3, 'k'],
  ] as const) {
    if (abs >= limit) return `${Number((n / limit).toPrecision(3))}${suffix}`
  }
  return String(Number(n.toPrecision(4)))
}

const pad = (n: number) => String(n).padStart(2, '0')

/** A date axis label for a time in ms: the day, or the hour and minute when the axis spans less than two days. */
export function formatDateTick(ms: number, span: number, utc = false): string {
  const d = new Date(ms)
  const [y, m, day, h, min] = utc
    ? [d.getUTCFullYear(), d.getUTCMonth() + 1, d.getUTCDate(), d.getUTCHours(), d.getUTCMinutes()]
    : [d.getFullYear(), d.getMonth() + 1, d.getDate(), d.getHours(), d.getMinutes()]
  if (span < 2 * 86400e3) return `${pad(h)}:${pad(min)}`
  return span > 300 * 86400e3 ? `${y}-${pad(m)}-${pad(day)}` : `${pad(m)}-${pad(day)}`
}

/** An x value in words, for titles and the data table. */
export function xText(axis: string, x: unknown, utc = false): string {
  if (axis === 'date' && typeof x === 'number') {
    const d = new Date(x)
    const iso = utc
      ? d.toISOString()
      : `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
    return iso.slice(11, 16) === '00:00' ? iso.slice(0, 10) : `${iso.slice(0, 10)} ${iso.slice(11, 16)}`
  }
  return typeof x === 'number' ? formatNumber(x) : typeof x === 'string' ? x : ''
}

const cut = (s: string, n: number) => (s.length > n ? `${s.slice(0, n - 1)}…` : s)

/** Whether the x axis is a band per value (bars, or categories) rather than a continuous scale. */
export function bandAxis(chart: ChartData): boolean {
  return chart.type === 'bar' || (chart.x.type !== 'number' && chart.x.type !== 'date')
}

/**
 * The bands of a band axis, in order: categories as they first appear,
 * numbers and dates ascending. At most MAX_CATEGORIES; points of later ones
 * are not drawn.
 */
export function bands(chart: ChartData): (string | number)[] {
  const seen = new Set<string | number>()
  for (const s of chart.series) {
    for (const p of s.points) {
      if (typeof p.x === 'string' || typeof p.x === 'number') seen.add(p.x)
    }
  }
  const all = [...seen]
  if (chart.x.type === 'number' || chart.x.type === 'date') all.sort((a, b) => Number(a) - Number(b))
  return all.slice(0, MAX_CATEGORIES)
}

/** The y axis' ticks: the data's range (from zero for bars and areas) widened to round numbers, unless the chart sets a bound. */
export function yTickValues(chart: ChartData, target = 5): number[] {
  let lo = Infinity
  let hi = -Infinity
  for (const s of chart.series) {
    for (const p of s.points) {
      if (Number.isFinite(p.y)) {
        lo = Math.min(lo, p.y)
        hi = Math.max(hi, p.y)
      }
    }
  }
  if (lo === Infinity) [lo, hi] = [0, 1]
  if (chart.type !== 'line') {
    lo = Math.min(lo, 0)
    hi = Math.max(hi, 0)
  }
  const min = typeof chart.y.min === 'number' && Number.isFinite(chart.y.min) ? chart.y.min : null
  const max = typeof chart.y.max === 'number' && Number.isFinite(chart.y.max) ? chart.y.max : null
  if (min !== null) lo = min
  if (max !== null) hi = max
  if (lo > hi) [lo, hi] = min !== null ? [lo, lo + 1] : [hi - 1, hi]
  const ticks = niceTicks(lo, hi, target)
  // A bound the chart sets is the axis' end, not the next round number.
  if (min !== null) {
    while (ticks.length > 1 && ticks[0] < min) ticks.shift()
    if (ticks[0] > min) ticks.unshift(min)
  }
  if (max !== null) {
    while (ticks.length > 1 && ticks[ticks.length - 1] > max) ticks.pop()
    if (ticks[ticks.length - 1] < max) ticks.push(max)
  }
  return ticks
}

const round = (n: number) => Math.round(n * 10) / 10

/**
 * Where everything of a chart goes in a width × height drawing. Values
 * outside the y axis (a chart's own min and max) are clamped to it.
 */
export function chartGeometry(chart: ChartData, width: number, height: number, utc = false): ChartGeometry {
  const yValues = yTickValues(chart, Math.max(2, Math.min(6, Math.floor(height / 40))))
  const yLabels = yValues.map(formatTick)
  const left = Math.min(72, 12 + Math.max(...yLabels.map((l) => l.length)) * 6.5)
  const plot = { left, top: 8, right: Math.max(left + 10, width - 10), bottom: Math.max(18, height - 22) }
  const y0 = yValues[0]
  const y1 = yValues[yValues.length - 1]
  const py = (v: number) => plot.bottom - ((Math.min(y1, Math.max(y0, v)) - y0) / (y1 - y0 || 1)) * (plot.bottom - plot.top)
  const yTicks = yValues.map((v, i) => ({ at: round(py(v)), label: yLabels[i] }))
  const base = py(0)
  const plotWidth = plot.right - plot.left
  const out: ChartGeometry = { width, height, plot, yTicks, xTicks: [], series: [], empty: true }
  const title = (s: ChartSeries, p: ChartPoint) => `${s.label}, ${xText(chart.x.type, p.x, utc)}: ${formatNumber(p.y)}`

  if (bandAxis(chart)) {
    const all = bands(chart)
    const index = new Map(all.map((b, i) => [b, i]))
    const band = plotWidth / Math.max(1, all.length)
    const labels = all.map((b) => cut(xText(chart.x.type, b, utc), 14))
    const room = Math.min(96, Math.max(...labels.map((l) => l.length), 1) * 6.5 + 10)
    const every = Math.max(1, Math.ceil(room / Math.max(band, 0.001)))
    for (let i = 0; i < all.length; i += every) out.xTicks.push({ at: round(plot.left + (i + 0.5) * band), label: labels[i] })
    const drawn = chart.series.length || 1
    const barWidth = chart.type === 'bar' ? Math.max(1, (band * 0.76) / drawn) : 0
    chart.series.forEach((s, si) => {
      const g: SeriesGeometry = { id: s.id, label: s.label, line: '', area: '', marks: [] }
      const pts: [number, number][] = []
      for (const p of s.points) {
        const i = index.get(p.x as string | number)
        if (i === undefined || !Number.isFinite(p.y)) continue
        const cx = plot.left + (i + 0.5) * band
        const cy = py(p.y)
        if (chart.type === 'bar') {
          const x = plot.left + i * band + band * 0.12 + si * barWidth
          g.marks.push({ x: round(x), y: round(Math.min(cy, base)), width: round(barWidth), height: round(Math.max(1, Math.abs(base - cy))), title: title(s, p) })
        } else {
          pts.push([cx, cy])
          if (s.points.length <= MAX_DOTS) g.marks.push({ x: round(cx), y: round(cy), width: 0, height: 0, title: title(s, p) })
        }
      }
      if (chart.type !== 'bar') {
        pts.sort((a, b) => a[0] - b[0])
        linePaths(g, pts, base, chart.type === 'area')
      }
      if (g.marks.length || g.line) out.empty = false
      out.series.push(g)
    })
    return out
  }

  // A continuous x axis: numbers, or times.
  let lo = Infinity
  let hi = -Infinity
  for (const s of chart.series) {
    for (const p of s.points) {
      if (typeof p.x === 'number' && Number.isFinite(p.x)) {
        lo = Math.min(lo, p.x)
        hi = Math.max(hi, p.x)
      }
    }
  }
  if (lo === Infinity) [lo, hi] = [0, 1]
  const span = hi - lo
  const px = (v: number) => (span === 0 ? plot.left + plotWidth / 2 : plot.left + ((v - lo) / span) * plotWidth)
  const count = Math.max(2, Math.min(8, Math.floor(plotWidth / 80)))
  if (span === 0) {
    out.xTicks.push({ at: round(px(lo)), label: chart.x.type === 'date' ? formatDateTick(lo, 0, utc) : formatTick(lo) })
  } else if (chart.x.type === 'date') {
    for (let i = 0; i < count; i++) {
      const v = lo + (span * i) / (count - 1)
      out.xTicks.push({ at: round(px(v)), label: formatDateTick(v, span, utc) })
    }
  } else {
    for (const v of niceTicks(lo, hi, count)) {
      if (v >= lo && v <= hi) out.xTicks.push({ at: round(px(v)), label: formatTick(v) })
    }
  }
  for (const s of chart.series) {
    const g: SeriesGeometry = { id: s.id, label: s.label, line: '', area: '', marks: [] }
    const real = s.points.filter((p) => typeof p.x === 'number' && Number.isFinite(p.x) && Number.isFinite(p.y))
    const sorted = [...real].sort((a, b) => (a.x as number) - (b.x as number))
    const pts = sorted.map((p): [number, number] => [px(p.x as number), py(p.y)])
    if (sorted.length <= MAX_DOTS) {
      sorted.forEach((p, i) => g.marks.push({ x: round(pts[i][0]), y: round(pts[i][1]), width: 0, height: 0, title: title(s, p) }))
    }
    linePaths(g, pts, base, chart.type === 'area')
    if (g.line || g.marks.length) out.empty = false
    out.series.push(g)
  }
  return out
}

function linePaths(g: SeriesGeometry, pts: readonly [number, number][], base: number, area: boolean): void {
  if (pts.length === 0) return
  g.line = pts.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${round(x)} ${round(y)}`).join('')
  if (area) g.area = `${g.line}L${round(pts[pts.length - 1][0])} ${round(base)}L${round(pts[0][0])} ${round(base)}Z`
}

/** One sentence about the chart, for screen readers. */
export function chartSummary(chart: ChartData, title: string): string {
  const points = chart.series.reduce((n, s) => n + s.points.length, 0)
  const kind = chart.type === 'bar' ? 'Bar chart' : chart.type === 'area' ? 'Area chart' : 'Line chart'
  const names = chart.series.map((s) => s.label).join(', ')
  return `${kind}${title ? `: ${title}` : ''}. ${chart.series.length} series${names ? ` (${cut(names, 200)})` : ''}, ${points} points.`
}

/** The chart's values as rows for a plain table: x, then one value per series ('' where it has none). At most limit rows. */
export function chartRows(chart: ChartData, limit = 500, utc = false): { header: string[]; rows: string[][]; more: number } {
  const xs: (string | number)[] = []
  const at = new Map<string | number, number>()
  for (const s of chart.series) {
    for (const p of s.points) {
      if ((typeof p.x === 'string' || typeof p.x === 'number') && !at.has(p.x)) {
        at.set(p.x, xs.length)
        xs.push(p.x)
      }
    }
  }
  if (chart.x.type !== 'category') {
    xs.sort((a, b) => Number(a) - Number(b))
    xs.forEach((x, i) => at.set(x, i))
  }
  const rows = xs.slice(0, limit).map((x) => [xText(chart.x.type, x, utc), ...chart.series.map(() => '')])
  chart.series.forEach((s, si) => {
    for (const p of s.points) {
      const i = at.get(p.x as string | number)
      if (i !== undefined && i < limit) rows[i][si + 1] = formatNumber(p.y)
    }
  })
  return { header: [chart.x.label || 'x', ...chart.series.map((s) => s.label)], rows, more: Math.max(0, xs.length - limit) }
}
