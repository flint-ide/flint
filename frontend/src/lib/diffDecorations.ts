// Side-by-side diff rendering with plain CodeMirror decorations: changed-line
// backgrounds, changed words, spacers that keep both sides aligned and folded
// unchanged stretches. The hunks themselves come from Go (DiffAPI.Compare).
import { StateEffect, StateField, type EditorState, type Extension, type Range } from '@codemirror/state'
import { Decoration, EditorView, WidgetType, type DecorationSet } from '@codemirror/view'
import type { Hunk } from '../api/backend'

export type Side = 'a' | 'b'

export interface CollapseOptions {
  /** Unchanged lines kept visible around each change. */
  margin: number
  /** Shortest unchanged stretch worth folding. */
  minSize: number
}

/** A folded unchanged stretch: first 0-based line on each side and its length. */
export interface Fold {
  a: number
  b: number
  lines: number
}

/** Expands one folded stretch (by index into the folds list) on a side. */
export const expandFold = StateEffect.define<number>()

/**
 * Unchanged stretches that get folded. They have the same length on both sides,
 * so folding them keeps the sides aligned.
 */
export function foldsFor(hunks: Hunk[], linesA: number, opts: CollapseOptions): Fold[] {
  const folds: Fold[] = []
  let a = 0
  let b = 0
  const consider = (endA: number, first: boolean, last: boolean) => {
    const start = first ? 0 : opts.margin
    const end = last ? 0 : opts.margin
    const lines = endA - a - start - end
    if (lines >= opts.minSize) folds.push({ a: a + start, b: b + start, lines })
  }
  hunks.forEach((h, i) => {
    consider(h.fromA, i === 0, false)
    a = h.toA
    b = h.toB
  })
  consider(linesA, hunks.length === 0, true)
  return folds
}

class Spacer extends WidgetType {
  constructor(readonly lines: number) {
    super()
  }
  eq(other: Spacer) {
    return other.lines === this.lines
  }
  get estimatedHeight() {
    return -1
  }
  toDOM(view: EditorView) {
    const el = document.createElement('div')
    el.className = 'cm-diff-spacer'
    el.style.height = `${this.lines * view.defaultLineHeight}px`
    return el
  }
}

class FoldWidget extends WidgetType {
  constructor(
    readonly index: number,
    readonly lines: number,
    readonly onExpand: (index: number) => void,
  ) {
    super()
  }
  eq(other: FoldWidget) {
    return other.index === this.index && other.lines === this.lines
  }
  toDOM() {
    const el = document.createElement('div')
    el.className = 'cm-diff-fold'
    el.textContent = `⦚ ${this.lines} unchanged lines`
    el.title = 'Show unchanged lines'
    el.addEventListener('mousedown', (e) => {
      e.preventDefault()
      this.onExpand(this.index)
    })
    return el
  }
  ignoreEvent() {
    return false
  }
}

const lineDeco = { a: Decoration.line({ class: 'cm-diff-deleted' }), b: Decoration.line({ class: 'cm-diff-inserted' }) }
const textDeco = { a: Decoration.mark({ class: 'cm-diff-deleted-text' }), b: Decoration.mark({ class: 'cm-diff-inserted-text' }) }

function build(
  state: EditorState,
  side: Side,
  hunks: Hunk[],
  folds: Fold[],
  expanded: ReadonlySet<number>,
  onExpand: (index: number) => void,
): DecorationSet {
  const doc = state.doc
  const ranges: Range<Decoration>[] = []
  const lineStart = (n: number) => doc.line(Math.min(Math.max(n, 0), doc.lines - 1) + 1).from

  for (const h of hunks) {
    const [from, to, otherLen] = side === 'a' ? [h.fromA, h.toA, h.toB - h.fromB] : [h.fromB, h.toB, h.toA - h.fromA]
    for (let l = from; l < to && l < doc.lines; l++) ranges.push(lineDeco[side].range(lineStart(l)))
    if (from < to && from < doc.lines) {
      const base = lineStart(from)
      for (const r of h.inline ?? []) {
        const [rf, rt] = side === 'a' ? [r.fromA, r.toA] : [r.fromB, r.toB]
        const start = Math.min(base + rf, doc.length)
        const end = Math.min(base + rt, doc.length)
        if (end > start) ranges.push(textDeco[side].range(start, end))
      }
    }
    // Pad the shorter side so the next unchanged line sits level on both.
    const missing = otherLen - (to - from)
    if (missing > 0) {
      const widget = Decoration.widget({ widget: new Spacer(missing), block: true, side: to < doc.lines ? -1 : 1 })
      ranges.push(widget.range(to < doc.lines ? lineStart(to) : doc.length))
    }
  }

  folds.forEach((f, i) => {
    if (expanded.has(i)) return
    const first = side === 'a' ? f.a : f.b
    const last = first + f.lines - 1
    if (last >= doc.lines) return
    const deco = Decoration.replace({ widget: new FoldWidget(i, f.lines, onExpand), block: true })
    ranges.push(deco.range(lineStart(first), doc.line(last + 1).to))
  })
  return Decoration.set(ranges, true)
}

/** Decorations for one side of the diff. `onExpand` should dispatch expandFold to both sides. */
export function diffSide(side: Side, hunks: Hunk[], folds: Fold[], onExpand: (index: number) => void): Extension {
  const field = StateField.define<{ expanded: ReadonlySet<number>; decorations: DecorationSet }>({
    create: (state) => ({ expanded: new Set(), decorations: build(state, side, hunks, folds, new Set(), onExpand) }),
    update(value, tr) {
      const opened = tr.effects.filter((e) => e.is(expandFold)).map((e) => e.value as number)
      if (opened.length === 0) return value
      const expanded = new Set([...value.expanded, ...opened])
      return { expanded, decorations: build(tr.state, side, hunks, folds, expanded, onExpand) }
    },
    provide: (f) => EditorView.decorations.from(f, (v) => v.decorations),
  })
  return [field, EditorView.editorAttributes.of({ class: `cm-diff-${side}` })]
}
