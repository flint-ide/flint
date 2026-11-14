// Editor decorations (docs/design/extensions.md, "Editor decorations"): the
// registry behind the decorationSets contribution point. A feature module or
// an external extension contributes a decoration set, hears which documents
// are open (watchDocuments) and publishes marks for them (publishDecorations):
//
//   diagnostic  an underline with a message, and a marker in the gutter
//   gutter      a marker in the gutter, with a hover
//   inline      dim text after the end of a line, clickable with a command
//   highlight   a background over lines or a range
//
// Colors come from the severity (info, ok, warning, error), so marks follow
// the theme. A publish replaces what its set showed in that document; one
// made for an older version of the document is dropped, positions are clamped
// to the document, and the marks move with later edits until the next publish.
// A decoration's actions are quick fixes: a button in its hover, a click on
// its gutter marker, or the list extensionQuickFixes returns for a shared menu.
import { RangeSet, RangeValue, StateEffect, StateField, type EditorState, type Extension, type Range, type Text } from '@codemirror/state'
import {
  Decoration,
  EditorView,
  GutterMarker,
  ViewPlugin,
  WidgetType,
  gutter,
  hoverTooltip,
  showTooltip,
  type DecorationSet,
  type Tooltip,
  type ViewUpdate,
} from '@codemirror/view'
import { useContributions } from './contributions'
import type { DecorationSetContribution } from './features'
import {
  GUTTER_ICONS,
  SEVERITY_RANK,
  decorationRange,
  editChanges,
  isLineLevel,
  positionOf,
  publishIsCurrent,
  type DecorationAction,
  type EditorDecoration,
  type Severity,
} from './decorationModel'
import { showToast } from '../store/dialog'

// ---------- state ----------

/** A published decoration at its place in the document; it moves with edits. */
export class Mark extends RangeValue {
  constructor(
    readonly set: string,
    readonly d: EditorDecoration,
    /** A place rather than a range: a line's start. It stays with the line's text. */
    point: boolean,
  ) {
    super()
    this.startSide = 1
    // A range ends before text typed at its end, and goes when its text is deleted.
    this.endSide = point ? 1 : -1
  }
}

interface SetDecorations {
  set: string
  /** The document version the decorations were made for; 0: not said. */
  version: number
  decorations: readonly EditorDecoration[]
}

/** Replaces what a set shows in this document. */
export const setDecorationsEffect = StateEffect.define<SetDecorations>()
/** Removes the decorations of the sets the function names. */
export const clearDecorationsEffect = StateEffect.define<(set: string) => boolean>()
/** Hides (true) or shows every set's decorations in this editor. */
export const hideDecorationsEffect = StateEffect.define<boolean>()

export interface DecorationState {
  /** The document's version: 1 when opened, one more with every change. */
  version: number
  sets: ReadonlyMap<string, RangeSet<Mark>>
  hidden: boolean
  /** What the sets draw, derived from the marks. */
  decorations: DecorationSet
  markers: RangeSet<GutterMarker>
}

const NO_MARKERS = RangeSet.empty as RangeSet<GutterMarker>

/** The marks of a publish, placed in doc. */
export function placeMarks(doc: Text, set: string, decorations: readonly EditorDecoration[]): RangeSet<Mark> {
  const ranges: Range<Mark>[] = []
  for (const d of decorations) {
    let { from, to } = decorationRange(doc, d)
    const line = doc.lineAt(from)
    if (d.kind === 'gutter' || d.kind === 'inline') {
      ranges.push(new Mark(set, d, true).range(line.from))
      continue
    }
    if (d.kind === 'diagnostic' && from === to) {
      // Something to underline and hover: the character at the position, else the one before.
      if (to < line.to) to++
      else if (from > line.from) from--
    }
    ranges.push(from === to ? new Mark(set, d, true).range(from) : new Mark(set, d, false).range(from, to))
  }
  return RangeSet.of(ranges, true)
}

