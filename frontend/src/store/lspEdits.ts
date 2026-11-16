// Applies a language server's change across files (rename, a code action): files
// open in an editor are changed in their buffer, so the change can be undone there
// and nothing unsaved is lost; the rest are rewritten on disk by Go.
import * as LSP from '../../bindings/flint/lspapi'
import type { lsp } from '../wails/models'
import { EventsOn } from '../wails/runtime'
import { applyEditsToView } from '../lib/lsp'
import { editSummary } from '../lib/lspText'
import { fileTabId, viewFor } from './editor'
import { showToast } from './dialog'

/** Resolves to false when a file could not be changed (the toast says which). */
export async function applyWorkspaceEdit(edit: lsp.WorkspaceEdit, what: string): Promise<boolean> {
  const files = edit.files ?? []
  if (!files.length) return true
  // Disk first: it is the part that can fail, and nothing in an editor changed yet.
  const done: string[] = []
  for (const f of files) {
    if (viewFor(fileTabId(f.path))) continue
    try {
      await LSP.ApplyToDisk(f.path, f.edits ?? [])
      done.push(f.path)
    } catch (err) {
      const changed = done.length ? ` ${done.length} other ${done.length === 1 ? 'file was' : 'files were'} already changed: review them in the Commit panel.` : ''
      showToast(`${what} stopped at ${f.path}: ${err instanceof Error ? err.message : String(err)}.${changed}`, 'error')
      return false
    }
  }
  for (const f of files) {
    const view = viewFor(fileTabId(f.path))
    if (view) applyEditsToView(view, f.edits ?? [])
  }
  // One file is the editor in front of the user: the change itself is the feedback.
  if (new Set(files.map((f) => f.path)).size > 1) showToast(`${what}: ${editSummary(files.map((f) => ({ path: f.path, edits: f.edits ?? [] })))}`)
  return true
}

let subscribed = false
/** Edits a language server sends on its own (a command it runs later) are applied the same way. */
export function subscribeLspEdits(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('lsp:applyEdit', (edit: lsp.WorkspaceEdit) => void applyWorkspaceEdit(edit, 'Language server edit'))
}
