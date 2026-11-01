// The merge editor's inline conflict blocks: tinted sides, labelled markers and an
// action bar above each conflict. Conflicts are re-read from the document on every
// change, so manual edits (deleting a marker, say) update them right away.
import { StateField, type EditorState, type Extension, type Range } from '@codemirror/state'
import { Decoration, EditorView, WidgetType, type DecorationSet } from '@codemirror/view'
import { findConflicts, type ConflictRange, type Resolution } from './mergeConflicts'
import { formatKeys, getCommand, shownKeys } from './commands'

export interface MergeLabels {
  current: string
  incoming: string
}

export interface MergeActions {
  resolve: (index: number, how: Resolution) => void
  askAgent: (index: number) => void
}

// command: the palette command whose (rebindable) shortcut the tooltip shows.
const BUTTONS: { how: Resolution | 'agent'; label: string; title: string; command: string }[] = [
  { how: 'current', label: 'Accept Current', title: 'Keep the current side', command: 'merge.acceptCurrent' },
  { how: 'incoming', label: 'Accept Incoming', title: 'Keep the incoming side', command: 'merge.acceptIncoming' },
  { how: 'both', label: 'Accept Both', title: 'Current, then incoming', command: 'merge.acceptBoth' },
  { how: 'both-incoming-first', label: 'Both, Incoming First', title: 'Incoming, then current', command: 'merge.acceptBothIncomingFirst' },
  { how: 'agent', label: '✦ Ask Agent', title: 'Put this conflict, with both sides and the base, in the agent chat', command: 'merge.askAgent' },
]

function tooltip(title: string, command: string): string {
  const cmd = getCommand(command)
  const keys = cmd && shownKeys(cmd)
  return keys ? `${title} (${formatKeys(keys)})` : title
}

class ActionBar extends WidgetType {
  constructor(
    readonly index: number,
    readonly total: number,
    readonly labels: MergeLabels,
    readonly actions: MergeActions,
  ) {
    super()
  }
  eq(other: ActionBar) {
    return other.index === this.index && other.total === this.total && other.labels === this.labels
  }
  toDOM() {
    const bar = document.createElement('div')
    bar.className = 'cm-merge-actions'
    const count = document.createElement('span')
    count.className = 'cm-merge-count'
    count.textContent = `Conflict ${this.index + 1} of ${this.total}`
    bar.append(count)
    for (const b of BUTTONS) {
      const button = document.createElement('button')
      button.type = 'button'
      button.className = b.how === 'agent' ? 'link agent-action' : 'link'
      button.textContent = b.label
      // Read on hover, so it follows the keymap.
      button.addEventListener('mouseenter', () => (button.title = tooltip(b.title, b.command)))
      // Keep the editor's focus and selection; act on click.
      button.addEventListener('mousedown', (e) => e.preventDefault())
      button.addEventListener('click', () => (b.how === 'agent' ? this.actions.askAgent(this.index) : this.actions.resolve(this.index, b.how)))
      bar.append(button)
    }
    return bar
  }
  ignoreEvent() {
    return true
  }
}

class Tag extends WidgetType {
  constructor(
    readonly text: string,
    readonly side: string,
  ) {
    super()
  }
  eq(other: Tag) {
    return other.text === this.text && other.side === this.side
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = `cm-merge-tag cm-merge-tag-${this.side}`
    el.textContent = this.text
    return el
  }
}

const line = (cls: string) => Decoration.line({ class: cls })
const deco = {
  marker: line('cm-merge-marker'),
  current: line('cm-merge-current'),
  base: line('cm-merge-base'),
  incoming: line('cm-merge-incoming'),
}

export interface MergeState {
  conflicts: ConflictRange[]
  decorations: DecorationSet
}

function build(state: EditorState, labels: MergeLabels, actions: MergeActions): MergeState {
  const doc = state.doc
  const conflicts = findConflicts(doc)
  const ranges: Range<Decoration>[] = []
  const lines = (from: number, to: number, d: Decoration) => {
    for (let n = from; n < to; n++) ranges.push(d.range(doc.line(n).from))
  }
  const tag = (n: number, text: string, side: string) =>
    ranges.push(Decoration.widget({ widget: new Tag(text, side), side: 1 }).range(doc.line(n).to))
  conflicts.forEach((c, i) => {
    const open = doc.line(c.open)
    ranges.push(Decoration.widget({ widget: new ActionBar(i, conflicts.length, labels, actions), block: true, side: -1 }).range(open.from))
    ranges.push(deco.marker.range(open.from))
    tag(c.open, `Current · ${labels.current}`, 'current')
    lines(c.open + 1, c.base ?? c.sep, deco.current)
    if (c.base !== null) {
      ranges.push(deco.marker.range(doc.line(c.base).from))
      tag(c.base, 'Base', 'base')
      lines(c.base + 1, c.sep, deco.base)
    }
    ranges.push(deco.marker.range(doc.line(c.sep).from))
    lines(c.sep + 1, c.close, deco.incoming)
    ranges.push(deco.marker.range(doc.line(c.close).from))
    tag(c.close, `Incoming · ${labels.incoming}`, 'incoming')
  })
  return { conflicts, decorations: Decoration.set(ranges, true) }
}

/** The merge editor's conflict decorations; read the conflicts with state.field(field). */
export function mergeConflicts(labels: MergeLabels, actions: MergeActions): { field: StateField<MergeState>; extension: Extension } {
  const field = StateField.define<MergeState>({
    create: (state) => build(state, labels, actions),
    update: (value, tr) => (tr.docChanged ? build(tr.state, labels, actions) : value),
    provide: (f) => EditorView.decorations.from(f, (v) => v.decorations),
  })
  return { field, extension: field }
}