const SEVERITIES: readonly Severity[] = ['info', 'ok', 'warning', 'error']
const bySeverity = (make: (s: Severity) => Decoration): Record<Severity, Decoration> =>
  Object.fromEntries(SEVERITIES.map((s) => [s, make(s)])) as Record<Severity, Decoration>

const UNDERLINE = bySeverity((s) => Decoration.mark({ class: `cm-ext-diagnostic cm-ext-${s}` }))
const RANGE_HIGHLIGHT = bySeverity((s) => Decoration.mark({ class: `cm-ext-highlight cm-ext-${s}` }))
const LINE_HIGHLIGHT = bySeverity((s) => Decoration.line({ class: `cm-ext-highlight-line cm-ext-${s}` }))

/** Lines one line-level highlight may cover (a set holds at most 5,000 decorations). */
const MAX_HIGHLIGHT_LINES = 2000

class LineMarker extends GutterMarker {
  constructor(
    readonly severity: Severity,
    readonly glyph: string,
    readonly title: string,
    readonly actionable: boolean,
  ) {
    super()
  }
  eq(other: LineMarker) {
    return other.severity === this.severity && other.glyph === this.glyph && other.title === this.title && other.actionable === this.actionable
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = `cm-ext-marker cm-ext-${this.severity}${this.actionable ? ' cm-ext-actionable' : ''}`
    el.textContent = this.glyph
    el.title = this.actionable ? `${this.title}${this.title ? '\n' : ''}Click for quick fixes` : this.title
    return el
  }
}

class InlineWidget extends WidgetType {
  constructor(readonly mark: Mark) {
    super()
  }
  eq(other: InlineWidget) {
    return other.mark === this.mark
  }
  toDOM() {
    const { d, set } = this.mark
    const el = document.createElement('span')
    el.className = `cm-ext-inline cm-ext-${d.severity}`
    el.textContent = d.text ?? ''
    if (d.message) el.title = d.message
    const command = d.command
    if (command) {
      el.classList.add('cm-ext-clickable')
      el.setAttribute('role', 'button')
      el.onmousedown = (e) => {
        e.preventDefault()
        run(() => contributedSet(set)?.command?.(command))
      }
    }
    return el
  }
  ignoreEvent() {
    return true
  }
}

/** What the sets' marks draw: decorations in the text and one marker per line in the gutter. */
function draw(doc: Text, sets: ReadonlyMap<string, RangeSet<Mark>>, hidden: boolean): Pick<DecorationState, 'decorations' | 'markers'> {
  if (hidden || sets.size === 0) return { decorations: Decoration.none, markers: NO_MARKERS }
  const out: Range<Decoration>[] = []
  const lines = new Map<number, { severity: Severity; glyph: string; titles: string[]; actionable: boolean }>()
  const mark = (at: number, d: EditorDecoration) => {
    const have = lines.get(at)
    const glyph = d.icon && Object.hasOwn(GUTTER_ICONS, d.icon) ? GUTTER_ICONS[d.icon] : GUTTER_ICONS.dot
    if (!have) lines.set(at, { severity: d.severity, glyph, titles: d.message ? [d.message] : [], actionable: !!d.actions?.length })
    else {
      if (SEVERITY_RANK[d.severity] > SEVERITY_RANK[have.severity]) Object.assign(have, { severity: d.severity, glyph })
      if (d.message && have.titles.length < 5) have.titles.push(d.message)
      have.actionable ||= !!d.actions?.length
    }
  }
  for (const marks of sets.values()) {
    for (const cur = marks.iter(); cur.value; cur.next()) {
      const { d } = cur.value
      const { from, to } = cur
      const line = doc.lineAt(from)
      switch (d.kind) {
        case 'gutter':
          mark(line.from, d)
          break
        case 'inline':
          out.push(Decoration.widget({ widget: new InlineWidget(cur.value), side: 1 }).range(line.to))
          break
        case 'diagnostic':
          mark(line.from, d)
          if (to > from) out.push(UNDERLINE[d.severity].range(from, to))
          break
        case 'highlight':
          if (isLineLevel(d)) {
            const last = Math.min(doc.lineAt(to).number, line.number + MAX_HIGHLIGHT_LINES - 1)
            for (let n = line.number; n <= last; n++) out.push(LINE_HIGHLIGHT[d.severity].range(doc.line(n).from))
          } else if (to > from) out.push(RANGE_HIGHLIGHT[d.severity].range(from, to))
          break
      }
    }
  }
  const markers = [...lines].map(([at, m]) => new LineMarker(m.severity, m.glyph, m.titles.join('\n'), m.actionable).range(at))
  return { decorations: Decoration.set(out, true), markers: RangeSet.of(markers, true) }
}

