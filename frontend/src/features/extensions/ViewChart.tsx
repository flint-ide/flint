import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { Chart } from '../../../bindings/flint/internal/extensions/host/models'
import { chartGeometry, chartRows, chartSummary, type ChartData } from '../../lib/chartScale'

/** Series colors are Flint's: the classes s0…s9 map to theme variables (style.css). */
const PALETTE = 10

interface Props {
  chart: Chart
  /** The extension sent more series or points than Flint draws. */
  truncated: boolean
  /** In a sidebar panel the chart is short; in a tab it takes more room. */
  compact: boolean
}

/**
 * A chart view of an extension: lines, areas or bars as plain SVG in the
 * theme's colors (the extension can't set one), with a legend and the same
 * values as a table for keyboards and screen readers.
 */
export function ViewChart({ chart, truncated, compact }: Props) {
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const measure = () => setWidth(Math.floor(el.clientWidth))
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const data = chart as unknown as ChartData
  const height = compact ? 180 : Math.max(220, Math.min(420, Math.round(width * 0.45)))
  const g = useMemo(() => (width > 40 ? chartGeometry(data, width, height) : null), [data, width, height])
  const table = useMemo(() => chartRows(data), [data])
  const title = chart.title ?? ''

  return (
    <figure className={`ext-chart${compact ? ' compact' : ''}`}>
      {title && <figcaption className="ext-chart-title">{title}</figcaption>}
      <div ref={box} className="ext-chart-box">
        {g && (
          <svg width={g.width} height={g.height} viewBox={`0 0 ${g.width} ${g.height}`} role="img" aria-label={chartSummary(data, title)}>
            {g.yTicks.map((t, i) => (
              <g key={i}>
                <line className="ext-chart-grid" x1={g.plot.left} x2={g.plot.right} y1={t.at} y2={t.at} />
                <text className="ext-chart-tick" x={g.plot.left - 6} y={t.at + 3} textAnchor="end">
                  {t.label}
                </text>
              </g>
            ))}
            <line className="ext-chart-axis" x1={g.plot.left} x2={g.plot.right} y1={g.plot.bottom} y2={g.plot.bottom} />
            {g.xTicks.map((t, i) => (
              // A label at an end of the axis stays inside the drawing.
              <text key={i} className="ext-chart-tick" x={t.at} y={g.plot.bottom + 14} textAnchor={t.at <= g.plot.left + 1 ? 'start' : t.at >= g.plot.right - 1 ? 'end' : 'middle'}>
                {t.label}
              </text>
            ))}
            {g.series.map((s, i) => (
              <g key={s.id} className={`ext-chart-series s${i % PALETTE}`}>
                {s.area && <path className="area" d={s.area} />}
                {s.line && <path className="line" d={s.line} />}
                {s.marks.map((m, j) =>
                  m.width > 0 ? (
                    <rect key={j} className="bar" x={m.x} y={m.y} width={m.width} height={m.height}>
                      <title>{m.title}</title>
                    </rect>
                  ) : (
                    <circle key={j} className="dot" cx={m.x} cy={m.y} r={3}>
                      <title>{m.title}</title>
                    </circle>
                  ),
                )}
              </g>
            ))}
            {g.empty && (
              <text className="ext-chart-tick" x={(g.plot.left + g.plot.right) / 2} y={(g.plot.top + g.plot.bottom) / 2} textAnchor="middle">
                No data
              </text>
            )}
          </svg>
        )}
      </div>
      <div className="ext-chart-legend">
        {chart.y.label && <span className="muted">{chart.y.label}</span>}
        {chart.series.map((s, i) => (
          <span key={s.id} className={`ext-chart-key s${i % PALETTE}`}>
            <i aria-hidden="true" />
            {s.label}
          </span>
        ))}
        {chart.x.label && <span className="muted ext-chart-xlabel">{chart.x.label}</span>}
      </div>
      {truncated && <div className="muted ext-chart-note">Only the first 10 series of 2,000 points each are drawn.</div>}
      <details className="ext-chart-data">
        <summary>Values</summary>
        <div className="ext-chart-data-scroll">
          <table>
            <thead>
              <tr>
                {table.header.map((h, i) => (
                  <th key={i} scope="col">{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {table.rows.map((r, i) => (
                <tr key={i}>
                  {r.map((c, j) => (j === 0 ? <th key={j} scope="row">{c}</th> : <td key={j}>{c}</td>))}
                </tr>
              ))}
            </tbody>
          </table>
          {table.more > 0 && <div className="muted">… and {table.more.toLocaleString('en-US')} more</div>}
        </div>
      </details>
    </figure>
  )
}
