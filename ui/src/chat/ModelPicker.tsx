import { Button, Input, Popover, PopoverContent, PopoverTrigger, Segment } from '@wunderui/react'
import { Check, ChevronDown } from 'lucide-react'
import { type KeyboardEvent, useEffect, useId, useMemo, useRef, useState } from 'react'
import { classNames } from '../treeIndent'
import {
  filterModels,
  flattenGroups,
  formatContextWindow,
  groupModels,
  type ModelOption,
  nextEnabledIndex,
  triggerAccessibleName,
  triggerText,
} from './modelPickerLogic'

export type ModelPickerProps = {
  models: readonly ModelOption[]
  /** The current model's id. An id that is not in `models` shows as it is. */
  value: string
  onChange: (id: string) => void
  /** The current reasoning effort, shown after the model's name. */
  effort?: string
  onEffortChange?: (effort: string) => void
  /** Dimmed and closed, e.g. while a turn is running. */
  disabled?: boolean
  /** A filter field above the list. Default: only when there are more than 8 models. */
  searchable?: boolean
  size?: 'sm' | 'md'
  className?: string
}

const SEARCH_ABOVE = 8

/**
 * The model chip of a prompt box: shows the model (and effort), and opens a popover to
 * switch model and reasoning effort. Up/Down/Home/End move over the enabled rows, Enter
 * picks, Escape closes and gives the focus back to the chip; with a filter field, typing filters.
 */
export function ModelPicker({ models, value, onChange, effort, onEffortChange, disabled, searchable, size = 'sm', className }: ModelPickerProps) {
  const [open, setOpen] = useState(false)
  const focusRef = useRef<HTMLElement | null>(null)
  if (disabled && open) setOpen(false)
  const showSearch = searchable ?? models.length > SEARCH_ABOVE
  const pick = (id: string) => {
    onChange(id)
    setOpen(false)
  }
  return (
    <Popover open={open} onOpenChange={(next) => setOpen(next && !disabled)}>
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size={size}
            disabled={disabled}
            aria-label={triggerAccessibleName(models, value, effort)}
            className={classNames('fui-model-trigger', className)}
          />
        }
      >
        <span className="fui-model-trigger-text">{triggerText(models, value, effort)}</span>
        <ChevronDown data-icon="inline-end" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" side="top" className="fui-model-popup" initialFocus={focusRef}>
        <ModelPanel
          models={models}
          value={value}
          onPick={pick}
          effort={effort}
          onEffortChange={onEffortChange}
          searchable={showSearch}
          focusRef={focusRef}
        />
      </PopoverContent>
    </Popover>
  )
}

type PanelProps = {
  models: readonly ModelOption[]
  value: string
  onPick: (id: string) => void
  effort?: string
  onEffortChange?: (effort: string) => void
  searchable: boolean
  /** Where Base UI puts the focus when the popup opens: the filter field, or the list. */
  focusRef: { current: HTMLElement | null }
}

function ModelPanel({ models, value, onPick, effort, onEffortChange, searchable, focusRef }: PanelProps) {
  const uid = useId()
  const [query, setQuery] = useState('')
  const groups = useMemo(() => groupModels(filterModels(models, query)), [models, query])
  const flat = useMemo(() => flattenGroups(groups), [groups])
  // The cursor is a model id, so it follows the model when the list changes under it.
  const [cursor, setCursor] = useState<string | null>(null)
  const found = flat.findIndex((m) => m.id === cursor && !m.disabled)
  const active = found >= 0 ? found : initialActive(flat, value)
  const activeId = flat[active] ? `${uid}-opt-${active}` : undefined

  useEffect(() => {
    if (activeId) document.getElementById(activeId)?.scrollIntoView?.({ block: 'nearest' })
  }, [activeId])

  const onKeys = (e: KeyboardEvent, homeEnd: boolean) => {
    const next = movedIndex(e.key, flat, active, homeEnd)
    if (next !== undefined) {
      e.preventDefault()
      if (flat[next]) setCursor(flat[next].id)
    } else if (e.key === 'Enter' && flat[active]) {
      e.preventDefault()
      onPick(flat[active].id)
    }
  }
  const current = models.find((m) => m.id === value)

  return (
    <div className="fui-model-panel">
      {searchable && (
        <div className="fui-model-search">
          <Input
            ref={(el) => {
              focusRef.current = el
            }}
            size="sm"
            role="combobox"
            aria-expanded
            aria-label="Filter models"
            aria-controls={`${uid}-list`}
            aria-activedescendant={activeId}
            placeholder="Filter models…"
            value={query}
            onKeyDown={(e) => onKeys(e, false)}
            onChange={(e) => {
              setQuery(e.target.value)
              setCursor(null)
            }}
          />
        </div>
      )}
      <div
        ref={(el) => {
          if (!searchable) focusRef.current = el
        }}
        onKeyDown={(e) => onKeys(e, true)}
        id={`${uid}-list`}
        role="listbox"
        aria-label="Models"
        tabIndex={searchable ? -1 : 0}
        aria-activedescendant={searchable ? undefined : activeId}
        className="fui-model-list"
      >
        {groups.map((g, gi) => (
          <ModelGroupRows key={g.group ?? `#${gi}`} uid={uid} groupIndex={gi} group={g} flat={flat} value={value} active={active} onActive={setCursor} onPick={onPick} />
        ))}
        {flat.length === 0 && <div className="fui-model-empty">No matching models</div>}
      </div>
      {current?.efforts && current.efforts.length > 0 && onEffortChange && (
        <EffortControl efforts={current.efforts} effort={effort} onChange={onEffortChange} />
      )}
    </div>
  )
}

