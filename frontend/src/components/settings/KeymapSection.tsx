import { useEffect, useMemo, useState } from 'react'
import {
  canonicalKeys,
  formatKeys,
  isOverridden,
  isRebindable,
  keyConflicts,
  keysFromEvent,
  listCommands,
  setKeyCapture,
  shownKeys,
  withOverride,
  type Command,
} from '../../lib/commands'
import { useSettings } from '../../store/settings'
import { isMac } from '../../lib/platform'
import { Button, IconButton, Input, Kbd, ViewToolbar } from '@flint-ide/ui'
import { RotateCcw, X } from '@flint-ide/ui/icons'

/** Every command with its shortcut: search, record a new chord, reset or remove it. */
export function KeymapSection({ active }: { active: boolean }) {
  const keymap = useSettings((s) => s.settings.keymap)
  const update = useSettings((s) => s.update)
  const [query, setQuery] = useState('')
  const [recording, setRecording] = useState<string | null>(null)
  const [notice, setNotice] = useState<{ id: string; text: string } | null>(null)

  // The registry has no change events; it is read again whenever the keymap changes.
  const commands = useMemo(() => sortCommands(listCommands()), [keymap]) // eslint-disable-line react-hooks/exhaustive-deps
  const byKeys = useMemo(() => groupByKeys(commands, keymap), [commands, keymap])
  const rows = useMemo(() => filterCommands(commands, keymap, query), [commands, keymap, query])

  const setKeys = (cmd: Command, keys: string | null) => {
    const next = withOverride(keymap, cmd, keys)
    update({ keymap: next })
    const clash = keys ? keyConflicts(keys, cmd.id, commands, next) : []
    setNotice(clash.length ? { id: cmd.id, text: `${safeFormat(keys!)} is also bound to ${clash.map((c) => c.title).join(', ')}` } : null)
  }

  // While recording, keydowns go here instead of running commands (lib/commands.ts).
  useEffect(() => {
    if (!recording || !active) return
    const cmd = commands.find((c) => c.id === recording)
    setKeyCapture((e) => {
      const plain = !e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey
      if (e.key === 'Escape' && plain) return setRecording(null)
      const keys = keysFromEvent(e)
      if (!keys || !cmd) return
      setRecording(null)
      setKeys(cmd, keys)
    })
    return () => setKeyCapture(null)
    // setKeys reads the current keymap through the render that started recording.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recording, active, commands, keymap])

  useEffect(() => {
    if (!active) setRecording(null)
  }, [active])

  const overrides = Object.keys(keymap).length
  let category: string | undefined
  return (
    <section className="keymap">
      <h2>Keymap</h2>
      <ViewToolbar aria-label="Keymap">
        <Input
          className="settings-text"
          placeholder={`Search actions or shortcuts (e.g. push, ${formatKeys('Mod+K')})…`}
          value={query}
          spellCheck={false}
          onChange={(e) => setQuery(e.target.value)}
        />
        <Button variant="plain" disabled={overrides === 0} onClick={() => update({ keymap: {} })} title="Restore every default shortcut">
          Reset All{overrides ? ` (${overrides})` : ''}
        </Button>
      </ViewToolbar>
      <p className="muted settings-note">
        Click a shortcut and press the new keys; Esc cancels. {isMac() ? 'Keys of the macOS menu (⌘Q, ⌘C, ⌘V, ⌘O, ⌘,) stay with the menu.' : 'Ctrl+C, Ctrl+V, Ctrl+X, Ctrl+Z and Ctrl+A stay with text fields, and plain Ctrl+letter keys in a terminal go to the shell.'}
      </p>
      <div className="keymap-list">
        {rows.map((cmd) => {
          const header = cmd.category !== category ? (category = cmd.category) : undefined
          const keys = shownKeys(cmd, keymap)
          const shared = keys ? (byKeys.get(canonical(keys)) ?? []).filter((c) => c.id !== cmd.id) : []
          const changed = isOverridden(cmd, keymap)
          return (
            <div key={cmd.id}>
              {header !== undefined && <div className="keymap-category">{header || 'Other'}</div>}
              <div className={`keymap-row${changed ? ' changed' : ''}`}>
                <span className="keymap-title" title={cmd.id}>{cmd.title}</span>
                {isRebindable(cmd) ? (
                  <button
                    className={`keymap-chord${recording === cmd.id ? ' recording' : ''}`}
                    title="Click, then press the new shortcut"
                    onClick={() => setRecording(recording === cmd.id ? null : cmd.id)}
                    onBlur={() => setRecording((r) => (r === cmd.id ? null : r))}
                  >
                    {recording === cmd.id ? 'Press keys…' : keys ? <Kbd>{safeFormat(keys)}</Kbd> : <span className="muted">—</span>}
                  </button>
                ) : (
                  <span className="keymap-chord fixed" title="Handled by the editor; it can't be changed here">
                    {keys && <Kbd>{safeFormat(keys)}</Kbd>} <span className="muted">editor</span>
                  </span>
                )}
                <span className="keymap-actions">
                  {changed && (
                    <IconButton
                      variant="ghost"
                      size="sm"
                      aria-label="Reset the shortcut"
                      title={`Reset to ${cmd.keys ? safeFormat(cmd.keys) : 'no shortcut'}`}
                      onClick={() => setKeys(cmd, null)}
                    >
                      <RotateCcw />
                    </IconButton>
                  )}
                  {isRebindable(cmd) && keys && (
                    <IconButton variant="ghost" size="sm" aria-label="Remove the shortcut" title="Remove the shortcut" onClick={() => setKeys(cmd, '')}>
                      <X />
                    </IconButton>
                  )}
                </span>
                {notice?.id === cmd.id ? (
                  <span className="keymap-shared warn">{notice.text}</span>
                ) : (
                  shared.length > 0 && <span className="keymap-shared muted">Also: {shared.map((c) => c.title).join(', ')}</span>
                )}
              </div>
            </div>
          )
        })}
        {rows.length === 0 && <div className="placeholder muted">No matching actions</div>}
      </div>
    </section>
  )
}

function sortCommands(list: Command[]): Command[] {
  return [...list].sort((a, b) => (a.category ?? '').localeCompare(b.category ?? '') || a.title.localeCompare(b.title))
}

function canonical(keys: string): string {
  try {
    return canonicalKeys(keys)
  } catch {
    return keys
  }
}

function safeFormat(keys: string): string {
  try {
    return formatKeys(keys)
  } catch {
    return keys
  }
}

/** Commands by the shortcut they show, to point out shared keys. */
function groupByKeys(list: Command[], keymap: Record<string, string>): Map<string, Command[]> {
  const out = new Map<string, Command[]>()
  for (const cmd of list) {
    const keys = shownKeys(cmd, keymap)
    if (!keys) continue
    const k = canonical(keys)
    out.set(k, [...(out.get(k) ?? []), cmd])
  }
  return out
}

/** Every word of the query must appear in the title, category, id, keywords or shortcut (⌘K or mod+k). */
function filterCommands(list: Command[], keymap: Record<string, string>, query: string): Command[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return list
  return list.filter((cmd) => {
    const keys = shownKeys(cmd, keymap)
    const text = [cmd.title, cmd.category, cmd.id, cmd.keywords, keys, keys && safeFormat(keys)].join(' ').toLowerCase()
    return words.every((w) => text.includes(w))
  })
}