export const decorationField = StateField.define<DecorationState>({
  create: () => ({ version: 1, sets: new Map(), hidden: false, decorations: Decoration.none, markers: NO_MARKERS }),
  update(value, tr) {
    let { version, hidden } = value
    let sets = value.sets
    let redraw = false
    if (tr.docChanged) {
      version++
      if (sets.size) {
        sets = new Map([...sets].map(([id, marks]) => [id, marks.map(tr.changes)]))
        redraw = true
      }
    }
    for (const e of tr.effects) {
      if (e.is(setDecorationsEffect)) {
        if (!publishIsCurrent(e.value.version, version)) continue // made for a text that is gone
        const next = new Map(sets)
        if (e.value.decorations.length) next.set(e.value.set, placeMarks(tr.newDoc, e.value.set, e.value.decorations))
        else next.delete(e.value.set)
        sets = next
        redraw = true
      } else if (e.is(clearDecorationsEffect)) {
        const kept = [...sets].filter(([id]) => !e.value(id))
        if (kept.length !== sets.size) {
          sets = new Map(kept)
          redraw = true
        }
      } else if (e.is(hideDecorationsEffect) && e.value !== hidden) {
        hidden = e.value
        redraw = true
      }
    }
    if (!redraw) return version === value.version ? value : { ...value, version }
    return { version, sets, hidden, ...draw(tr.newDoc, sets, hidden) }
  },
  provide: (f) => EditorView.decorations.from(f, (v) => v.decorations),
})

/** The document version of an editor with decorations (0 without). */
export function documentVersion(state: EditorState): number {
  return state.field(decorationField, false)?.version ?? 0
}

// ---------- quick fixes ----------

export interface QuickFix {
  title: string
  run: () => Promise<void>
}

interface Placed {
  mark: Mark
  from: number
  to: number
}

/** The marks on the line of pos; with `at`, only those whose range holds pos, or that are about the whole line. */
function marksOnLine(state: EditorState, pos: number, at: boolean): Placed[] {
  const value = state.field(decorationField, false)
  if (!value || value.hidden) return []
  const line = state.doc.lineAt(Math.min(Math.max(pos, 0), state.doc.length))
  const out: Placed[] = []
  for (const marks of value.sets.values()) {
    marks.between(line.from, line.to, (from, to, mark) => {
      const lineLevel = mark.d.kind === 'gutter' || mark.d.kind === 'inline' || isLineLevel(mark.d) || from === to
      if (!at || lineLevel || (from <= pos && pos <= to)) out.push({ mark, from, to })
    })
  }
  return out
}

function contributedSet(id: string): DecorationSetContribution | undefined {
  return useContributions.getState().decorationSets.find((s) => s.id === id)
}

function setIsOn(id: string): boolean {
  const spec = contributedSet(id)
  return !!spec && (!spec.enabled || spec.enabled())
}

/** Runs something a decoration asked for; a failure is a toast, never an exception in the editor. */
function run(fn: () => void | Promise<unknown> | undefined): void {
  try {
    void Promise.resolve(fn()).catch((err: unknown) => showToast(err instanceof Error ? err.message : String(err), 'error'))
  } catch (err) {
    showToast(err instanceof Error ? err.message : String(err), 'error')
  }
}

/** Where mark is now, or null when an edit or a publish removed it. */
function locate(state: EditorState, mark: Mark): { from: number; to: number } | null {
  let found: { from: number; to: number } | null = null
  state.field(decorationField, false)?.sets.get(mark.set)?.between(0, state.doc.length, (from, to, m) => {
    if (m !== mark) return undefined
    found = { from, to }
    return false
  })
  return found
}

