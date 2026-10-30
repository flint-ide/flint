// Breakpoints in the editor: a gutter of red dots (click toggles) that follow
// edits, and the paused program's current line. The breakpoint lines live in
// store/debug; this only mirrors them into the view and reports edits back.
import { RangeSet, StateEffect, StateField, type EditorState, type Extension, type Text } from '@codemirror/state'
import { Decoration, EditorView, GutterMarker, ViewPlugin, gutter, type DecorationSet, type ViewUpdate } from '@codemirror/view'
import { sameLines } from './breakpoints'

export interface BreakpointHooks {
  lines: (path: string) => number[]
  toggle: (path: string, line: number) => void
  /** Edits moved the file's breakpoints to these lines. */
  moved: (path: string, lines: number[]) => void
  /** Line the paused program is at in this file, or null. */
  pausedLine: (path: string) => number | null
  /** Calls back when breakpoints or the paused location change; returns the unsubscribe. */
  subscribe: (listener: () => void) => () => void
}

class BreakpointMarker extends GutterMarker {
  eq() {
    return true
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = 'cm-breakpoint'
    return el
  }
}
const marker = new BreakpointMarker()

const setBreakpoints = StateEffect.define<RangeSet<GutterMarker>>()
const breakpointsField = StateField.define<RangeSet<GutterMarker>>({
  create: () => RangeSet.empty,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setBreakpoints)) return e.value
    return tr.docChanged ? value.map(tr.changes) : value
  },
})

const setPaused = StateEffect.define<number | null>()
const pausedLine = Decoration.line({ class: 'cm-debug-line' })
const pausedField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(value, tr) {
    for (const e of tr.effects) {
      if (!e.is(setPaused)) continue
      const n = e.value
      return n && n <= tr.state.doc.lines ? Decoration.set([pausedLine.range(tr.state.doc.line(n).from)]) : Decoration.none
    }
    return tr.docChanged ? value.map(tr.changes) : value
  },
  provide: (f) => EditorView.decorations.from(f),
})

function markersFor(doc: Text, lines: number[]): RangeSet<GutterMarker> {
  return RangeSet.of(
    lines.filter((l) => l >= 1 && l <= doc.lines).map((l) => marker.range(doc.line(l).from)),
    true,
  )
}

function linesOf(state: EditorState): number[] {
  const lines: number[] = []
  state.field(breakpointsField).between(0, state.doc.length, (from) => {
    const n = state.doc.lineAt(from).number
    if (lines[lines.length - 1] !== n) lines.push(n) // two dots that collapsed onto one line
  })
  return lines
}

export function breakpointGutter(path: string, hooks: BreakpointHooks): Extension {
  const sync = ViewPlugin.fromClass(
    class {
      unsubscribe: () => void
      paused: number | null = null
      constructor(readonly view: EditorView) {
        this.unsubscribe = hooks.subscribe(() => this.pull())
        queueMicrotask(() => this.pull()) // dispatching from a constructor is not allowed
      }
      update(u: ViewUpdate) {
        if (!u.docChanged) return
        const before = linesOf(u.startState)
        if (before.length === 0) {
          // Nothing shown could have moved — but the file's text may have just
          // loaded into the empty editor, so place the stored breakpoints now.
          queueMicrotask(() => this.pull())
          return
        }
        const after = linesOf(u.state)
        if (!sameLines(after, before)) hooks.moved(path, after)
      }
      /** Store → view, when they differ. */
      pull() {
        const want = hooks.lines(path)
        const paused = hooks.pausedLine(path)
        const effects: StateEffect<unknown>[] = []
        if (!sameLines(want, linesOf(this.view.state))) effects.push(setBreakpoints.of(markersFor(this.view.state.doc, want)))
        if (paused !== this.paused) {
          this.paused = paused
          effects.push(setPaused.of(paused))
        }
        if (effects.length) this.view.dispatch({ effects })
      }
      destroy() {
        this.unsubscribe()
      }
    },
  )

  return [
    breakpointsField,
    pausedField,
    sync,
    gutter({
      class: 'cm-breakpoint-gutter',
      markers: (view) => view.state.field(breakpointsField),
      initialSpacer: () => marker,
      domEventHandlers: {
        mousedown(view, line) {
          hooks.toggle(path, view.state.doc.lineAt(line.from).number)
          return true
        },
      },
    }),
  ]
}
