import { Chunk } from '@codemirror/merge'
import { RangeSet, StateEffect, StateField, Text, type Extension } from '@codemirror/state'
import { EditorView, GutterMarker, ViewPlugin, gutter, type ViewUpdate } from '@codemirror/view'

export type LineChange = 'added' | 'modified' | 'deleted'

/** Sets the HEAD version the gutter compares against (null = no baseline, no markers). */
export const setGitBaseline = StateEffect.define<string | null>()

const baselineField = StateField.define<Text | null>({
  create: () => null,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setGitBaseline)) return e.value === null ? null : Text.of(e.value.split(/\r?\n/))
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

/**
 * Classifies lines of `current` against `base`: 1-based line number → change kind.
 * A deletion is marked on the line following the removed block (or the last line).
 */
export function lineChanges(base: Text, current: Text): Map<number, LineChange> {
  const out = new Map<number, LineChange>()
  for (const chunk of Chunk.build(base, current)) {
    const fromLine = current.lineAt(Math.min(chunk.fromB, current.length)).number
    if (chunk.fromB === chunk.toB) {
      if (!out.has(fromLine)) out.set(fromLine, 'deleted')
      continue
    }
    const toLine = current.lineAt(Math.min(chunk.endB, current.length)).number
    const kind: LineChange = chunk.fromA === chunk.toA ? 'added' : 'modified'
    for (let l = fromLine; l <= toLine; l++) out.set(l, kind)
  }
  return out
}

const DEBOUNCE_MS = 250

/** Recomputes markers off the typing path: on baseline change and shortly after edits. */
const differ = ViewPlugin.fromClass(
  class {
    timer: ReturnType<typeof setTimeout> | undefined
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
      this.timer = setTimeout(() => this.compute(), delay)
    }
    compute() {
      const { state } = this.view
      const base = state.field(baselineField)
      let set = RangeSet.empty as RangeSet<GutterMarker>
      if (base) {
        const ranges = [...lineChanges(base, state.doc)].sort((a, b) => a[0] - b[0])
        set = RangeSet.of(ranges.map(([line, kind]) => MARKERS[kind].range(state.doc.line(line).from)))
      }
      this.view.dispatch({ effects: setMarkers.of(set) })
    }
    destroy() {
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
