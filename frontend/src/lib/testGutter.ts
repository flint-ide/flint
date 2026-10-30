// ▶ markers next to tests (Go `func TestXxx` / `t.Run`, JS `describe` / `it` /
// `test`) that run them, like IntelliJ's gutter. Tests are found in Go
// (RunAPI.TestMarks) off the typing path; after a run the marker shows the result.
import { RangeSet, StateEffect, StateField, type Extension } from '@codemirror/state'
import { EditorView, GutterMarker, ViewPlugin, gutter, type ViewUpdate } from '@codemirror/view'
import * as RunAPI from '../../wailsjs/go/main/RunAPI'
import type { TestStatus } from './runTree'
import { testLang, type TestGutterHooks, type TestMark } from './testFiles'

export { testLang, type TestGutterHooks, type TestMark }

const setMarkers = StateEffect.define<RangeSet<GutterMarker>>()

const markersField = StateField.define<RangeSet<GutterMarker>>({
  create: () => RangeSet.empty,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setMarkers)) return e.value
    return tr.docChanged ? value.map(tr.changes) : value
  },
})

const ICONS: Record<TestStatus | 'idle', string> = {
  idle: '▶',
  running: '◌',
  passed: '✓',
  failed: '✗',
  skipped: '↷',
  cancelled: '✗',
}

class TestMarker extends GutterMarker {
  constructor(
    readonly mark: TestMark,
    readonly status: TestStatus | undefined,
    /** Debuggable (Go): the tooltip mentions ⇧-click. */
    readonly go: boolean,
  ) {
    super()
  }
  eq(other: TestMarker) {
    return other.mark.name === this.mark.name && other.mark.kind === this.mark.kind && other.status === this.status
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = `cm-test-run ${this.status ?? 'idle'}`
    el.textContent = ICONS[this.status ?? 'idle']
    el.title =
      this.mark.kind === 'file'
        ? `Run tests in file (⌥-click: whole package${this.go ? ', ⇧-click: debug' : ''})`
        : `Run ${this.mark.name}${this.status ? ` — last run: ${this.status}` : ''}${this.go ? ' (⇧-click: debug, right click: more)' : ''}`
    return el
  }
}

const DEBOUNCE_MS = 300

function markAt(view: EditorView, pos: number): TestMark | null {
  let found: TestMark | null = null
  view.state.field(markersField).between(pos, pos, (_from, _to, marker) => {
    found = (marker as TestMarker).mark
  })
  return found
}

/** Gutter with run markers for test files; [] for other files. */
export function testGutter(path: string, hooks: TestGutterHooks): Extension {
  if (!testLang(path)) return []

  const finder = ViewPlugin.fromClass(
    class {
      marks: TestMark[] = []
      /** What the gutter shows now, to skip redraws that change nothing. */
      drawn: { marks: TestMark[]; statuses: (TestStatus | undefined)[] } | null = null
      timer: ReturnType<typeof setTimeout> | undefined
      destroyed = false
      unsubscribe: () => void
      constructor(readonly view: EditorView) {
        this.schedule(0)
        this.unsubscribe = hooks.onResults(() => this.draw())
      }
      update(u: ViewUpdate) {
        if (u.docChanged) this.schedule(DEBOUNCE_MS)
      }
      schedule(delay: number) {
        clearTimeout(this.timer)
        this.timer = setTimeout(() => void this.find(), delay)
      }
      async find() {
        const doc = this.view.state.doc
        let marks: TestMark[]
        try {
          marks = ((await RunAPI.TestMarks(path, doc.toString())) ?? []) as TestMark[]
        } catch {
          return // gutter is best-effort
        }
        if (this.destroyed || this.view.state.doc !== doc) return // a newer edit scheduled another run
        this.marks = marks
        this.draw()
      }
      draw() {
        if (this.destroyed) return
        const { doc } = this.view.state
        const statuses = this.marks.map((m) => (m.name ? hooks.status(path, m.name) : undefined))
        // Results stream in for every test file: redraw only when this file's markers change.
        const prev = this.drawn
        if (prev && prev.marks === this.marks && prev.statuses.every((s, i) => s === statuses[i])) return
        this.drawn = { marks: this.marks, statuses }
        const go = !!hooks.debug && testLang(path) === 'go'
        const ranges = this.marks.flatMap((m, i) =>
          m.line >= 1 && m.line <= doc.lines ? [new TestMarker(m, statuses[i], go).range(doc.line(m.line).from)] : [],
        )
        this.view.dispatch({ effects: setMarkers.of(RangeSet.of(ranges, true)) })
      }
      destroy() {
        this.destroyed = true
        clearTimeout(this.timer)
        this.unsubscribe()
      }
    },
  )

  return [
    markersField,
    finder,
    gutter({
      class: 'cm-test-gutter',
      markers: (view) => view.state.field(markersField),
      domEventHandlers: {
        mousedown(view, line, event) {
          const found = markAt(view, line.from)
          const e = event as MouseEvent
          if (!found || e.button !== 0 || e.ctrlKey) return false // ⌃-click is a right click on macOS
          if (e.shiftKey && hooks.debug && testLang(path) === 'go') hooks.debug(path, found)
          else hooks.run(path, found, e.altKey)
          return true
        },
        contextmenu(view, line, event) {
          const found = markAt(view, line.from)
          if (!found || !hooks.menu) return false
          const e = event as MouseEvent
          e.preventDefault() // the editor's own gutter menu (blame) skips handled events
          hooks.menu(path, found, e.clientX, e.clientY)
          return true
        },
      },
    }),
  ]
}