function initialActive(flat: readonly ModelOption[], value: string): number {
  const at = flat.findIndex((m) => m.id === value && !m.disabled)
  return at >= 0 ? at : nextEnabledIndex(flat, -1, 1)
}

/** The index a navigation key moves to, or undefined when the key is not one. */
function movedIndex(key: string, flat: readonly ModelOption[], active: number, homeEnd: boolean): number | undefined {
  if (key === 'ArrowDown') return nextEnabledIndex(flat, active, 1)
  if (key === 'ArrowUp') return nextEnabledIndex(flat, active < 0 ? flat.length : active, -1)
  if (key === 'Home' && homeEnd) return nextEnabledIndex(flat, -1, 1)
  if (key === 'End' && homeEnd) return nextEnabledIndex(flat, flat.length, -1)
  return undefined
}

type RowsProps = {
  uid: string
  groupIndex: number
  group: { group: string | undefined; models: readonly ModelOption[] }
  flat: readonly ModelOption[]
  value: string
  active: number
  onActive: (id: string) => void
  onPick: (id: string) => void
}

function ModelGroupRows({ uid, groupIndex, group, flat, value, active, onActive, onPick }: RowsProps) {
  const headerId = `${uid}-g${groupIndex}`
  const rows = group.models.map((m) => {
    const index = flat.indexOf(m)
    return (
      <ModelRow
        key={m.id}
        id={`${uid}-opt-${index}`}
        model={m}
        selected={m.id === value}
        active={index === active}
        onActive={() => onActive(m.id)}
        onPick={() => onPick(m.id)}
      />
    )
  })
  if (group.group === undefined) return <>{rows}</>
  return (
    <div role="group" aria-labelledby={headerId}>
      <div id={headerId} role="presentation" className="fui-model-group-title">
        {group.group}
      </div>
      {rows}
    </div>
  )
}

type RowProps = { id: string; model: ModelOption; selected: boolean; active: boolean; onActive: () => void; onPick: () => void }

function ModelRow({ id, model, selected, active, onActive, onPick }: RowProps) {
  return (
    <div
      id={id}
      role="option"
      aria-selected={selected}
      aria-disabled={model.disabled || undefined}
      data-active={active || undefined}
      title={model.disabled ? model.disabledReason : undefined}
      className="fui-model-row"
      onMouseMove={model.disabled ? undefined : onActive}
      onClick={model.disabled ? undefined : onPick}
    >
      <span className="fui-model-check" aria-hidden>
        {selected && <Check />}
      </span>
      <span className="fui-model-name">{model.label}</span>
      {model.description && <span className="fui-model-desc">{model.description}</span>}
      <span className="fui-model-meta">
        {model.contextWindow !== undefined && <span>{formatContextWindow(model.contextWindow)}</span>}
        {model.price && <span>{model.price}</span>}
      </span>
    </div>
  )
}

type EffortProps = { efforts: readonly string[]; effort?: string; onChange: (effort: string) => void }

function EffortControl({ efforts, effort, onChange }: EffortProps) {
  return (
    <div className="fui-model-effort">
      <span className="fui-model-effort-label">Effort</span>
      <Segment
        size="xs"
        shape="rounded"
        aria-label="Reasoning effort"
        options={efforts.map((e) => ({ value: e, label: e }))}
        value={effort && efforts.includes(effort) ? effort : ''}
        onValueChange={onChange}
      />
    </div>
  )
}
