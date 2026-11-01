// Inline suggestions from a code model as ghost text after the cursor. After
// a typing pause (settings: completions.debounceMs) the code around the cursor
// goes to Go, which asks the model; every new request cancels the editor's
// previous one. Tab accepts a shown suggestion (otherwise Tab indents as
// usual), Esc or any other edit dismisses it, and typing its next characters
// keeps the rest shown.
import { EditorSelection, Prec, StateEffect, StateField, Transaction, type Extension, type Text } from '@codemirror/state'
import { Decoration, EditorView, ViewPlugin, WidgetType, keymap, type DecorationSet, type ViewUpdate } from '@codemirror/view'
import { acceptGhost, advanceGhost, cleanSuggestion, completionContext, shouldSuggest, type Ghost } from './ghostText'
import type { Cancellable } from './inlineEditWidget'

export interface CompletionOptions {
  enabled: boolean
  debounceMs: number
  contextLines: number
}

export interface InlineCompletionConfig {
  /** Read on every keystroke, so a settings change applies right away. */
  options: () => CompletionOptions
  complete: (editor: string, prefix: string, suffix: string) => Cancellable<string>
  /** The request was given up (cursor moved, editor closed). */
  cancel: (editor: string) => void
  onSuccess: () => void
  onError: (err: unknown) => void
}

const setGhost = StateEffect.define<Ghost | null>()

const ghostField = StateField.define<Ghost | null>({
  create: () => null,
  update(g, tr) {
    for (const e of tr.effects) if (e.is(setGhost)) return e.value
    if (!g) return null
    if (tr.docChanged) {
      // Typing the suggestion's next characters keeps the rest of it.
      const changes: { from: number; to: number; text: string }[] = []
      tr.changes.iterChanges((from, to, _fromB, _toB, inserted) => changes.push({ from, to, text: inserted.toString() }))
      const [c] = changes
      return changes.length === 1 && c.from === c.to && tr.isUserEvent('input.type') ? advanceGhost(g, c.from, c.text) : null
    }
    if (tr.selection && tr.state.selection.main.head !== g.pos) return null
    return g
  },
  provide: (f) => EditorView.decorations.from(f, ghostDecorations),
})

function ghostDecorations(g: Ghost | null): DecorationSet {
  if (!g) return Decoration.none
  return Decoration.set([Decoration.widget({ widget: new GhostWidget(g.text), side: 1 }).range(g.pos)])
}

class GhostWidget extends WidgetType {
  constructor(readonly text: string) {
    super()
  }
  eq(other: GhostWidget): boolean {
    return other.text === this.text
  }
  toDOM(): HTMLElement {
    const span = document.createElement('span')
    span.className = 'cm-ghost-text'
    span.textContent = this.text
    span.setAttribute('aria-hidden', 'true')
    return span
  }
}

let editorSeq = 0

class Controller {
  private readonly id = `editor-${++editorSeq}`
  private timer: ReturnType<typeof setTimeout> | undefined
  private inflight: Cancellable<string> | null = null

  constructor(
    private readonly view: EditorView,
    private readonly config: InlineCompletionConfig,
  ) {}

  update(u: ViewUpdate): void {
    if (!u.docChanged && !u.selectionSet) return
    this.stop()
    const opts = this.config.options()
    if (!opts.enabled || !u.docChanged) return
    const tr = u.transactions.find((t) => t.docChanged)
    const state = u.state
    const head = state.selection.main.head
    const line = state.doc.lineAt(head)
    const trigger = shouldSuggest({
      ranges: state.selection.ranges,
      userEvent: tr?.annotation(Transaction.userEvent),
      lineText: line.text,
      column: head - line.from,
    })
    // The completion popup may be open too: Enter picks from it, Tab takes the suggestion.
    if (!trigger || state.field(ghostField, false)) return
    this.timer = setTimeout(() => this.request(), opts.debounceMs)
  }

  private request(): void {
    this.timer = undefined
    const opts = this.config.options()
    const state = this.view.state
    if (!opts.enabled || state.selection.ranges.length !== 1 || !state.selection.main.empty) return
    const pos = state.selection.main.head
    const doc: Text = state.doc
    const { prefix, suffix } = completionContext(doc, pos, opts.contextLines)
    const call = this.config.complete(this.id, prefix, suffix)
    this.inflight = call
    call.then(
      (text) => {
        if (this.inflight !== call) return
        this.inflight = null
        this.config.onSuccess()
        const now = this.view.state
        if (now.doc !== doc || now.selection.main.head !== pos || !now.selection.main.empty) return
        const line = now.doc.lineAt(pos)
        const shown = cleanSuggestion(text, line.text.slice(pos - line.from))
        if (shown) this.view.dispatch({ effects: setGhost.of({ pos, text: shown }) })
      },
      (err) => {
        if (this.inflight !== call) return // superseded or cancelled
        this.inflight = null
        this.config.onError(err)
      },
    )
  }

  /** Drops the pending timer and the request in flight. */
  private stop(): void {
    clearTimeout(this.timer)
    this.timer = undefined
    const call = this.inflight
    this.inflight = null
    if (call) {
      call.cancel()
      this.config.cancel(this.id)
    }
  }

  destroy(): void {
    this.stop()
  }
}

function accept(view: EditorView): boolean {
  const g = view.state.field(ghostField, false)
  if (!g) return false
  const { from, insert, cursor } = acceptGhost(g, view.state.lineBreak)
  view.dispatch({
    changes: { from, insert },
    selection: EditorSelection.cursor(cursor),
    effects: setGhost.of(null),
    userEvent: 'input.complete',
    scrollIntoView: true,
  })
  return true
}

function dismiss(view: EditorView): boolean {
  if (!view.state.field(ghostField, false)) return false
  view.dispatch({ effects: setGhost.of(null) })
  return true
}

/** Ghost-text suggestions for one file editor. */
export function inlineCompletion(config: InlineCompletionConfig): Extension {
  return [
    ghostField,
    ViewPlugin.define((view) => new Controller(view, config)),
    Prec.highest(keymap.of([
      { key: 'Tab', run: accept },
      { key: 'Escape', run: dismiss },
    ])),
    EditorView.baseTheme({
      '.cm-ghost-text': { color: 'var(--fg-faint)', fontStyle: 'italic', whiteSpace: 'pre', pointerEvents: 'none' },
    }),
  ]
}
