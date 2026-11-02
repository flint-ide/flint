// Keys the terminal view handles itself instead of xterm: Shift+Enter (a newline in
// agent prompts) and, where Ctrl belongs to the shell (Linux), Ctrl+Shift+C copies
// and Ctrl+Shift+V pastes. Framework-free for node:test.

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

/** What Shift+Enter sends: ESC + CR (Meta+Enter), a newline in claude / codex prompts. */
export const NEWLINE_SEQUENCE = '\x1b\r'

/** Shift+Enter alone: a newline in the prompt rather than a submit. */
export function isNewlineKey(e: TerminalKey): boolean {
  return e.key === 'Enter' && e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey
}
