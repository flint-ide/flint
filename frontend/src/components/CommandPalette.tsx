import { useEffect, useMemo, useRef, useState } from 'react'
import { formatKeys, isAvailable, listCommands, runCommand, type Command } from '../lib/commands'
import { pushRecent, rankCommands } from '../lib/commandSearch'
import { useWorkspace } from '../store/workspace'
import { Modal } from './layout/Modal'

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
  const [cursor, setCursor] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  const rows = useMemo(() => rankCommands(commands, query, recent), [commands, query, recent])

  useEffect(() => setCursor(0), [query])
  useEffect(() => {
    list.current?.querySelector('.selected')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  const close = () => {
    useWorkspace.getState().setPaletteOpen(false)
    if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus()
  }

  const choose = (cmd: Command | undefined) => {
    if (!cmd) return
    close()
    saveRecent(pushRecent(recent, cmd.id))
    runCommand(cmd)
  }

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
      <div className="quick-list" ref={list}>
        {rows.map(({ item, positions }, i) => {
          const keys = item.keys ?? item.hintKeys
          return (
            <div
              key={item.id}
              className={`quick-item${i === cursor ? ' selected' : ''}${i === firstRest && i > 0 ? ' after-recent' : ''}`}
              onMouseMove={() => setCursor(i)}
              onClick={() => choose(item)}
            >
              <span className="file-name">
                <Highlighted text={item.title} positions={positions} />
              </span>
              {item.category && <span className="muted path-hint">{item.category}</span>}
              {keys && <kbd>{formatKeys(keys)}</kbd>}
            </div>
          )
        })}
        {rows.length === 0 && <div className="placeholder muted">No matching actions</div>}
      </div>
    </Modal>
  )
}

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
