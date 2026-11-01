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
//     keys: 'Mod+Alt+B',               // optional global shortcut ('Mod' = ⌘ / Ctrl)
//     when: () => useGit.getState().isRepo, // optional: hidden and not dispatched when false
//     run: () => openBlame(),
//   }), [])
//
// registerCommand returns the unregister function (handy as an effect cleanup);
// registerCommands does the same for a list. A later registration with the same id
// replaces the earlier one, and shortcuts registered later win over earlier ones,
// so a component can shadow an app-wide command while it is mounted.
// Shortcuts owned by someone else (CodeMirror keymaps) go in `hintKeys`: shown in
// the palette, never dispatched here, and not rebindable.
//
// The user's keymap (Settings ▸ Keymap) is an override layer on top: setKeyOverrides
// maps a command id to other keys, or to '' for none. effectiveKeys is what the
// dispatcher runs and the palette shows; `keys` stays the default.
// Framework-free on purpose: no React, no stores, so it is testable with node:test.
//
// Platforms (lib/platform.ts): 'Mod' is ⌘ on macOS and Ctrl on Linux / Windows, where
// 'Meta' names the Super key. Shortcuts show as glyphs on macOS (⇧⌘P) and spelled out
// elsewhere (Ctrl+Shift+P). A default that has to differ per platform picks with isMac().

import { isMac } from './platform'

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
  /** Physical key ('KeyB', 'Digit2'); names the key when ⌥ or ⇧ changed `key` (⌥B gives '∫'). */
  code?: string
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
let overrides: Readonly<Record<string, string>> = {}
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
  cmd: 'meta', meta: 'meta', super: 'meta', ctrl: 'ctrl', control: 'ctrl', alt: 'alt', option: 'alt', shift: 'shift',
}

/** The chord flag of a modifier name; 'Mod' is ⌘ on macOS and Ctrl elsewhere. */
function modifierFlag(name: string): keyof Omit<Chord, 'key'> | undefined {
  const m = name.toLowerCase()
  return m === 'mod' ? (isMac() ? 'meta' : 'ctrl') : MODIFIERS[m]
}

/** 'Mod+Shift+P' → { key: 'p', meta: true, shift: true, … } on macOS ({ ctrl: true, … } elsewhere). */
export function parseKeys(keys: string): Chord {
  const parts = keys.split('+')
  // 'Mod++' names the plus key itself.
  const key = keys.endsWith('++') ? '+' : parts[parts.length - 1]
  const mods = keys.endsWith('++') ? parts.slice(0, -2) : parts.slice(0, -1)
  const chord: Chord = { key: key.toLowerCase(), meta: false, ctrl: false, alt: false, shift: false }
  for (const m of mods) {
    const flag = modifierFlag(m)
    if (!flag) throw new Error(`Unknown modifier "${m}" in shortcut "${keys}"`)
    chord[flag] = true
  }
  return chord
}

// Every keystroke in an editor passes through the dispatcher: parse each shortcut once
// (per platform, since 'Mod' depends on it).
const parsed = new Map<string, Chord>()
const chordFor = (keys: string): Chord => {
  const id = (isMac() ? 'mac:' : 'pc:') + keys
  const hit = parsed.get(id)
  if (hit) return hit
  const c = parseKeys(keys)
  parsed.set(id, c)
  return c
}

/**
 * The key of an event as shortcuts name it: lower-case `key`, except that a letter
 * or digit typed with ⌥ or ⇧ is named by its physical key (⌥⌘B is 'b', not '∫';
 * ⇧⌘2 is '2', not '@'), and the space bar is 'space'.
 */
export function eventKey(e: KeyEventLike): string {
  // With a non-Latin layout (Cyrillic, Greek…) Ctrl+P gives key 'з': shortcuts go by
  // the physical key then too. (Windows later: AltGr arrives as Ctrl+Alt, so a
  // character typed with AltGr must not be read as a shortcut.)
  const nonLatin = (e.ctrlKey || e.metaKey) && e.key.length === 1 && e.key > '\x7e'
  if ((e.altKey || e.shiftKey || nonLatin) && e.code) {
    const m = /^(?:Key([A-Z])|Digit(\d))$/.exec(e.code)
    if (m) return (m[1] ?? m[2]).toLowerCase()
  }
  return e.key === ' ' ? 'space' : e.key.toLowerCase()
}

