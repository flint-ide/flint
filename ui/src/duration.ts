// Pure helpers of DurationBar, kept apart so they can be unit tested without a DOM.

export type DurationSharesInput = {
  /** Measured duration in ms. */
  value: number
  /** Expected duration in ms; a missing, zero or negative estimate draws no tick. */
  estimate?: number
  /** The scale's maximum in ms; defaults to the larger of value and estimate. */
  total?: number
}

export type DurationShares = {
  /** The fill as a percentage of the track, 0..100. */
  fillPercent: number
  /** Where the estimate tick sits, 0..100; undefined when there is no estimate. */
  estimatePercent: number | undefined
  /** True when the value is above a known estimate. */
  over: boolean
}

/** A finite, non-negative number, or 0 for anything else (NaN, infinite, negative). */
function nonNegative(n: number): number {
  return Number.isFinite(n) && n > 0 ? n : 0
}

/** A duration that counts: a finite, positive number, or undefined. */
function positiveOrUndefined(n: number | undefined): number | undefined {
  return n !== undefined && Number.isFinite(n) && n > 0 ? n : undefined
}

function clampPercent(part: number, whole: number): number {
  if (whole <= 0) return 0
  return Math.min(Math.max((part / whole) * 100, 0), 100)
}

/** The track's maximum in ms: the total when it is given, else the larger of value and estimate. */
export function durationScale({ value, estimate, total }: DurationSharesInput): number {
  return positiveOrUndefined(total) ?? Math.max(nonNegative(value), positiveOrUndefined(estimate) ?? 0)
}

/** Fill and tick positions of a duration bar, clamped to the track, and whether the value is over its estimate. */
export function durationShares(input: DurationSharesInput): DurationShares {
  const measured = nonNegative(input.value)
  const expected = positiveOrUndefined(input.estimate)
  const scale = durationScale(input)
  return {
    fillPercent: clampPercent(measured, scale),
    estimatePercent: expected === undefined ? undefined : clampPercent(expected, scale),
    over: expected !== undefined && measured > expected,
  }
}

/** A duration for people: "850ms", "12.4s", "3m 05s", "1h 02m". Negative and NaN input shows as 0. */
export function formatDuration(ms: number): string {
  const measured = nonNegative(ms)
  // Round first, then pick the unit, so 999.6 ms is "1.0s" and never "1000ms".
  const millis = Math.round(measured)
  if (millis < 1000) return `${millis}ms`
  const tenths = Math.round(measured / 100)
  if (tenths < 600) return `${(tenths / 10).toFixed(1)}s`
  const totalSeconds = Math.round(measured / 1000)
  const seconds = String(totalSeconds % 60).padStart(2, '0')
  if (totalSeconds < 3600) return `${Math.floor(totalSeconds / 60)}m ${seconds}s`
  const minutes = String(Math.floor(totalSeconds / 60) % 60).padStart(2, '0')
  return `${Math.floor(totalSeconds / 3600)}h ${minutes}m`
}

/** The accessible value of a duration bar: "12.4s of 10.0s estimated, over", or just the value without an estimate. */
export function durationValueText({ value, estimate }: DurationSharesInput): string {
  const text = formatDuration(value)
  if (positiveOrUndefined(estimate) === undefined) return text
  const { over } = durationShares({ value, estimate })
  return `${text} of ${formatDuration(estimate ?? 0)} estimated${over ? ', over' : ''}`
}
