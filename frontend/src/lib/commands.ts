// Command registry: every user-facing action lives here once, with a title for the
// ⇧⌘P palette and optionally a global shortcut. One capturing keydown listener
// (handleGlobalKey, installed by App) runs commands by their `keys`, so editors and
// terminals never see a shortcut the app claimed.
//
// Adding a command from a feature:
//
//   useEffect(() => registerCommand({
//     id: 'git.blame',                 // unique, "<area>.<action>"
//     title: 'Annotate with Git Blame',
//     category: 'Git',
//     keys: 'Mod+Alt+B',               // optional global shortcut ('Mod' = ⌘)
//     when: () => useGit.getState().isRepo, // optional: hidden and not dispatched when false
//     run: () => openBlame(),
//   }), [])
//
// registerCommand returns the unregister function (handy as an effect cleanup);
// registerCommands does the same for a list. A later registration with the same id
// replaces the earlier one, and shortcuts registered later win over earlier ones,
// so a component can shadow an app-wide command while it is mounted.
// Shortcuts owned by someone else (CodeMirror keymaps) go in `hintKeys`: shown in
// the palette, never dispatched here.
// Framework-free on purpose: no React, no stores, so it is testable with node:test.

export interface Command {
  /** Unique id, "<area>.<action>" (e.g. "git.push"). */
  id: string
  /** Palette title in Title Case, e.g. "Push…" or "Toggle Terminal". */
  title: string
  /** Palette group shown next to the title (e.g. "Git", "View"). */
  category?: string
  /** Global shortcut such as 'Mod+Shift+P', run by handleGlobalKey and shown in the palette. */
  keys?: string
  /** CSS selector: the global shortcut is left to the focused element inside it (⌘K clears a terminal). */
  keysIgnoredIn?: string
  /** Shortcut handled elsewhere (e.g. a CodeMirror keymap): only displayed. */
  hintKeys?: string
  /** Extra words the palette filter matches (synonyms). */
  keywords?: string
  /** Availability: false hides it from the palette and skips its shortcut. */
  when?: () => boolean
  run: () => void | Promise<unknown>
}

/** The parts of a KeyboardEvent the dispatcher looks at (keeps it testable). */
export interface KeyEventLike {
  key: string
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
}

export interface Chord {
  key: string // lower-case KeyboardEvent.key, e.g. 'p', '[', 'enter', 'f12'
  meta: boolean
  ctrl: boolean
  alt: boolean
  shift: boolean
}

let commands = new Map<string, Command>()
let reportError: (err: unknown) => void = (err) => {
  setTimeout(() => {
    throw err
  })
}

/** Adds (or replaces) a command; the returned function removes exactly this registration. */
export function registerCommand(cmd: Command): () => void {
  commands = new Map(commands).set(cmd.id, cmd)
  return () => {
    if (commands.get(cmd.id) !== cmd) return
    const next = new Map(commands)
    next.delete(cmd.id)
    commands = next
  }
}

export function registerCommands(list: readonly Command[]): () => void {
  const offs = list.map(registerCommand)
  return () => offs.forEach((off) => off())
}

/** All registered commands in registration order, including unavailable ones. */
export function listCommands(): Command[] {
  return [...commands.values()]
}

export function getCommand(id: string): Command | undefined {
  return commands.get(id)
}

export function isAvailable(cmd: Command): boolean {
  try {
    return !cmd.when || cmd.when()
  } catch {
    return false
  }
}

/** Where failures of async commands go (App routes them to a toast). */
export function setCommandErrorHandler(handler: (err: unknown) => void): void {
  reportError = handler
}

/** Runs a command, reporting sync throws and rejected promises instead of losing them. */
export function runCommand(cmd: Command): void {
  try {
    const result = cmd.run()
    if (result instanceof Promise) result.catch(reportError)
  } catch (err) {
    reportError(err)
  }
}

const MODIFIERS: Record<string, keyof Omit<Chord, 'key'>> = {
  mod: 'meta', cmd: 'meta', meta: 'meta', ctrl: 'ctrl', control: 'ctrl', alt: 'alt', option: 'alt', shift: 'shift',
}

