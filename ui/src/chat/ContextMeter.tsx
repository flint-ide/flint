import { Popover, PopoverContent, PopoverTrigger, Tooltip, TooltipContent, TooltipTrigger } from '@wunderui/react'
import type { CSSProperties } from 'react'
import { classNames } from '../treeIndent'
import {
  type ContextPart,
  contextPercent,
  formatShare,
  formatTokens,
  hasLimit,
  PART_COLORS,
  type UsageBreakdown,
  usageBreakdown,
  ringGeometry,
  usageText,
} from './contextMeterMath'
import { contextLevel } from './status'

export type ContextMeterProps = {
  /** Tokens in the context window now. */
  used: number
  /** The window's size in tokens; null or 0 when the agent doesn't report it (nothing is rendered). */
  limit: number | null | undefined
  /** What the tokens are made of (System prompt, Tools, Files, Conversation), in order. */
  parts?: readonly ContextPart[]
  /** `ring`: a small gauge for a toolbar, `bar`: a stacked bar with a legend. */
  variant?: 'ring' | 'bar'
  /** The ring's diameter in px. */
  size?: number
  /** The accessible name. */
  label?: string
  className?: string
}

const RING_STROKE_RATIO = 8

/** How full the model's context window is: a ring for the prompt toolbar, or a stacked bar with a legend. */
export function ContextMeter({ used, limit, parts, variant = 'ring', size = 16, label = 'Context used', className }: ContextMeterProps) {
  if (!hasLimit(limit)) return null
  if (variant === 'bar') return <ContextBar used={used} limit={limit} parts={parts} label={label} className={className} />
  return <ContextRing used={used} limit={limit} parts={parts} size={size} label={label} className={className} />
}

type Resolved = { label: string; className?: string; parts?: readonly ContextPart[]; used: number; limit: number }

function meterProps(used: number, limit: number, label: string) {
  const percent = contextPercent(used, limit)
  return {
    percent,
    level: contextLevel(percent),
    aria: {
      'aria-label': label,
      'aria-valuemin': 0,
      'aria-valuemax': 100,
      'aria-valuenow': percent,
      'aria-valuetext': usageText(used, limit),
    },
  }
}

function ContextRing({ used, limit, parts, size, label, className }: Resolved & { size: number }) {
  const { percent, level, aria } = meterProps(used, limit, label)
  const stroke = Math.max(1.5, size / RING_STROKE_RATIO)
  const ring = ringGeometry(size, percent, stroke)
  const circle = (arc?: string) => (
    <circle cx={ring.center} cy={ring.center} r={ring.radius} fill="none" strokeWidth={stroke} strokeLinecap="round" strokeDasharray={arc} />
  )
  const svg = (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true" focusable="false">
      <g className="fui-ctx-track">{circle()}</g>
      <g className="fui-ctx-arc">{percent > 0 && circle(ring.dashArray)}</g>
    </svg>
  )
  const classes = classNames('fui-ctx-ring', className)
  const summary = usageText(used, limit, ' · ')
  if (parts && parts.length > 0) {
    const trigger = <button type="button" className={classes} data-level={level} aria-label={`${label}: ${aria['aria-valuetext']}`} />
    return (
      <Popover>
        <Tooltip>
          <TooltipTrigger describe={false} render={<PopoverTrigger render={trigger}>{svg}</PopoverTrigger>} />
          <TooltipContent side="top">{summary}</TooltipContent>
        </Tooltip>
        <PopoverContent side="top" align="end" className="fui-ctx-popover">
          <ContextBar used={used} limit={limit} parts={parts} label={label} />
        </PopoverContent>
      </Popover>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger describe={false} render={<span role="meter" tabIndex={0} className={classes} data-level={level} {...aria} />}>{svg}</TooltipTrigger>
      <TooltipContent side="top">{summary}</TooltipContent>
    </Tooltip>
  )
}

function segmentStyle(share: number): CSSProperties {
  return { width: `${share}%` }
}

function ContextBar({ used, limit, parts, label, className }: Resolved) {
  const { percent, level, aria } = meterProps(used, limit, label)
  const breakdown = usageBreakdown(parts ?? [], used, limit)
  const { shares } = breakdown
  return (
    <div className={classNames('fui-ctx-bar', className)} data-level={level}>
      <div className="fui-ctx-bar-track" role="meter" {...aria}>
        {shares.length > 0 ? (
          shares.map((s, i) => <span key={s.id} className="fui-ctx-seg" data-part-index={i % PART_COLORS} style={segmentStyle(s.share)} />)
        ) : (
          <span className="fui-ctx-seg fui-ctx-seg-total" style={segmentStyle(percent)} />
        )}
        {shares.length > 0 && breakdown.other > 0 && <span className="fui-ctx-seg fui-ctx-seg-other" style={segmentStyle(breakdown.other)} />}
      </div>
      {shares.length > 0 && <ContextLegend breakdown={breakdown} limit={limit} />}
    </div>
  )
}

function ContextLegend({ breakdown, limit }: { breakdown: UsageBreakdown; limit: number }) {
  const { shares, free } = breakdown
  return (
    <ul className="fui-ctx-legend">
      {shares.map((s, i) => (
        <li key={s.id}>
          <span className="fui-ctx-dot" data-part-index={i % PART_COLORS} aria-hidden="true" />
          <span className="fui-ctx-name fui-ellipsis">{s.label}</span>
          <span className="fui-ctx-count">{formatTokens(s.tokens)}</span>
          <span className="fui-ctx-share">{formatShare(s.share)}</span>
        </li>
      ))}
      {breakdown.other > 0 && (
        <li>
          <span className="fui-ctx-dot fui-ctx-dot-other" aria-hidden="true" />
          <span className="fui-ctx-name fui-ellipsis">Other</span>
          <span className="fui-ctx-count">{formatTokens(breakdown.otherTokens)}</span>
          <span className="fui-ctx-share">{formatShare(breakdown.other)}</span>
        </li>
      )}
      <li>
        <span className="fui-ctx-dot fui-ctx-dot-free" aria-hidden="true" />
        <span className="fui-ctx-name fui-ellipsis">Free</span>
        <span className="fui-ctx-count">{formatTokens((limit * free) / 100)}</span>
        <span className="fui-ctx-share">{formatShare(free)}</span>
      </li>
    </ul>
  )
}
