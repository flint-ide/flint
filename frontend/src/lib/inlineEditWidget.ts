// Edit with Agent (⌘I) in CodeMirror: a small prompt widget above the
// selection (or the cursor line). Enter sends the instruction to the default
// agent; its answer replaces the range in one transaction (one ⌘Z undoes it),
// but only if the range still holds the text the agent was given. Esc closes
// the widget and cancels the run, which stops the agent process. No
// accept/reject step: the edit is applied, and undo takes it back.
import { EditorSelection, Prec, StateEffect, StateField, Transaction, type Extension } from '@codemirror/state'
import { isolateHistory } from '@codemirror/commands'
import { Decoration, EditorView, ViewPlugin, WidgetType, keymap, type DecorationSet } from '@codemirror/view'
import { buildEditRequest, canApply, editRange, fitReplacement, mapPending, type EditRequest, type Pending } from './inlineEdit'

/** A running request the widget can cancel (a Wails CancellablePromise). */
export interface Cancellable<T> extends PromiseLike<T> {
  cancel(): void
}

export interface InlineEditConfig {
  path: string
  /** Lines of context on each side (settings: inlineEdit.contextLines), read when sending. */
  contextLines: () => number
  run: (req: EditRequest) => Cancellable<string>
  /** Turns an error into the text shown in the widget. */
  errorText: (err: unknown) => string
}

type Phase = 'prompt' | 'running' | 'error'

interface EditState extends Pending {
  id: number
  /** Start of the range's first line, where the widget sits. */
  anchor: number
  phase: Phase
  instruction: string
  message: string
}

const openEffect = StateEffect.define<Pending>()
const updateEffect = StateEffect.define<Partial<Pick<EditState, 'phase' | 'instruction' | 'message'>>>()
const closeEffect = StateEffect.define<null>()

let nextId = 1

const editField = StateField.define<EditState | null>({
  create: () => null,
  update(value, tr) {
    let v = value && tr.docChanged ? { ...value, ...mapPending(value, tr.changes) } : value
    for (const e of tr.effects) {
      if (e.is(openEffect)) v = { ...e.value, id: nextId++, anchor: 0, phase: 'prompt', instruction: '', message: '' }
      else if (e.is(updateEffect) && v) v = { ...v, ...e.value }
      else if (e.is(closeEffect)) v = null
    }
    if (v && v !== value) v = { ...v, anchor: tr.state.doc.lineAt(v.from).from }
    return v
  },
  provide: (f) => EditorView.decorations.from(f, decorationsFor),
})

function decorationsFor(s: EditState | null): DecorationSet {
  if (!s) return Decoration.none
  const widget = Decoration.widget({ widget: new PromptWidget(s), block: true, side: -1 })
  // The widget sits above the range's first line.
  const ranges = [widget.range(s.anchor)]
  if (s.from < s.to) ranges.push(Decoration.mark({ class: 'cm-inline-edit-target' }).range(s.from, s.to))
  return Decoration.set(ranges, true)
}

class PromptWidget extends WidgetType {
  constructor(readonly s: EditState) {
    super()
  }

  eq(other: PromptWidget): boolean {
    return other.s.id === this.s.id && other.s.phase === this.s.phase && other.s.message === this.s.message
  }

  toDOM(view: EditorView): HTMLElement {
    const s = this.s
    const box = document.createElement('div')
    box.className = `cm-inline-edit cm-inline-edit-${s.phase}`
    box.setAttribute('role', 'group')
    box.setAttribute('aria-label', 'Edit with Agent')
    if (s.phase === 'running') {
      const status = document.createElement('div')
      status.className = 'cm-inline-edit-status'
      status.textContent = `Editing with the agent: ${s.instruction}`
      const hint = document.createElement('span')
      hint.className = 'cm-inline-edit-hint'
      hint.textContent = 'Esc to cancel'
      box.append(status, hint)
      return box
    }
    const input = document.createElement('input')
    input.className = 'cm-inline-edit-input'
    input.placeholder = 'Describe the change, e.g. "add error handling" — Enter to apply, Esc to close'
    input.value = s.instruction
    input.spellcheck = false
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.isComposing) {
        e.preventDefault()
        controllerOf(view)?.submit(input.value)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        controllerOf(view)?.close()
      }
    })
    box.append(input)
    if (s.message) {
      const msg = document.createElement('div')
      msg.className = 'cm-inline-edit-error'
      msg.setAttribute('role', 'alert')
      msg.textContent = s.message
      box.append(msg)
    }
    setTimeout(() => input.isConnected && input.focus())
    return box
  }

  // Keys and clicks inside the widget belong to its input, not the editor.
  ignoreEvent(): boolean {
    return true
  }
}