/** 'Mod+Shift+P' → { key: 'p', meta: true, shift: true, … }. 'Mod' is ⌘ (Flint is macOS-only). */
export function parseKeys(keys: string): Chord {
  const parts = keys.split('+')
  // 'Mod++' names the plus key itself.
  const key = keys.endsWith('++') ? '+' : parts[parts.length - 1]
  const mods = keys.endsWith('++') ? parts.slice(0, -2) : parts.slice(0, -1)
  const chord: Chord = { key: key.toLowerCase(), meta: false, ctrl: false, alt: false, shift: false }
  for (const m of mods) {
    const flag = MODIFIERS[m.toLowerCase()]
    if (!flag) throw new Error(`Unknown modifier "${m}" in shortcut "${keys}"`)
    chord[flag] = true
  }
  return chord
}

// Every keystroke in an editor passes through the dispatcher: parse each shortcut once.
const parsed = new Map<string, Chord>()
const chordFor = (keys: string): Chord => {
  const hit = parsed.get(keys)
  if (hit) return hit
  const c = parseKeys(keys)
  parsed.set(keys, c)
  return c
}

/** Exact match: every modifier must agree, so ⌘P never fires on ⇧⌘P or ⌥⌘P. */
export function matchesKeys(e: KeyEventLike, keys: string): boolean {
  const c = chordFor(keys)
  return (
    e.key.toLowerCase() === c.key && e.metaKey === c.meta && e.ctrlKey === c.ctrl && e.altKey === c.alt && e.shiftKey === c.shift
  )
}

const KEY_GLYPHS: Record<string, string> = {
  enter: '↵', escape: 'Esc', backspace: '⌫', delete: '⌦', tab: '⇥', space: 'Space',
  arrowup: '↑', arrowdown: '↓', arrowleft: '←', arrowright: '→',
}

/** 'Mod+Shift+P' → '⇧⌘P' (macOS order: ⌃⌥⇧⌘). */
export function formatKeys(keys: string): string {
  const c = parseKeys(keys)
  const key = KEY_GLYPHS[c.key] ?? c.key.toUpperCase()
  return `${c.ctrl ? '⌃' : ''}${c.alt ? '⌥' : ''}${c.shift ? '⇧' : ''}${c.meta ? '⌘' : ''}${key}`
}

/**
 * The available command bound to this key event, latest registration first.
 * `isInside(selector)` says whether the event target sits inside an element
 * matching the selector (for keysIgnoredIn).
 */
export function commandForKey(e: KeyEventLike, isInside: (selector: string) => boolean): Command | undefined {
  const bound = keyIndex().get(e.key.toLowerCase())
  if (!bound) return undefined // plain typing: no command uses this key
  for (let i = bound.length - 1; i >= 0; i--) {
    const cmd = bound[i]
    if (!matchesKeys(e, cmd.keys!)) continue
    if (cmd.keysIgnoredIn && isInside(cmd.keysIgnoredIn)) continue
    if (isAvailable(cmd)) return cmd
  }
  return undefined
}

// Every keystroke reaches the dispatcher, so commands with a shortcut are indexed
// by key (rebuilt when the registry changes; registration order kept).
let indexed: { from: Map<string, Command>; byKey: Map<string, Command[]> } | null = null
function keyIndex(): Map<string, Command[]> {
  if (indexed?.from === commands) return indexed.byKey
  const byKey = new Map<string, Command[]>()
  for (const cmd of commands.values()) {
    if (!cmd.keys) continue
    const key = chordFor(cmd.keys).key
    byKey.set(key, [...(byKey.get(key) ?? []), cmd])
  }
  indexed = { from: commands, byKey }
  return byKey
}

/**
 * The single app-wide keydown dispatcher (install with capture: true, so it runs
 * before CodeMirror and xterm). Returns whether a command took the event.
 */
export function handleGlobalKey(e: KeyboardEvent): boolean {
  const target = e.target
  const cmd = commandForKey(e, (selector) => target instanceof Element && !!target.closest(selector))
  if (!cmd) return false
  e.preventDefault()
  e.stopPropagation()
  runCommand(cmd)
  return true
}
