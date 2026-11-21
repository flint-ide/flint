import { Input } from '@wunderui/react'
import { type KeyboardEvent, useMemo, useState } from 'react'
import { filterByLabel, moveIndex } from './filter'
import { Keys } from './Keys'

export type PaletteCommand = {
  id: string
  label: string
  /** Shown dimmed on the right, e.g. the command's category. */
  detail?: string
  keys?: readonly string[]
}

export type CommandPaletteProps = {
  commands: readonly PaletteCommand[]
  onRun: (command: PaletteCommand) => void
  onClose?: () => void
  placeholder?: string
  emptyText?: string
}

/** A filterable command list driven by the keyboard (↑ ↓ ↵ Esc). */
export function CommandPalette({ commands, onRun, onClose, placeholder = 'Type a command…', emptyText = 'No matching commands' }: CommandPaletteProps) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const matches = useMemo(() => filterByLabel(commands, query), [commands, query])

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => moveIndex(i, e.key === 'ArrowDown' ? 1 : -1, matches.length))
    } else if (e.key === 'Enter' && matches[active]) {
      onRun(matches[active])
    } else if (e.key === 'Escape') {
      onClose?.()
    }
  }

  return (
    <div className="fui-palette">
      <div className="fui-palette-search">
        <Input
          autoFocus
          placeholder={placeholder}
          value={query}
          aria-label={placeholder}
          aria-controls="fui-palette-list"
          onChange={(e) => {
            setQuery(e.target.value)
            setActive(0)
          }}
          onKeyDown={onKeyDown}
        />
      </div>
      <ul id="fui-palette-list" role="listbox" className="fui-palette-list">
        {matches.map((c, i) => (
          <li
            key={c.id}
            role="option"
            aria-selected={i === active}
            className="fui-palette-item"
            onMouseEnter={() => setActive(i)}
            onClick={() => onRun(c)}
          >
            <span className="fui-grow">{c.label}</span>
            {c.detail && <span className="fui-muted">{c.detail}</span>}
            <span className="fui-palette-keys">{c.keys && <Keys keys={c.keys} />}</span>
          </li>
        ))}
        {matches.length === 0 && <li className="fui-palette-empty">{emptyText}</li>}
      </ul>
    </div>
  )
}
