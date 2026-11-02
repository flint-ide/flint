import { parseKeys } from '../lib/commands'
import { useEditor, viewFor } from '../store/editor'

/**
 * Runs the active editor's own key binding (LSP, search, ⌘↵ in SQL consoles), so a
 * palette entry behaves exactly like its shortcut. CodeMirror is imported lazily:
 * it's already loaded whenever an editor exists.
 */
export async function pressInEditor(keys: string): Promise<void> {
  const id = useEditor.getState().activeId
  const view = id ? viewFor(id) : undefined
  if (!view) return
  const { runScopeHandlers } = await import('@codemirror/view')
  const chord = parseKeys(keys)
  const raw = keys.split('+').pop() ?? ''
  const event = new KeyboardEvent('keydown', {
    key: raw.length === 1 ? raw.toLowerCase() : raw, // CodeMirror names: 'b', 'F7', 'Enter'
    metaKey: chord.meta,
    ctrlKey: chord.ctrl,
    altKey: chord.alt,
    shiftKey: chord.shift,
    bubbles: true,
    cancelable: true,
  })
  view.focus()
  runScopeHandlers(view, event, 'editor')
}