const controllers = new WeakMap<EditorView, Controller>()
const controllerOf = (view: EditorView) => controllers.get(view)

class Controller {
  private inflight: Cancellable<string> | null = null

  constructor(
    private readonly view: EditorView,
    private readonly config: InlineEditConfig,
  ) {
    controllers.set(view, this)
  }

  open(): void {
    const state = this.view.state
    const current = state.field(editField, false)
    if (current?.phase === 'running') return
    const sel = state.selection.main
    const range = editRange(state.doc, sel)
    this.view.dispatch({ effects: openEffect.of({ ...range, original: state.doc.sliceString(range.from, range.to) }) })
  }

  submit(instruction: string): void {
    const s = this.view.state.field(editField, false)
    if (!s || s.phase === 'running') return
    const req = buildEditRequest(this.config.path, this.view.state.doc, s, instruction, this.config.contextLines())
    if ('error' in req) {
      this.view.dispatch({ effects: updateEffect.of({ phase: 'error', instruction, message: req.error }) })
      return
    }
    this.view.dispatch({ effects: updateEffect.of({ phase: 'running', instruction: req.instruction, message: '' }) })
    this.view.focus() // Esc in the editor cancels while the agent works
    const call = this.config.run(req)
    this.inflight = call
    call.then(
      (reply) => {
        if (this.inflight !== call) return
        this.inflight = null
        this.finish(s.id, reply)
      },
      (err) => {
        if (this.inflight !== call) return // cancelled
        this.inflight = null
        this.fail(s.id, this.config.errorText(err))
      },
    )
  }

  private finish(id: number, reply: string): void {
    const s = this.view.state.field(editField, false)
    if (!s || s.id !== id) return
    if (!canApply(this.view.state.doc, s)) {
      this.fail(id, 'The code changed while the agent was working, so its edit was not applied.')
      return
    }
    const text = fitReplacement(s.original, reply)
    if (!text && s.original) {
      this.fail(id, 'The agent returned no code.')
      return
    }
    const insert = text.split('\n').join(this.view.state.lineBreak)
    this.view.dispatch({
      changes: { from: s.from, to: s.to, insert },
      selection: EditorSelection.range(s.from, s.from + text.length),
      effects: closeEffect.of(null),
      annotations: [Transaction.userEvent.of('input.inlineEdit'), isolateHistory.of('full')],
      scrollIntoView: true,
    })
    this.view.focus()
  }

  private fail(id: number, message: string): void {
    const s = this.view.state.field(editField, false)
    if (s?.id === id) this.view.dispatch({ effects: updateEffect.of({ phase: 'error', message }) })
  }

  /** Closes the widget; a running request is cancelled (the agent stops). */
  close(): boolean {
    if (!this.view.state.field(editField, false)) return false
    this.cancel()
    this.view.dispatch({ effects: closeEffect.of(null) })
    this.view.focus()
    return true
  }

  cancel(): void {
    const call = this.inflight
    this.inflight = null
    call?.cancel()
  }

  destroy(): void {
    this.cancel()
    controllers.delete(this.view)
  }
}

/** The inline edit extension of one file editor. */
export function inlineEdit(config: InlineEditConfig): Extension {
  return [
    editField,
    ViewPlugin.define((view) => new Controller(view, config)),
    Prec.highest(keymap.of([{ key: 'Escape', run: (view) => controllerOf(view)?.close() ?? false }])),
    widgetTheme,
  ]
}

/** Opens the prompt in view (⌘I); false when the editor has no inline edit. */
export function openInlineEdit(view: EditorView): boolean {
  const c = controllerOf(view)
  if (!c) return false
  c.open()
  return true
}

const widgetTheme = EditorView.baseTheme({
  '.cm-inline-edit': {
    margin: '4px 0',
    padding: '6px 8px',
    border: '1px solid var(--accent-dim)',
    borderRadius: '6px',
    backgroundColor: 'var(--bg-raised)',
    fontFamily: 'var(--ui-font, system-ui)',
    fontSize: '12px',
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: '6px',
  },
  '.cm-inline-edit-input': {
    flex: '1',
    minWidth: '200px',
    background: 'var(--bg-editor)',
    color: 'var(--fg)',
    border: '1px solid var(--border)',
    borderRadius: '4px',
    padding: '3px 6px',
    font: 'inherit',
    outline: 'none',
  },
  '.cm-inline-edit-status': { flex: '1', color: 'var(--fg-muted)' },
  '.cm-inline-edit-running .cm-inline-edit-status::before': { content: '"◌ "' },
  '.cm-inline-edit-hint': { color: 'var(--fg-faint)' },
  '.cm-inline-edit-error': { flexBasis: '100%', color: 'var(--danger)' },
  '.cm-inline-edit-target': { backgroundColor: 'var(--selection-match)' },
})