/** Exact match: every modifier must agree, so ⌘P never fires on ⇧⌘P or ⌥⌘P. */
export function matchesKeys(e: KeyEventLike, keys: string): boolean {
  const c = chordFor(keys)
  return eventKey(e) === c.key && e.metaKey === c.meta && e.ctrlKey === c.ctrl && e.altKey === c.alt && e.shiftKey === c.shift
}

const KEY_NAMES: Record<string, string> = {
  enter: 'Enter', escape: 'Escape', backspace: 'Backspace', delete: 'Delete', tab: 'Tab', space: 'Space',
  arrowup: 'ArrowUp', arrowdown: 'ArrowDown', arrowleft: 'ArrowLeft', arrowright: 'ArrowRight',
  home: 'Home', end: 'End', pageup: 'PageUp', pagedown: 'PageDown',
}

/**
 * A chord in the canonical spelling, so equal shortcuts compare equal: 'Ctrl+Alt+Shift+Mod+K'
 * on macOS, 'Mod+Meta+Alt+Shift+K' elsewhere (where Ctrl is 'Mod').
 */
export function chordToKeys(c: Chord): string {
  const named = KEY_NAMES[c.key] ?? (/^f\d{1,2}$/.test(c.key) ? c.key.toUpperCase() : c.key.length === 1 ? c.key.toUpperCase() : c.key)
  const mods = isMac()
    ? [c.ctrl && 'Ctrl', c.alt && 'Alt', c.shift && 'Shift', c.meta && 'Mod']
    : [c.ctrl && 'Mod', c.meta && 'Meta', c.alt && 'Alt', c.shift && 'Shift']
  return [...mods.filter(Boolean), named].join('+')
}

/** 'shift+mod+p' → 'Shift+Mod+P'; throws on an unknown modifier like parseKeys. */
export function canonicalKeys(keys: string): string {
  return chordToKeys(parseKeys(keys))
}

const MODIFIER_KEYS = new Set(['meta', 'control', 'alt', 'shift', 'capslock', 'fn', 'os', 'hyper', 'super'])

/**
 * The shortcut a keydown would record in the keymap editor, or null for a lone
 * modifier. Keys that type text need a modifier other than ⇧ (they'd stop typing);
 * function keys and the like may stand alone.
 */
export function keysFromEvent(e: KeyEventLike): string | null {
  const key = eventKey(e)
  if (!key || MODIFIER_KEYS.has(key) || key === 'dead' || key === 'unidentified') return null
  const typing = key.length === 1 || key === 'space' || key === 'enter' || key === 'backspace' || key === 'tab'
  if (typing && !e.metaKey && !e.ctrlKey && !e.altKey) return null
  return chordToKeys({ key, meta: e.metaKey, ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey })
}

// ---------- user keymap (override layer) ----------

/** Replaces the user's overrides: command id → keys, '' for no shortcut. Invalid keys are ignored. */
export function setKeyOverrides(map: Readonly<Record<string, string>>): void {
  const next: Record<string, string> = {}
  for (const [id, keys] of Object.entries(map)) {
    if (keys === '') next[id] = ''
    else if (validKeys(keys)) next[id] = keys
  }
  overrides = next
}

export function getKeyOverrides(): Readonly<Record<string, string>> {
  return overrides
}

function validKeys(keys: string): boolean {
  try {
    parseKeys(keys)
    return true
  } catch {
    return false
  }
}

/** Whether the keymap may change this command's shortcut (not when CodeMirror owns it). */
export function isRebindable(cmd: Command): boolean {
  return !cmd.hintKeys
}

/** The shortcut the dispatcher runs: the user's override, else the default `keys`. */
export function effectiveKeys(cmd: Command, map: Readonly<Record<string, string>> = overrides): string | undefined {
  if (!isRebindable(cmd)) return cmd.keys
  const own = Object.prototype.hasOwnProperty.call(map, cmd.id) ? map[cmd.id] : cmd.keys
  return own || undefined
}

