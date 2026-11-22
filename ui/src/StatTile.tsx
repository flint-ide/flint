import { Card, Meter, TrendChip } from '@wunderui/react'
import type { ComponentProps, ReactNode } from 'react'
import { classNames } from './treeIndent'

export type StatTileTrend = Pick<ComponentProps<typeof TrendChip>, 'value' | 'direction' | 'sentiment'>

export type StatTileProps = {
  label: ReactNode
  /** The figure itself; one line, ellipsised when the tile is narrow. */
  value: ReactNode
  /** Dimmed line under the value: a unit, a period, a second figure. */
  sub?: ReactNode
  /** A change against an earlier period, shown as a TrendChip beside the label. */
  trend?: StatTileTrend
  /** A bar under the text: a value in 0..max (a budget or quota used). */
  meter?: { value: number; max?: number; label: string }
  /** Native tooltip with the breakdown. */
  title?: string
  className?: string
}

/** A small card with one figure: label, value, sub-text and optionally a trend or a meter. */
export function StatTile({ label, value, sub, trend, meter, title, className }: StatTileProps) {
  return (
    <Card size="sm" className={classNames('fui-stat-tile', className)} title={title}>
      <div className="fui-stat-head">
        <span className="fui-stat-label">{label}</span>
        {trend && <TrendChip {...trend} />}
      </div>
      <strong className="fui-stat-value">{value}</strong>
      {sub != null && <span className="fui-stat-sub">{sub}</span>}
      {meter && <Meter className="fui-meter-thin" value={meter.value} max={meter.max ?? 100} aria-label={meter.label} />}
    </Card>
  )
}

/** Tiles side by side, equal width. */
export function StatTiles({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={classNames('fui-stat-tiles', className)}>{children}</div>
}
