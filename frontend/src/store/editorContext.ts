// Sends what the editor shows (open files, active file, selection) to Go for
// the agents' MCP tool get_editor_state: debounced, only when it changed, and
// only while the setting is on. No polling: editor-store changes and
// CodeMirror selection updates (CodeEditor) schedule a send.
import * as EditorContextAPI from '../../bindings/flint/editorcontextapi'
import { EditorSnapshot } from '../../bindings/flint/internal/mcptools/models'
import { editorContext, sameContext, selectionInfo, type EditorContext } from '../lib/editorContext'
import { sharesEditor } from '../lib/mcpSettings'
import { useEditor, viewFor } from './editor'
import { useSettings } from './settings'

const DELAY_MS = 300
const EMPTY: EditorContext = { files: [], active: '', selection: null }

let timer: ReturnType<typeof setTimeout> | undefined
let lastSent: EditorContext | null = null

/** Sends the editor state shortly, unless it doesn't change by then. */
export function scheduleEditorContextSync(): void {
  if (!sharesEditor(useSettings.getState().settings.mcpServer)) return
  clearTimeout(timer)
  timer = setTimeout(send, DELAY_MS)
}

function current(): EditorContext {
  const { tabs, activeId } = useEditor.getState()
  return editorContext(tabs, activeId, (tabId) => {
    const view = viewFor(tabId)
    if (!view) return null
    const { from, to } = view.state.selection.main
    return selectionInfo(view.state.doc, from, to)
  })
}

function send(ctx: EditorContext = current()): void {
  timer = undefined
  if (sameContext(lastSent, ctx)) return
  lastSent = ctx
  EditorContextAPI.Update(EditorSnapshot.createFrom(structuredClone(ctx))).catch(() => {
    lastSent = null // try again with the next change
  })
}

/** Follows editor tabs and the setting; returns the unsubscribe function. */
export function subscribeEditorContext(): () => void {
  const offEditor = useEditor.subscribe((s, prev) => {
    if (s.tabs !== prev.tabs || s.activeId !== prev.activeId) scheduleEditorContextSync()
  })
  const offSettings = useSettings.subscribe((s, prev) => {
    if (s.settings.mcpServer === prev.settings.mcpServer) return
    if (sharesEditor(s.settings.mcpServer)) {
      scheduleEditorContextSync()
    } else {
      clearTimeout(timer)
      send(EMPTY) // switched off: Go forgets what it had
    }
  })
  scheduleEditorContextSync()
  return () => {
    offEditor()
    offSettings()
    clearTimeout(timer)
  }
}
