import { memo, useEffect, useMemo, useState } from 'react'
import { formatKeys, isAvailable, listCommands, runCommand, shownKeys, type Command } from '../lib/commands'
import { pushRecent, rankCommands } from '../lib/commandSearch'
import { useWorkspace } from '../store/workspace'
import { Modal } from './layout/Modal'
import { useListCursor } from '../lib/useListCursor'
import { useEventCallback } from '../lib/useEventCallback'

const RECENT_KEY = 'flint.recentCommands'

function loadRecent(): string[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]')
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string') : []
  } catch {
    return []
  }
}

function saveRecent(ids: string[]): void {
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(ids))
  } catch {
    // Storage blocked: recents just aren't remembered.
  }
}

/** ⇧⌘P: IntelliJ-style "Find Action" over the command registry. */
export function CommandPalette() {
  // Snapshot on open: availability (when) is judged in the context the palette was opened from.
  const [commands] = useState(() => listCommands().filter(isAvailable))
  const [recent] = useState(loadRecent)
  // Where focus goes back to (a terminal, an editor) unless the command moves it.
  const [previousFocus] = useState(() => document.activeElement)
  const [query, setQuery] = useState('')
  const { listRef, cursor, setCursor, hover } = useListCursor()

  const rows = useMemo(() => rankCommands(commands, query, recent), [commands, query, recent])

  useEffect(() => setCursor(0), [query, setCursor])

  const close = () => {
    useWorkspace.getState().setPaletteOpen(false)
    if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus()
  }

  const choose = useEventCallback((cmd: Command | undefined) => {
    if (!cmd) return
    close()
    saveRecent(pushRecent(recent, cmd.id))
    runCommand(cmd)
  })

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n')) {
      setCursor((c) => Math.min(c + 1, rows.length - 1))
    } else if (e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p')) {
      setCursor((c) => Math.max(c - 1, 0))
    } else if (e.key === 'Enter') {
      choose(rows[cursor]?.item)
    } else {
      return
    }
    e.preventDefault()
  }

  const firstRest = query.trim() ? -1 : rows.findIndex((r) => !recent.includes(r.item.id))

  return (
    <Modal onClose={close} className="quick-open command-palette">
      <input
        autoFocus
        className="quick-input"
        placeholder="Find action…"
        value={query}
        spellCheck={false}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={onKeyDown}
      />
      <div className="quick-list" ref={listRef}>
        {rows.map(({ item, positions }, i) => (
          <CommandRow
            key={item.id}
            command={item}
            positions={positions}
            index={i}
            selected={i === cursor}
            afterRecent={i === firstRest && i > 0}
            onHover={hover}
            onPick={choose}
          />
        ))}
        {rows.length === 0 && <div className="placeholder muted">No matching actions</div>}
      </div>
    </Modal>
  )
}

/** One action; memoized so moving the highlight re-renders only two rows. */
const CommandRow = memo(function CommandRow({ command, positions, index, selected, afterRecent, onHover, onPick }: {
  command: Command
  positions: number[]
  index: number
  selected: boolean
  afterRecent: boolean
  onHover: (i: number) => void
  onPick: (cmd: Command) => void
}) {
  const keys = shownKeys(command) // the user's keymap, else the default
  return (
    <div
      className={`quick-item${selected ? ' selected' : ''}${afterRecent ? ' after-recent' : ''}`}
      onMouseMove={() => onHover(index)}
      onClick={() => onPick(command)}
    >
      <span className="file-name">
        <Highlighted text={command.title} positions={positions} />
      </span>
      {command.category && <span className="muted path-hint">{command.category}</span>}
      {keys && <kbd>{formatKeys(keys)}</kbd>}
    </div>
  )
})

/** The title with matched letters in bold (rendered as text, no HTML injection). */
function Highlighted({ text, positions }: { text: string; positions: number[] }) {
  if (!positions.length) return <>{text}</>
  const hit = new Set(positions)
  const parts: { text: string; bold: boolean }[] = []
  for (let i = 0; i < text.length; i++) {
    const bold = hit.has(i)
    const last = parts[parts.length - 1]
    if (last && last.bold === bold) parts[parts.length - 1] = { text: last.text + text[i], bold }
    else parts.push({ text: text[i], bold })
  }
  return <>{parts.map((p, i) => (p.bold ? <b key={i}>{p.text}</b> : <span key={i}>{p.text}</span>))}</>
}
