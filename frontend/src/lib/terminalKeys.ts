// Keys the terminal view handles itself instead of xterm: Shift+Enter (a newline in
// agent prompts), on macOS ⌘+arrows (line start / end, scrollback top / bottom) and,
// where Ctrl belongs to the shell (Linux), Ctrl+Shift+C copies and Ctrl+Shift+V
// pastes. Framework-free for node:test.

export interface TerminalKey {
  key?: string
  code?: string
  ctrlKey: boolean
  shiftKey: boolean
  altKey: boolean
  metaKey: boolean
}

/**
 * What a keydown in a Linux terminal does with the clipboard: 'copy', 'paste',
 * 'none' (the copy key with nothing selected: the clipboard keeps its text),
 * or null when it isn't a clipboard key and goes to the terminal.
 */
export function terminalClipboardKey(e: TerminalKey, hasSelection: boolean): 'copy' | 'paste' | 'none' | null {
  if (!e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return null
  if (e.code === 'KeyV') return 'paste'
  if (e.code === 'KeyC') return hasSelection ? 'copy' : 'none'
  return null
}

/** What a ⌘+arrow does in a macOS terminal: bytes for the shell, or a scroll of the view. */
export type MacLineAction = { send: string } | { scroll: 'top' | 'bottom' }

// ⌘← / ⌘→ are Ctrl+A / Ctrl+E: start and end of the line in readline, zsh and agent prompts.
const MAC_LINE_KEYS: Readonly<Record<string, MacLineAction>> = {
  ArrowLeft: { send: '\x01' },
  ArrowRight: { send: '\x05' },
  ArrowUp: { scroll: 'top' },
  ArrowDown: { scroll: 'bottom' },
}

/**
 * ⌘ with an arrow and nothing else, as macOS terminals do it: ⌘← / ⌘→ jump to the
 * start / end of the line, ⌘↑ / ⌘↓ to the top / bottom of the scrollback. Null for
 * any other key (xterm sends nothing for ⌘+arrows by itself).
 */
export function macLineKey(e: TerminalKey): MacLineAction | null {
  if (!e.metaKey || e.ctrlKey || e.altKey || e.shiftKey || !e.key) return null
  return MAC_LINE_KEYS[e.key] ?? null
}

/** What Shift+Enter sends: ESC + CR (Meta+Enter), a newline in claude / codex prompts. */
export const NEWLINE_SEQUENCE = '\x1b\r'

/** Shift+Enter alone: a newline in the prompt rather than a submit. */
export function isNewlineKey(e: TerminalKey): boolean {
  return e.key === 'Enter' && e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey
}
