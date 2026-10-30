import { Prec, RangeSet, StateEffect, StateField, type EditorState, type Extension, type Text } from '@codemirror/state'
import { GutterMarker, gutter } from '@codemirror/view'
import { blameLabel, blameTooltip, lineCommits, type BlameCommit, type BlameRange } from './blame'

/** What the annotation column shows; open is called when an entry is clicked. */
export interface BlameAnnotations {
  commits: BlameCommit[]
  ranges: BlameRange[]
  untracked: boolean
  open: (commit: BlameCommit) => void
}

/** Shows annotations for the current document (null hides the column). */
export const setBlame = StateEffect.define<BlameAnnotations | null>()

class BlameMarker extends GutterMarker {
  constructor(readonly commit: BlameCommit) {
    super()
  }
  eq(other: BlameMarker) {
    return other.commit === this.commit
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = this.commit.uncommitted ? 'cm-blame-entry uncommitted' : 'cm-blame-entry'
    el.textContent = blameLabel(this.commit)
    el.title = blameTooltip(this.commit)
    return el
  }
}

/** One marker per line; markers are shared per commit, so even huge files stay cheap. */
export function blameMarkers(doc: Text, a: Omit<BlameAnnotations, 'open'>): RangeSet<BlameMarker> {
  const perLine = lineCommits(a.commits, a.ranges, doc.lines, a.untracked)
  const markers = new Map<BlameCommit, BlameMarker>()
  const ranges = []
  for (let n = 1; n <= doc.lines; n++) {
    const c = perLine[n]
    let m = markers.get(c)
    if (!m) markers.set(c, (m = new BlameMarker(c)))
    ranges.push(m.range(doc.line(n).from))
  }
  return RangeSet.of(ranges)
}

interface BlameState {
  markers: RangeSet<BlameMarker>
  open: (commit: BlameCommit) => void
}

const blameField = StateField.define<BlameState | null>({
  create: () => null,
  update(value, tr) {
    for (const e of tr.effects) {
      if (e.is(setBlame)) return e.value ? { markers: blameMarkers(tr.state.doc, e.value), open: e.value.open } : null
    }
    // Edits shift annotations with their lines; new lines stay blank until the next blame.
    return value && tr.docChanged ? { ...value, markers: value.markers.map(tr.changes) } : value
  },
})

/** The commit annotated on the line at pos, if annotations are shown. */
export function blameAt(state: EditorState, pos: number): BlameCommit | null {
  const b = state.field(blameField, false)
  if (!b) return null
  const from = state.doc.lineAt(pos).from
  let found: BlameCommit | null = null
  b.markers.between(from, from, (_f, _t, m) => {
    found = m.commit
    return false
  })
  return found
}

/** Opens the commit annotated at pos, as clicking its entry does. */
export function openBlameAt(state: EditorState, pos: number): boolean {
  const b = state.field(blameField, false)
  const c = blameAt(state, pos)
  if (!b || !c) return false
  b.open(c)
  return true
}

/** Whether annotations are currently shown. */
export function blameShown(state: EditorState): boolean {
  return !!state.field(blameField, false)
}

/** IntelliJ-style "Annotate with Git Blame" column, leftmost in the gutter. */
export function blameGutter(): Extension {
  return [
    blameField,
    Prec.highest(
      gutter({
        class: 'cm-blame-gutter',
        markers: (view) => view.state.field(blameField)?.markers ?? RangeSet.empty,
        domEventHandlers: {
          click: (view, line) => openBlameAt(view.state, line.from),
        },
      }),
    ),
  ]
}
