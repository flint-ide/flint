import { durationScale, durationShares, durationValueText } from './duration'
import { classNames } from './treeIndent'

export type DurationBarProps = {
  /** The step's measured duration in ms. */
  value: number
  /** The expected duration in ms; draws a tick on the track and turns the fill red once value is above it. */
  estimate?: number
  /** The scale's maximum in ms; defaults to the larger of value and estimate. */
  total?: number
  /** What the bar measures, read by screen readers. */
  label?: string
  className?: string
}

/** One step's duration as a thin horizontal bar, with an optional estimate tick. */
export function DurationBar({ value, estimate, total, label = 'Duration', className }: DurationBarProps) {
  const input = { value, estimate, total }
  const shares = durationShares(input)
  const scale = durationScale(input)
  // A meter needs max > min, and the announced value must match the drawn fill.
  const valueMax = Math.max(scale, 1)
  const valueNow = (shares.fillPercent / 100) * scale
  return (
    <span
      role="meter"
      className={classNames('fui-duration', className)}
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={valueMax}
      aria-valuenow={valueNow}
      aria-valuetext={durationValueText(input)}
    >
      <span className="fui-duration-fill" data-over={shares.over || undefined} style={{ width: `${shares.fillPercent}%` }} />
      {shares.estimatePercent !== undefined && (
        <span className="fui-duration-tick" style={{ left: `${shares.estimatePercent}%` }} aria-hidden />
      )}
    </span>
  )
}