/**
 * Runs a decoration's quick fix: its command, or the edits its contributor
 * answers with, applied as one change (one undo step) and only if the
 * document is still the version they were asked for.
 */
async function runAction(view: EditorView, path: string, mark: Mark, action: DecorationAction): Promise<void> {
  const spec = contributedSet(mark.set)
  if (!spec) return
  if (action.command) {
    await spec.command?.(action.command)
    return
  }
  const at = locate(view.state, mark)
  if (!at || !spec.action) return
  const version = documentVersion(view.state)
  const edits = await spec.action({ set: mark.set, path, version, action: action.id, decoration: mark.d.id ?? '', ...positionOf(view.state.doc, at.from, at.to) })
  if (!edits?.length) return
  if (owners.get(path) !== view) return // the editor closed meanwhile
  if (documentVersion(view.state) !== version) {
    showToast(`${action.label}: not applied, the file changed meanwhile. Try again.`, 'error')
    return
  }
  const changes = editChanges(view.state.doc, edits)
  if (typeof changes === 'string') {
    showToast(`${action.label}: not applied (${spec.group}: ${changes}).`, 'error')
    return
  }
  const { lineBreak } = view.state
  view.dispatch({
    changes: changes.map((c) => ({ ...c, insert: lineBreak === '\n' ? c.insert : c.insert.replace(/\n/g, lineBreak) })),
    userEvent: 'input.quickfix',
    scrollIntoView: true,
  })
}

function fixesOf(view: EditorView, path: string, placed: readonly Placed[]): QuickFix[] {
  return placed.flatMap(({ mark }) => (mark.d.actions ?? []).map((a) => ({ title: a.label, run: () => runAction(view, path, mark, a) })))
}

/**
 * The quick fixes of the decorations at pos (a decoration whose range holds
 * pos, or one about pos's line), for a menu shared with other sources of
 * fixes. Empty in an editor without decorations.
 */
export function extensionQuickFixes(view: EditorView, pos: number): Promise<QuickFix[]> {
  const path = pathOf(view)
  return Promise.resolve(path === undefined ? [] : fixesOf(view, path, marksOnLine(view.state, pos, true)))
}

// The quick-fix list, as a tooltip under the position: buttons, so Tab,
// the arrow keys, Enter and Escape work.
const menuEffect = StateEffect.define<Tooltip | null>()
const menuField = StateField.define<Tooltip | null>({
  create: () => null,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(menuEffect)) return e.value
    return tr.docChanged || tr.selection ? null : value
  },
  provide: (f) => showTooltip.from(f),
})

function openMenu(view: EditorView, pos: number, fixes: readonly QuickFix[]): void {
  const close = (focus: boolean) => {
    view.dispatch({ effects: menuEffect.of(null) })
    if (focus) view.focus()
  }
  const tooltip: Tooltip = {
    pos,
    above: false,
    create: () => {
      const dom = document.createElement('div')
      dom.className = 'cm-ext-fixes'
      dom.setAttribute('role', 'menu')
      const buttons = fixes.map((fix) => {
        const b = document.createElement('button')
        b.type = 'button'
        b.setAttribute('role', 'menuitem')
        b.textContent = fix.title
        b.onclick = () => {
          close(true)
          run(fix.run)
        }
        dom.appendChild(b)
        return b
      })
      dom.onkeydown = (e) => {
        const i = buttons.indexOf(document.activeElement as HTMLButtonElement)
        if (e.key === 'Escape') close(true)
        else if (e.key === 'ArrowDown') buttons[(i + 1) % buttons.length]?.focus()
        else if (e.key === 'ArrowUp') buttons[(i - 1 + buttons.length) % buttons.length]?.focus()
        else return
        e.preventDefault()
        e.stopPropagation()
      }
      dom.addEventListener('focusout', (e) => {
        if (!dom.contains(e.relatedTarget as Node | null) && view.state.field(menuField, false) === tooltip) close(false)
      })
      return { dom, mount: () => buttons[0]?.focus() }
    },
  }
  view.dispatch({ effects: menuEffect.of(tooltip) })
}

