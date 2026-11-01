// Clipboard keys of a terminal where Ctrl belongs to the shell (Linux):
// Ctrl+Shift+C copies, Ctrl+Shift+V pastes. Framework-free for node:test.

export interface TerminalKey {
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
