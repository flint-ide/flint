// Names of Vim's modes for the status bar. Framework-free (no CodeMirror), so it
// is testable with node:test; lib/vim.ts feeds it the events of @replit/codemirror-vim.

/** The 'vim-mode-change' event: mode 'normal' | 'insert' | 'visual' | 'replace', subMode 'linewise' | 'blockwise' | ''. */
export interface VimModeEvent {
  mode: string
  subMode?: string
}

/** 'NORMAL', 'INSERT', 'VISUAL', 'VISUAL LINE', 'VISUAL BLOCK' or 'REPLACE'. */
export function vimModeLabel(e: VimModeEvent): string {
  if (e.mode === 'visual') {
    if (e.subMode === 'linewise') return 'VISUAL LINE'
    if (e.subMode === 'blockwise') return 'VISUAL BLOCK'
    return 'VISUAL'
  }
  return (e.mode || 'normal').toUpperCase()
}

/** The flags codemirror-vim keeps per editor (cm.state.vim). */
export interface VimStateLike {
  insertMode?: boolean
  visualMode?: boolean
  visualLine?: boolean
  visualBlock?: boolean
}

/** The mode of an editor that just got focus, from its state flags. */
export function vimStateLabel(s: VimStateLike | null | undefined): string {
  if (s?.insertMode) return 'INSERT'
  if (s?.visualMode) return vimModeLabel({ mode: 'visual', subMode: s.visualLine ? 'linewise' : s.visualBlock ? 'blockwise' : '' })
  return 'NORMAL'
}