/** The shortcut to display (palette, keymap): the effective keys or the editor-owned hint. */
export function shownKeys(cmd: Command, map: Readonly<Record<string, string>> = overrides): string | undefined {
  return effectiveKeys(cmd, map) ?? cmd.hintKeys
}

/** Whether the command's shortcut differs from its default. */
export function isOverridden(cmd: Command, map: Readonly<Record<string, string>> = overrides): boolean {
  return isRebindable(cmd) && Object.prototype.hasOwnProperty.call(map, cmd.id) && !sameKeys(map[cmd.id], cmd.keys)
}

/** Equal shortcuts in any spelling ('' and undefined mean none). */
export function sameKeys(a: string | undefined, b: string | undefined): boolean {
  if (!a || !b) return !a && !b
  try {
    return canonicalKeys(a) === canonicalKeys(b)
  } catch {
    return a === b
  }
}

/**
 * Other commands showing the same shortcut (their effective or editor-owned keys).
 * Not all are real clashes: `when` and keysIgnoredIn let commands share keys by
 * context (⌘B toggles the sidebar, but is Go to Declaration in an editor).
 */
export function keyConflicts(
  keys: string,
  exceptId: string,
  list: readonly Command[] = listCommands(),
  map: Readonly<Record<string, string>> = overrides,
): Command[] {
  return list.filter((cmd) => cmd.id !== exceptId && sameKeys(shownKeys(cmd, map), keys))
}

/** The overrides after setting `keys` for a command; dropping the entry when it equals the default. */
export function withOverride(
  map: Readonly<Record<string, string>>,
  cmd: Command,
  keys: string | null,
): Record<string, string> {
  const rest = Object.fromEntries(Object.entries(map).filter(([id]) => id !== cmd.id))
  if (keys === null || sameKeys(keys, cmd.keys)) return rest // reset to default
  return { ...rest, [cmd.id]: keys }
}

// While the keymap editor records a shortcut, every keydown goes to it instead of
// running commands.
let capture: ((e: KeyboardEvent) => void) | null = null

/** Sends keydowns to fn instead of the commands (null ends it). */
export function setKeyCapture(fn: ((e: KeyboardEvent) => void) | null): void {
  capture = fn
}

const KEY_GLYPHS: Record<string, string> = {
  enter: '↵', escape: 'Esc', backspace: '⌫', delete: '⌦', tab: '⇥', space: 'Space',
  arrowup: '↑', arrowdown: '↓', arrowleft: '←', arrowright: '→',
}

const KEY_WORDS: Record<string, string> = {
  ...KEY_NAMES, escape: 'Esc', arrowup: '↑', arrowdown: '↓', arrowleft: '←', arrowright: '→',
}

/** 'Mod+Shift+P' → '⇧⌘P' on macOS (in the order ⌃⌥⇧⌘), 'Ctrl+Shift+P' elsewhere. */
export function formatKeys(keys: string): string {
  const c = parseKeys(keys)
  if (!isMac()) {
    const key = KEY_WORDS[c.key] ?? c.key.toUpperCase()
    return [c.ctrl && 'Ctrl', c.meta && 'Super', c.alt && 'Alt', c.shift && 'Shift', key].filter(Boolean).join('+')
  }
  const key = KEY_GLYPHS[c.key] ?? c.key.toUpperCase()
  return `${c.ctrl ? '⌃' : ''}${c.alt ? '⌥' : ''}${c.shift ? '⇧' : ''}${c.meta ? '⌘' : ''}${key}`
}

/** A command's shortcut as shown ('' when it has none), e.g. for a button's tooltip. */
export function keyHint(id: string): string {
  const cmd = commands.get(id)
  const keys = cmd && shownKeys(cmd)
  return keys ? formatKeys(keys) : ''
}

/** An editor with Vim mode on (class set by lib/vim.ts). */
export const VIM_EDITOR = '.flint-vim'

