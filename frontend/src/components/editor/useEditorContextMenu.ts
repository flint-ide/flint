import { useEffect, type RefObject } from 'react'
import { EditorSelection } from '@codemirror/state'
import type { EditorView } from '@codemirror/view'
import { formatKeys, getCommand, isAvailable, runCommand, shownKeys } from '../../lib/commands'
import { editorContextMenu } from '../../lib/editorMenu'
import { goMainLine, isGoProgramFile } from '../../lib/goMain'
import { ClipboardGetText, ClipboardSetText } from '../../wails/runtime'
import { openContextMenu } from '../../store/contextMenu'
import { copyPaths, revealInFinder } from '../../store/fileOps'
import { mainItems } from '../../store/mainRun'

/** Text of the selection (ranges joined by newlines, like CodeMirror's own copy). */
const selectedText = (v: EditorView) =>
  v.state.selection.ranges.filter((r) => !r.empty).map((r) => v.state.sliceDoc(r.from, r.to)).join('\n')

function clipboardActions(v: EditorView) {
  return {
    copy: () => void ClipboardSetText(selectedText(v)),
    cut: () => {
      const text = selectedText(v)
      if (!text) return
      void ClipboardSetText(text)
      v.dispatch(v.state.replaceSelection(''), { userEvent: 'delete.cut' })
    },
    paste: () =>
      void ClipboardGetText().then((text) => {
        if (text) v.dispatch(v.state.replaceSelection(text), { userEvent: 'input.paste', scrollIntoView: true })
        v.focus()
      }),
    selectAll: () => v.dispatch({ selection: EditorSelection.single(0, v.state.doc.length) }),
  }
}

/** A right click outside the selection moves the caret there first (so "Find Usages" acts on what was clicked). */
function placeCaret(v: EditorView, e: MouseEvent): void {
  const pos = v.posAtCoords({ x: e.clientX, y: e.clientY })
  if (pos === null) return
  if (v.state.selection.ranges.some((r) => r.from <= pos && pos <= r.to && !r.empty)) return
  v.dispatch({ selection: EditorSelection.cursor(pos) })
}

/** The editor's right-click menu on the text (gutters keep their own menus: tests, ▶, blame). */
export function useEditorContextMenu(view: RefObject<EditorView | null>, path: string, ready: boolean): void {
  useEffect(() => {
    const v = view.current
    if (!v || !ready) return
    const onContextMenu = (e: MouseEvent) => {
      if (e.defaultPrevented || !(e.target instanceof Element)) return
      // Only the text itself: gutters, panels, tooltips and widgets with their own inputs
      // (the agent's inline-edit prompt) keep their own menus and clipboard.
      if (!v.contentDOM.contains(e.target) || e.target.closest('input, textarea, [contenteditable="false"], .cm-inline-edit')) return
      e.preventDefault()
      v.focus()
      placeCaret(v, e)
      const program = isGoProgramFile(path) && goMainLine(v.state.doc.toString()) !== null ? mainItems(path) : []
      const items = editorContextMenu({
        hasSelection: v.state.selection.ranges.some((r) => !r.empty),
        command: (id) => {
          const cmd = getCommand(id)
          if (!cmd || !isAvailable(cmd)) return null
          const keys = shownKeys(cmd)
          return { title: cmd.title, kbd: keys ? formatKeys(keys) : undefined }
        },
        runCommand: (id) => {
          const cmd = getCommand(id)
          if (cmd) runCommand(cmd)
        },
        clipboard: clipboardActions(v),
        clipboardKeys: { cut: formatKeys('Mod+X'), copy: formatKeys('Mod+C'), paste: formatKeys('Mod+V'), selectAll: formatKeys('Mod+A') },
        program,
        file: {
          copyPath: () => void copyPaths([path], false),
          copyRelativePath: () => void copyPaths([path], true),
          reveal: () => void revealInFinder(path),
        },
      })
      openContextMenu(e.clientX, e.clientY, items, { minWidth: 240, onClose: () => v.focus() })
    }
    v.dom.addEventListener('contextmenu', onContextMenu)
    return () => v.dom.removeEventListener('contextmenu', onContextMenu)
  }, [view, path, ready])
}