/**
 * Shows the quick fixes of the decorations on the cursor's line as a list at
 * the cursor (the keyboard path); false when there are none.
 */
export function showExtensionQuickFixes(view: EditorView): boolean {
  const path = pathOf(view)
  if (path === undefined) return false
  const pos = view.state.selection.main.head
  let placed = marksOnLine(view.state, pos, true).filter((p) => p.mark.d.actions?.length)
  if (!placed.length) placed = marksOnLine(view.state, pos, false)
  const fixes = fixesOf(view, path, placed)
  if (!fixes.length) return false
  openMenu(view, pos, fixes)
  return true
}

// ---------- hover ----------

const hover = (path: string) =>
  hoverTooltip(
    (view, pos) => {
      const placed = marksOnLine(view.state, pos, true).filter(
        ({ mark }) => (mark.d.kind === 'diagnostic' || mark.d.kind === 'highlight') && (mark.d.message || mark.d.actions?.length),
      )
      if (!placed.length) return null
      return {
        pos: Math.min(...placed.map((p) => Math.max(p.from, view.state.doc.lineAt(pos).from))),
        end: Math.max(...placed.map((p) => Math.min(p.to, view.state.doc.lineAt(pos).to))),
        create: () => {
          const dom = document.createElement('div')
          dom.className = 'cm-ext-hover'
          for (const { mark } of placed) {
            const item = document.createElement('div')
            item.className = `cm-ext-hover-item cm-ext-${mark.d.severity}`
            if (mark.d.message) {
              const message = document.createElement('div')
              message.className = 'cm-ext-hover-message'
              message.textContent = mark.d.message
              item.appendChild(message)
            }
            const source = document.createElement('div')
            source.className = 'cm-ext-hover-source'
            source.textContent = contributedSet(mark.set)?.title ?? mark.set
            item.appendChild(source)
            for (const action of mark.d.actions ?? []) {
              const b = document.createElement('button')
              b.type = 'button'
              b.className = 'cm-ext-hover-action'
              b.textContent = action.label
              b.onclick = () => run(() => runAction(view, path, mark, action))
              item.appendChild(b)
            }
            dom.appendChild(item)
          }
          return { dom }
        },
      }
    },
    { hoverTime: 350 },
  )

// ---------- documents ----------

/** A document open in an editor; version and text are read when asked. */
export interface OpenDocument {
  readonly path: string
  /** 1 when opened, one more with every change. */
  readonly version: number
  /** The text as the editor has it, unsaved edits included. */
  text(): string
}

export interface DocumentListener {
  opened?(doc: OpenDocument): void
  /** At most every CHANGE_DELAY_MS while typing. */
  changed?(doc: OpenDocument): void
  closed?(path: string): void
}

/** How long after the last keystroke a change is reported. */
export const CHANGE_DELAY_MS = 300
/** Publishes for one editor are applied together, at most this often. */
export const APPLY_DELAY_MS = 50

/** The editor that shows each open path (the first one, if a file is open twice). */
const owners = new Map<string, EditorView>()
const listeners = new Set<DocumentListener>()

function pathOf(view: EditorView): string | undefined {
  for (const [path, v] of owners) if (v === view) return path
  return undefined
}

const documentOf = (path: string, view: EditorView): OpenDocument => ({
  path,
  get version() {
    return documentVersion(view.state)
  },
  text: () => view.state.sliceDoc(),
})

function tell(fn: (l: DocumentListener) => void): void {
  for (const l of [...listeners]) {
    try {
      fn(l)
    } catch (err) {
      console.error('decoration listener', err)
    }
  }
}

/** Hears about documents opening, changing and closing; the ones open now are announced right away. */
export function watchDocuments(listener: DocumentListener): () => void {
  listeners.add(listener)
  for (const [path, view] of owners) listener.opened?.(documentOf(path, view))
  return () => listeners.delete(listener)
}

