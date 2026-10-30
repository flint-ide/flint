import { RangeSet, StateEffect, StateField, type Extension, type Text } from '@codemirror/state'
import { EditorView, GutterMarker, ViewPlugin, gutter, type ViewUpdate } from '@codemirror/view'
import { Diff, type LineMark } from '../api/backend'

export type LineChange = 'added' | 'modified' | 'deleted'

/** Sets the HEAD version the gutter compares against (null = no baseline, no markers). */
export const setGitBaseline = StateEffect.define<string | null>()

const baselineField = StateField.define<string | null>({
  create: () => null,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setGitBaseline)) return e.value
    return value
  },
})

const setMarkers = StateEffect.define<RangeSet<GutterMarker>>()

const markersField = StateField.define<RangeSet<GutterMarker>>({
  create: () => RangeSet.empty,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setMarkers)) return e.value
    return tr.docChanged ? value.map(tr.changes) : value
  },
})

class ChangeMarker extends GutterMarker {
  readonly elementClass: string
  constructor(readonly kind: LineChange) {
    super()
    this.elementClass = `cm-git-${kind}`
  }
  eq(other: ChangeMarker) {
    return other.kind === this.kind
  }
}

const MARKERS: Record<LineChange, ChangeMarker> = {
  added: new ChangeMarker('added'),
  modified: new ChangeMarker('modified'),
  deleted: new ChangeMarker('deleted'),
}

/** Turns line marks (computed in Go, sorted by line) into gutter markers for `doc`. */
export function markerSet(doc: Text, marks: LineMark[]): RangeSet<GutterMarker> {
  const ranges = marks
    .filter((m) => m.line >= 1 && m.line <= doc.lines && m.kind in MARKERS)
    .map((m) => MARKERS[m.kind as LineChange].range(doc.line(m.line).from))
  return RangeSet.of(ranges)
}

const DEBOUNCE_MS = 250

/** Recomputes markers off the typing path: on baseline change and shortly after edits. */
const differ = ViewPlugin.fromClass(
  class {
    timer: ReturnType<typeof setTimeout> | undefined
    destroyed = false
    constructor(readonly view: EditorView) {
      this.schedule(0)
    }
    update(u: ViewUpdate) {
      const baselineChanged = u.transactions.some((tr) => tr.effects.some((e) => e.is(setGitBaseline)))
      if (baselineChanged) this.schedule(0)
      else if (u.docChanged) this.schedule(DEBOUNCE_MS)
    }
    schedule(delay: number) {
      clearTimeout(this.timer)
      this.timer = setTimeout(() => void this.compute(), delay)
    }
    async compute() {
      const { state } = this.view
      const base = state.field(baselineField)
      let set = RangeSet.empty as RangeSet<GutterMarker>
      if (base !== null) {
        let marks: LineMark[]
        try {
          marks = await Diff.Marks(base, state.doc.toString())
        } catch {
          return // gutter is best-effort
        }
        // A newer edit or baseline already scheduled another run.
        if (this.destroyed || this.view.state.doc !== state.doc || this.view.state.field(baselineField) !== base) return
        set = markerSet(state.doc, marks ?? [])
      }
      this.view.dispatch({ effects: setMarkers.of(set) })
    }
    destroy() {
      this.destroyed = true
      clearTimeout(this.timer)
    }
  },
)

/** Git change markers in a narrow gutter next to the line numbers. */
export function gitGutter(): Extension {
  return [
    baselineField,
    markersField,
    differ,
    gutter({
      class: 'cm-git-gutter',
      markers: (view) => view.state.field(markersField),
    }),
  ]
}
