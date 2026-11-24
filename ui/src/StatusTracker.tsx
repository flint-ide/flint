import { useRef, useState, type KeyboardEvent } from 'react'
import { rovingIndex, trackerSlots, trackerSummary, type TrackerItem, type TrackerSlot, type TrackerState } from './statusTrackerMath'
import { classNames } from './treeIndent'

export type StatusTrackerItem = TrackerItem
export type StatusTrackerState = TrackerState

export type StatusTrackerProps = {
  /** The runs, oldest first. */
  items: readonly StatusTrackerItem[]
  /** What the strip is about ("Nightly build"); read out with the counts. */
  label: string
  /** How many blocks the strip has; fewer items are padded on the left. Default 30. */
  max?: number
  size?: 'sm' | 'md'
  /** Makes every real block a button. */
  onSelect?: (item: StatusTrackerItem) => void
  className?: string
}

const STATE_WORD: Record<TrackerState, string> = {
  ok: 'Passed',
  failed: 'Failed',
  warn: 'Warning',
  running: 'Running',
  skipped: 'Skipped',
  none: 'No run',
}

const blockTitle = (slot: TrackerSlot): string => slot.title ?? STATE_WORD[slot.state]

/** A strip of small blocks, one per run, oldest left: the history of a workflow's or CI job's last runs. */
export function StatusTracker({ items, label, max = 30, size = 'sm', onSelect, className }: StatusTrackerProps) {
  const slots = trackerSlots(items, max)
  const summary = `${label}. ${trackerSummary(items, max)}`
  const cls = classNames('fui-status-tracker', className)
  if (!onSelect) {
    return (
      <span className={cls} data-size={size} role="img" aria-label={summary}>
        {slots.map((slot) => (
          <span key={slot.id} className="fui-status-block" data-state={slot.state} title={slot.placeholder ? undefined : blockTitle(slot)} />
        ))}
      </span>
    )
  }
  return <SelectableStrip slots={slots} summary={summary} size={size} onSelect={onSelect} className={cls} />
}

type StripProps = { slots: TrackerSlot[]; summary: string; size: 'sm' | 'md'; onSelect: (item: StatusTrackerItem) => void; className: string }

/** The strip with buttons: one tab stop, arrow keys move between the real blocks. */
function SelectableStrip({ slots, summary, size, onSelect, className }: StripProps) {
  const real = slots.filter((s) => !s.placeholder)
  const buttons = useRef<(HTMLButtonElement | null)[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const found = real.findIndex((s) => s.id === activeId)
  const current = found >= 0 ? found : real.length - 1

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    const next = rovingIndex(current, event.key, real.length)
    if (next === null) return
    event.preventDefault()
    setActiveId(real[next].id)
    buttons.current[next]?.focus()
  }

  return (
    <span className={className} data-size={size} role="group" aria-label={summary} onKeyDown={onKeyDown}>
      {slots.map((slot) => {
        if (slot.placeholder) return <span key={slot.id} className="fui-status-block" data-state="none" />
        const index = real.indexOf(slot)
        const title = blockTitle(slot)
        return (
          <button
            key={slot.id}
            ref={(el) => {
              buttons.current[index] = el
            }}
            type="button"
            className="fui-status-block"
            data-state={slot.state}
            title={title}
            aria-label={slot.title ? `${slot.title}, ${STATE_WORD[slot.state].toLowerCase()}` : title}
            tabIndex={index === current ? 0 : -1}
            onFocus={() => setActiveId(slot.id)}
            onClick={() => onSelect({ id: slot.id, state: slot.state, title: slot.title })}
          />
        )
      })}
    </span>
  )
}
