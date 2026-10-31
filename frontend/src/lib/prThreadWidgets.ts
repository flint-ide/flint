// Review threads inside the side-by-side diff. A thread is a block widget under its
// line whose contents React renders through a portal; the other side gets an empty
// spacer of the same height (kept in step by a ResizeObserver), so both sides stay
// aligned however long the thread or its reply box grows. A "+" gutter on lines
// GitHub accepts comments on opens the new-comment box.
import { StateEffect, StateField, type EditorState, type Extension, type Range } from '@codemirror/state'
import { Decoration, EditorView, GutterMarker, ViewPlugin, WidgetType, gutter, type DecorationSet, type ViewUpdate } from '@codemirror/view'
import type { Anchor, Side } from './prReview'

/** Height assumed for a thread that hasn't been rendered yet. */
const ESTIMATED_HEIGHT = 96

/**
 * Shared by both sides of one diff: the widget elements React renders threads into,
 * and the measured height of each thread (applied to its spacer on the other side).
 */
export class ThreadLayout {
  private readonly heights = new Map<string, number>()
  private readonly mirrors = new Map<string, HTMLElement>()
  private hosts: ReadonlyMap<string, HTMLElement> = new Map()
  private readonly listeners = new Set<() => void>()
  views: readonly EditorView[] = []

  height(key: string): number {
    return this.heights.get(key) ?? ESTIMATED_HEIGHT
  }

  /** The thread elements by anchor key; a new map whenever one comes or goes. */
  getHosts = (): ReadonlyMap<string, HTMLElement> => this.hosts

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  attachHost(key: string, el: HTMLElement): void {
    this.hosts = new Map(this.hosts).set(key, el)
    this.listeners.forEach((fn) => fn())
  }

  detachHost(key: string, el: HTMLElement): void {
    if (this.hosts.get(key) !== el) return
    const next = new Map(this.hosts)
    next.delete(key)
    this.hosts = next
    this.listeners.forEach((fn) => fn())
  }

  attachMirror(key: string, el: HTMLElement): void {
    this.mirrors.set(key, el)
  }

  detachMirror(key: string, el: HTMLElement): void {
    if (this.mirrors.get(key) === el) this.mirrors.delete(key)
  }

  /** A thread changed height: resize its spacer and let both editors re-measure. */
  resized(key: string, height: number): void {
    if (height <= 0 || this.heights.get(key) === height) return
    this.heights.set(key, height)
    const mirror = this.mirrors.get(key)
    if (mirror) mirror.style.height = `${height}px`
    this.views.forEach((v) => v.requestMeasure())
  }
}

const observers = new WeakMap<HTMLElement, ResizeObserver>()

class ThreadWidget extends WidgetType {
  constructor(
    readonly key: string,
    readonly layout: ThreadLayout,
  ) {
    super()
  }
  eq(other: ThreadWidget) {
    return other.key === this.key && other.layout === this.layout
  }
  get estimatedHeight() {
    return this.layout.height(this.key)
  }
  toDOM() {
    const el = document.createElement('div')
    el.className = 'cm-pr-thread'
    const observer = new ResizeObserver(() => this.layout.resized(this.key, el.getBoundingClientRect().height))
    observer.observe(el)
    observers.set(el, observer)
    this.layout.attachHost(this.key, el)
    return el
  }
  destroy(dom: HTMLElement) {
    observers.get(dom)?.disconnect()
    this.layout.detachHost(this.key, dom)
  }
  // Typing, clicks and selection inside the thread belong to it, not the editor.
  ignoreEvent() {
    return true
  }
}

class MirrorWidget extends WidgetType {
  constructor(
    readonly key: string,
    readonly layout: ThreadLayout,
  ) {
    super()
  }
  eq(other: MirrorWidget) {
    return other.key === this.key && other.layout === this.layout
  }
  get estimatedHeight() {
    return this.layout.height(this.key)
  }
  toDOM() {
    const el = document.createElement('div')
    el.className = 'cm-pr-thread-mirror'
    el.style.height = `${this.layout.height(this.key)}px`
    this.layout.attachMirror(this.key, el)
    return el
  }
  destroy(dom: HTMLElement) {
    this.layout.detachMirror(this.key, dom)
  }
}

/** Replaces the anchors (threads and the new-comment box) shown in a diff. */
export const setThreadAnchors = StateEffect.define<readonly Anchor[]>()

function build(state: EditorState, side: Side, layout: ThreadLayout, anchors: readonly Anchor[]): DecorationSet {
  const doc = state.doc
  const lineEnd = (line: number) => doc.line(Math.min(Math.max(line, 0), doc.lines - 1) + 1).to
  const ranges: Range<Decoration>[] = []
  for (const a of anchors) {
    if (a.side === side) {
      ranges.push(Decoration.widget({ widget: new ThreadWidget(a.key, layout), block: true, side: 1 }).range(lineEnd(a.line)))
    } else if (a.mirror < 0) {
      ranges.push(Decoration.widget({ widget: new MirrorWidget(a.key, layout), block: true, side: -1 }).range(0))
    } else {
      ranges.push(Decoration.widget({ widget: new MirrorWidget(a.key, layout), block: true, side: 1 }).range(lineEnd(a.mirror)))
    }
  }
  return Decoration.set(ranges, true)
}

/** Thread widgets (own side) and spacers (other side's threads) for one side of the diff. */
export function threadDecorations(side: Side, layout: ThreadLayout, initial: readonly Anchor[]): Extension {
  const field = StateField.define<DecorationSet>({
    create: (state) => build(state, side, layout, initial),
    update(value, tr) {
      const next = tr.effects.findLast((e) => e.is(setThreadAnchors))
      return next ? build(tr.state, side, layout, next.value as readonly Anchor[]) : value
    },
    provide: (f) => EditorView.decorations.from(f),
  })
  return [field, visibleWidth]
}

/**
 * Block widgets are as wide as the longest line; threads instead stick to the visible
 * part of the side (CSS reads these variables), so long lines don't push them away.
 */
const visibleWidth = ViewPlugin.define((view) => {
  const measure = () =>
    view.requestMeasure({
      read: () => ({
        gutter: view.dom.querySelector<HTMLElement>('.cm-gutters')?.offsetWidth ?? 0,
        width: view.scrollDOM.clientWidth,
      }),
      write: ({ gutter, width }) => {
        view.dom.style.setProperty('--pr-gutter-width', `${gutter}px`)
        view.dom.style.setProperty('--pr-visible-width', `${Math.max(200, width - gutter)}px`)
      },
    })
  measure()
  return { update: (u: ViewUpdate) => void (u.geometryChanged && measure()) }
})

class PlusMarker extends GutterMarker {
  toDOM() {
    const el = document.createElement('span')
    el.className = 'cm-pr-plus'
    el.textContent = '+'
    el.title = 'Comment on this line'
    return el
  }
}

const plus = new PlusMarker()

/**
 * The "+" gutter: on lines GitHub accepts comments on (canComment, 1-based), a click
 * calls onAdd with the 0-based line.
 */
export function commentGutter(side: Side, canComment: (line: number) => boolean, onAdd: (side: Side, line: number) => void): Extension {
  return gutter({
    class: 'cm-pr-gutter',
    lineMarker: (view, block) => (canComment(view.state.doc.lineAt(block.from).number) ? plus : null),
    initialSpacer: () => plus,
    domEventHandlers: {
      mousedown: (view, block) => {
        const line = view.state.doc.lineAt(block.from).number
        if (!canComment(line)) return false
        onAdd(side, line - 1)
        return true
      },
    },
  })
}
