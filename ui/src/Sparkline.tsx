import { formatCompact, sparklineAreaPath, sparklineDots, sparklinePath, sparklinePoints, sparklineSummary } from './sparklineMath'
import { classNames } from './treeIndent'

export type SparklineProps = {
  /** The series, oldest first. `null` is a gap that breaks the line. */
  values: readonly (number | null)[]
  /** What the series is ("Tokens per day"); read out with a summary of the values. */
  label: string
  width?: number
  height?: number
  /** A soft fill under the line. */
  area?: boolean
  tone?: 'accent' | 'ok' | 'warn' | 'danger' | 'muted'
  /** Pin the bottom of the scale so several sparklines are comparable. Default: the data's minimum. */
  min?: number
  /** Pin the top of the scale. Default: the data's maximum. */
  max?: number
  /** Formats the figures in the accessible summary. Default: 1200 → "1.2k". */
  format?: (n: number) => string
  className?: string
}

const PADDING = 2
const DOT_RADIUS = 1.5

/** A tiny inline trend line for a table row or a StatTile, with a dot on the last value. */
export function Sparkline({ values, label, width = 64, height = 16, area = false, tone = 'accent', min, max, format = formatCompact, className }: SparklineProps) {
  const segments = sparklinePoints(values, { width, height, min, max, padding: PADDING })
  const dots = sparklineDots(segments)
  const line = sparklinePath(segments)
  const fill = area ? sparklineAreaPath(segments, height - PADDING) : ''
  const empty = segments.length === 0
  return (
    <svg
      className={classNames('fui-sparkline', className)}
      data-tone={empty ? 'muted' : tone}
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={sparklineSummary(label, values, format)}
    >
      {empty && <line className="fui-sparkline-base" x1={PADDING} x2={width - PADDING} y1={height / 2} y2={height / 2} />}
      {fill && <path className="fui-sparkline-area" d={fill} />}
      {line && <path className="fui-sparkline-line" d={line} />}
      {dots.map((d) => (
        <circle key={`${d.x}:${d.y}`} className="fui-sparkline-dot" cx={d.x} cy={d.y} r={DOT_RADIUS} />
      ))}
    </svg>
  )
}