/**
 * Keys a focused Vim editor keeps for itself: ⌃ with a letter, [ or ] and no
 * other modifier (⌃R redo, ⌃D/⌃U scroll, ⌃V block visual, ⌃O/⌃I jumps,
 * ⌃A/⌃X numbers, ⌃W windows, ⌃[ escape…). Flint's shortcuts on those keys
 * (⌃R Run, ⌃D Debug) still work everywhere else. Where Ctrl is also Flint's
 * modifier (Linux), Vim keeps only redo, scrolling, block visual, numbers and
 * escape, so Ctrl+P, Ctrl+S, Ctrl+W… stay Flint's.
 */
export function vimOwnsKey(e: KeyEventLike): boolean {
  if (!e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return false
  return (isMac() ? /^[a-z[\]]$/ : /^[rduvax[\]]$/).test(eventKey(e))
}

/** A terminal (xterm.js) element: terminals and run output. */
export const TERMINAL = '.xterm'

/**
 * Keys a focused terminal keeps for the shell where Ctrl is Flint's modifier
 * (Linux): Ctrl with a letter, [, ] or \ and no other modifier (Ctrl+C, Ctrl+R
 * history search, Ctrl+W…), and Ctrl+Shift+C / Ctrl+Shift+V (copy, paste). Ctrl+J
 * still toggles the terminal; other Ctrl+Shift shortcuts stay Flint's. On macOS
 * Flint's ⌘ never clashes with the shell.
 */
export function terminalOwnsKey(e: KeyEventLike): boolean {
  if (isMac() || !e.ctrlKey || e.metaKey || e.altKey) return false
  const key = eventKey(e)
  if (e.shiftKey) return key === 'c' || key === 'v'
  return key !== 'j' && /^[a-z[\]\\]$/.test(key)
}

/**
 * The available command bound to this key event, latest registration first.
 * `isInside(selector)` says whether the event target sits inside an element
 * matching the selector (for keysIgnoredIn and Vim editors).
 */
export function commandForKey(e: KeyEventLike, isInside: (selector: string) => boolean): Command | undefined {
  const bound = keyIndex().get(eventKey(e))
  if (!bound) return undefined // plain typing: no command uses this key
  if (vimOwnsKey(e) && isInside(VIM_EDITOR)) return undefined
  if (terminalOwnsKey(e) && isInside(TERMINAL)) return undefined
  for (let i = bound.length - 1; i >= 0; i--) {
    const { cmd, keys } = bound[i]
    if (!matchesKeys(e, keys)) continue
    if (cmd.keysIgnoredIn && isInside(cmd.keysIgnoredIn)) continue
    if (isAvailable(cmd)) return cmd
  }
  return undefined
}

// Every keystroke reaches the dispatcher, so commands with a shortcut are indexed
// by key (rebuilt when the registry or the overrides change; registration order kept).
interface Bound {
  cmd: Command
  keys: string
}
let indexed: { from: Map<string, Command>; overrides: Readonly<Record<string, string>>; byKey: Map<string, Bound[]> } | null = null
function keyIndex(): Map<string, Bound[]> {
  if (indexed?.from === commands && indexed.overrides === overrides) return indexed.byKey
  const byKey = new Map<string, Bound[]>()
  for (const cmd of commands.values()) {
    const keys = effectiveKeys(cmd)
    if (!keys) continue
    const key = chordFor(keys).key
    byKey.set(key, [...(byKey.get(key) ?? []), { cmd, keys }])
  }
  indexed = { from: commands, overrides, byKey }
  return byKey
}

/**
 * The single app-wide keydown dispatcher (install with capture: true, so it runs
 * before CodeMirror and xterm). Returns whether a command took the event.
 */
export function handleGlobalKey(e: KeyboardEvent): boolean {
  if (capture) {
    e.preventDefault()
    e.stopPropagation()
    capture(e)
    return true
  }
  const target = e.target
  const cmd = commandForKey(e, (selector) => target instanceof Element && !!target.closest(selector))
  if (!cmd) return false
  e.preventDefault()
  e.stopPropagation()
  runCommand(cmd)
  return true
}
