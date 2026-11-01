// What the editor shows, as sent to Go for the agents' MCP tool
// get_editor_state (Go: internal/mcptools/editor.go). Pure, so it is testable;
// store/editorContext.ts sends it when it changes.

/** A range in the active file; 1-based lines and columns. An empty text is the cursor. */
export interface SelectionInfo {
  startLine: number
  startColumn: number
  endLine: number
  endColumn: number
  text: string
}

export interface EditorContext {
  files: { path: string; dirty: boolean }[]
  /** The focused file tab's path, '' when another kind of tab is active. */
  active: string
  selection: SelectionInfo | null
}

/** Characters of selected text sent (Go keeps at most 32 KiB). */
export const MAX_SELECTION_CHARS = 16 * 1024
/** Open files sent (Go refuses more than 500). */
export const MAX_FILES = 500

/** The part of a CodeMirror Text this needs. */
export interface DocLike {
  lineAt(pos: number): { number: number; from: number }
  sliceString(from: number, to: number): string
}

/** The selection from..to of doc as lines and columns, its text bounded. */
export function selectionInfo(doc: DocLike, from: number, to: number): SelectionInfo {
  const a = Math.min(from, to)
  const b = Math.max(from, to)
  const start = doc.lineAt(a)
  const end = doc.lineAt(b)
  return {
    startLine: start.number,
    startColumn: a - start.from + 1,
    endLine: end.number,
    endColumn: b - end.from + 1,
    text: a === b ? '' : doc.sliceString(a, Math.min(b, a + MAX_SELECTION_CHARS)),
  }
}

interface TabLike {
  id: string
  kind: string
  path?: string
  dirty: boolean
}

/** The snapshot of the open file tabs; selectionOf reads the active tab's live editor. */
export function editorContext(tabs: readonly TabLike[], activeId: string | null, selectionOf: (tabId: string) => SelectionInfo | null): EditorContext {
  const files = tabs
    .filter((t): t is TabLike & { path: string } => t.kind === 'file' && typeof t.path === 'string')
    .slice(0, MAX_FILES)
    .map((t) => ({ path: t.path, dirty: t.dirty }))
  const active = tabs.find((t) => t.id === activeId && t.kind === 'file')
  return {
    files,
    active: active?.path ?? '',
    selection: active ? selectionOf(active.id) : null,
  }
}

/** Equal snapshots aren't sent again. */
export const sameContext = (a: EditorContext | null, b: EditorContext): boolean => a !== null && JSON.stringify(a) === JSON.stringify(b)
