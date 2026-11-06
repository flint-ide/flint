// ▶ next to `func main` of a Go `package main` file, like IntelliJ's run marker:
// click runs the program, ⇧-click debugs it, right click shows a menu. Styled as
// the test gutter's markers (testGutter.ts); store/mainRun.ts does the work.
import { StateField, type Extension } from '@codemirror/state'
import { EditorView, GutterMarker, gutter } from '@codemirror/view'
import { goMainLine, isGoProgramFile } from './goMain'
import { modifierName } from './platform'

export interface MainGutterHooks {
  run: (path: string) => void
  debug: (path: string) => void
  menu: (path: string, x: number, y: number) => void
}

class MainMarker extends GutterMarker {
  eq(other: GutterMarker) {
    return other instanceof MainMarker
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = 'cm-test-run idle cm-main-run'
    el.textContent = '▶'
    el.title = `Run program (${modifierName('Shift')}-click: debug, right click: more)`
    return el
  }
}

const marker = new MainMarker()

/** Line (1-based) of func main, recomputed as the file changes; a regex over a Go file is cheap. */
const mainLineField = StateField.define<number | null>({
  create: (state) => goMainLine(state.doc.toString()),
  update: (value, tr) => (tr.docChanged ? goMainLine(tr.newDoc.toString()) : value),
})

function onMainLine(view: EditorView, from: number): boolean {
  const line = view.state.field(mainLineField)
  return line !== null && line <= view.state.doc.lines && view.state.doc.lineAt(from).number === line
}

/** Gutter with ▶ at func main for Go program files; [] for other files. */
export function mainGutter(path: string, hooks: MainGutterHooks): Extension {
  if (!isGoProgramFile(path)) return []
  return [
    mainLineField,
    gutter({
      class: 'cm-test-gutter cm-main-gutter',
      lineMarker: (view, line) => (onMainLine(view, line.from) ? marker : null),
      lineMarkerChange: (update) => update.startState.field(mainLineField) !== update.state.field(mainLineField),
      domEventHandlers: {
        mousedown(view, line, event) {
          const e = event as MouseEvent
          if (!onMainLine(view, line.from) || e.button !== 0 || e.ctrlKey) return false // ⌃-click is a right click on macOS
          if (e.shiftKey) hooks.debug(path)
          else hooks.run(path)
          return true
        },
        contextmenu(view, line, event) {
          if (!onMainLine(view, line.from)) return false
          const e = event as MouseEvent
          e.preventDefault() // the editor's own gutter menu (blame) skips handled events
          hooks.menu(path, e.clientX, e.clientY)
          return true
        },
      },
    }),
  ]
}