/** The documents open in editors now. */
export function openDocuments(): OpenDocument[] {
  return [...owners].map(([path, view]) => documentOf(path, view))
}

// Effects waiting for the next APPLY_DELAY_MS tick, per editor.
const pending = new Map<EditorView, { effects: StateEffect<unknown>[]; timer: ReturnType<typeof setTimeout> }>()

function flush(view: EditorView): void {
  const p = pending.get(view)
  if (!p) return
  clearTimeout(p.timer)
  pending.delete(view)
  if (pathOf(view) !== undefined) view.dispatch({ effects: p.effects }) // still open
}

function queue(view: EditorView, effect: StateEffect<unknown>, now: boolean): void {
  const p = pending.get(view)
  if (p) p.effects.push(effect)
  else pending.set(view, { effects: [effect], timer: setTimeout(() => flush(view), APPLY_DELAY_MS) })
  if (now) flush(view)
}

/**
 * Replaces what a decoration set shows in the document at path. version is
 * the document version the decorations were made for (0: unknown). False
 * when nothing will be drawn: the set isn't contributed or is off, or the
 * file isn't open in an editor.
 */
export function publishDecorations(set: string, path: string, version: number, decorations: readonly EditorDecoration[]): boolean {
  const view = owners.get(path)
  if (!view || !setIsOn(set)) return false
  queue(view, setDecorationsEffect.of({ set, version, decorations }), false)
  return true
}

/** Removes the decorations of the sets the function names, in one document or (no path) in all. */
export function clearDecorations(sets: (set: string) => boolean, path?: string): void {
  for (const [p, view] of owners) {
    if (path !== undefined && p !== path) continue
    queue(view, clearDecorationsEffect.of(sets), true)
  }
}

/** Removes what sets that are off, or no longer contributed, still show. Call it when a set's `enabled` changes. */
export function syncDecorationSets(): void {
  clearDecorations((set) => !setIsOn(set))
}

useContributions.subscribe((s, prev) => {
  if (s.decorationSets !== prev.decorationSets) syncDecorationSets()
})

/** Hides or shows all decorations in one editor; returns whether they are hidden now. */
export function toggleDecorationsHidden(view: EditorView): boolean {
  const value = view.state.field(decorationField, false)
  if (!value) return false
  view.dispatch({ effects: hideDecorationsEffect.of(!value.hidden) })
  return !value.hidden
}

const tracker = (path: string) =>
  ViewPlugin.fromClass(
    class {
      owner: boolean
      timer: ReturnType<typeof setTimeout> | undefined
      constructor(readonly view: EditorView) {
        this.owner = !owners.has(path)
        if (!this.owner) return
        owners.set(path, view)
        // After the editor is built: a listener may publish right away.
        queueMicrotask(() => {
          if (owners.get(path) === view) tell((l) => l.opened?.(documentOf(path, view)))
        })
      }
      update(u: ViewUpdate) {
        if (!this.owner || !u.docChanged) return
        clearTimeout(this.timer)
        this.timer = setTimeout(() => tell((l) => l.changed?.(documentOf(path, this.view))), CHANGE_DELAY_MS)
      }
      destroy() {
        if (!this.owner) return
        clearTimeout(this.timer)
        const p = pending.get(this.view)
        if (p) clearTimeout(p.timer)
        pending.delete(this.view)
        owners.delete(path)
        queueMicrotask(() => tell((l) => l.closed?.(path)))
      }
    },
  )

/**
 * The editor extension for the file at path: it announces the document to
 * the decoration sets' contributors and draws what they publish.
 */
export function editorDecorations(path: string): Extension {
  return [
    decorationField,
    menuField,
    tracker(path),
    hover(path),
    gutter({
      class: 'cm-ext-gutter',
      markers: (view) => view.state.field(decorationField).markers,
      domEventHandlers: {
        mousedown(view, line, event) {
          const fixes = fixesOf(view, path, marksOnLine(view.state, line.from, false))
          if (!fixes.length) return false
          event.preventDefault()
          openMenu(view, line.from, fixes)
          return true
        },
      },
    }),
  ]
}
