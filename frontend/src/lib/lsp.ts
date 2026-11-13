import { StateEffect, StateField, type Extension } from '@codemirror/state'
import { EditorView, ViewPlugin, hoverTooltip, keymap, showTooltip, type Tooltip, type ViewUpdate } from '@codemirror/view'
import type { Completion, CompletionContext, CompletionResult } from '@codemirror/autocomplete'
import { EditorState } from '@codemirror/state'
import { lintGutter, lintKeymap, setDiagnostics, type Action, type Diagnostic as CMDiagnostic } from '@codemirror/lint'
import * as LSP from '../../bindings/flint/lspapi'
import type { lsp } from '../wails/models'
import { onDiagnostics, currentDiagnostics } from '../store/lsp'
import { agentActions, fixFileProblemsWithAgent } from './lspAgent'
import { modKey } from './platform'
import { editsToChanges, fromLsp, signatureParts, sortCodeActions, toLsp, type DocChange } from './lspText'

export { fromLsp, toLsp }

export type Location = lsp.Location

const LSP_EXTENSIONS = new Set(['go', 'ts', 'tsx', 'mts', 'cts', 'js', 'jsx', 'mjs', 'cjs', 'py', 'pyi', 'rs'])
const CHANGE_DEBOUNCE_MS = 200
const MENU_KINDS = ['quickfix', 'refactor', 'source.organizeImports', 'source.fixAll']

/** Whether Flint has a language server for this file type. */
export function lspSupported(path: string): boolean {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  return LSP_EXTENSIONS.has(ext)
}

export interface LspHooks {
  /** Jump to a definition (opens another file when needed). */
  goTo: (loc: Location) => void
  /** Show usages of a symbol. */
  showUsages: (locs: Location[], symbol: string) => void
  /** Report a user-facing problem (no server, request failed). */
  notify: (message: string) => void
  /** Apply a change across files (rename, a code action); resolves to false when it failed. */
  applyEdit: (edit: lsp.WorkspaceEdit, what: string) => Promise<boolean>
  /** Show a menu at window coordinates (quick fixes). */
  menu: (x: number, y: number, items: MenuItem[], onClose: () => void) => void
  /** Open the outline of this file. */
  showOutline: (symbols: lsp.Symbol[]) => void
  /** Tab width and whether the file indents with spaces, for the formatter. */
  indent: () => { tabSize: number; insertSpaces: boolean }
}

export interface MenuItem {
  label: string
  hint?: string
  disabled?: boolean
  onSelect: () => void
}

/** What other code may ask a file's language-server session to do (save hooks, commands). */
export interface LspActions {
  /** Formats the document; false when nothing was done (no formatter, a failure). */
  format: (quiet?: boolean) => Promise<boolean>
  organizeImports: (quiet?: boolean) => Promise<boolean>
}

const actionsByView = new WeakMap<EditorView, LspActions>()

/** The language-server actions of an editor, if it has a language server. */
export function lspActions(view: EditorView): LspActions | undefined {
  return actionsByView.get(view)
}

/** LSP edits applied to this editor as one undoable step. Returns the number of changes made. */
export function applyEditsToView(view: EditorView, edits: readonly lsp.TextEdit[], userEvent = 'lsp.edit'): number {
  const br = view.state.lineBreak
  const changes: DocChange[] = editsToChanges(view.state.doc, edits).map((c) => ({ ...c, insert: br === '\n' ? c.insert : c.insert.replace(/\n/g, br) }))
  if (changes.length) view.dispatch({ changes, userEvent })
  return changes.length
}

// Rename prompt: a small input in a tooltip at the symbol.
interface RenameState {
  from: number
  to: number
  /** Cursor position the request is made at. */
  pos: number
  value: string
}
const openRename = StateEffect.define<RenameState | null>()
const renameField = StateField.define<RenameState | null>({
  create: () => null,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(openRename)) return e.value
    // Any edit while the prompt is open would make its positions wrong: close it.
    return value && tr.docChanged ? null : value
  },
})

// Signature help: the call around the cursor, with the active parameter in bold.
interface SignatureState {
  pos: number
  help: lsp.SignatureHelp
}
const setSignature = StateEffect.define<SignatureState | null>()
const signatureField = StateField.define<SignatureState | null>({
  create: () => null,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setSignature)) return e.value
    return value
  },
  provide: (f) => showTooltip.from(f, (v) => (v ? signatureTooltip(v) : null)),
})

function signatureTooltip(s: SignatureState): Tooltip {
  return {
    pos: s.pos,
    above: true,
    create: () => {
      const dom = document.createElement('div')
      dom.className = 'cm-lsp-signature'
      const sig = s.help.signatures[s.help.activeSignature] ?? s.help.signatures[0]
      const parts = signatureParts(sig.label, sig.parameters ?? [], s.help.activeParameter)
      const label = dom.appendChild(document.createElement('div'))
      label.className = 'cm-lsp-signature-label'
      label.append(parts.before)
      if (parts.active) label.appendChild(document.createElement('b')).textContent = parts.active
      label.append(parts.after)
      if (s.help.signatures.length > 1) {
        const more = label.appendChild(document.createElement('span'))
        more.className = 'muted'
        more.textContent = `  ${s.help.activeSignature + 1}/${s.help.signatures.length}`
      }
      const doc = sig.parameters?.[s.help.activeParameter]?.documentation || sig.documentation
      if (doc) {
        const el = dom.appendChild(document.createElement('div'))
        el.className = 'cm-lsp-signature-doc'
        el.textContent = stripFences(doc)
      }
      return { dom }
    },
  }
}

/** Language-server features for one file: sync, completion, hover, navigation, diagnostics. */
export function lspExtension(root: string, path: string, hooks: LspHooks): Extension {
  const sync = ViewPlugin.fromClass(
    class {
      timer: ReturnType<typeof setTimeout> | undefined
      pending = false
      unsubscribe: () => void
      /** Characters after which the server wants to be asked for signature help. */
      triggers = new Set(['(', ','])
      signatureTimer: ReturnType<typeof setTimeout> | undefined
      constructor(readonly view: EditorView) {
        void LSP.Open(root, path, docText(view))
        this.unsubscribe = onDiagnostics(path, () => this.applyDiagnostics())
        this.applyDiagnostics()
        actionsByView.set(view, { format: (quiet) => format(view, quiet), organizeImports: (quiet) => organizeImports(view, quiet) })
      }
      update(u: ViewUpdate) {
        if (u.docChanged || u.selectionSet) this.signature(u)
        if (!u.docChanged) return
        this.pending = true
        clearTimeout(this.timer)
        this.timer = setTimeout(() => void this.flush(), CHANGE_DEBOUNCE_MS)
      }
      /** Opens signature help after a trigger character and keeps an open one in step with the cursor. */
      signature(u: ViewUpdate) {
        const open = u.state.field(signatureField, false)
        let typed = false
        if (u.docChanged) {
          for (const tr of u.transactions) {
            if (!tr.isUserEvent('input.type')) continue
            tr.changes.iterChanges((_fa, _ta, _fb, _tb, inserted) => {
              const text = inserted.toString()
              if (text && this.triggers.has(text[text.length - 1])) typed = true
            })
          }
        }
        if (!typed && !open) return
        clearTimeout(this.signatureTimer)
        this.signatureTimer = setTimeout(() => void showSignature(this.view, false), 80)
      }
      /** Sends pending edits now; called before every request so results match the buffer. */
      async flush() {
        clearTimeout(this.timer)
        if (!this.pending) return
        this.pending = false
        await LSP.Change(path, docText(this.view))
      }
      applyDiagnostics() {
        const doc = this.view.state.doc
        const list = currentDiagnostics(path)
        const diags: CMDiagnostic[] = list.map((d) => {
          const from = fromLsp(doc, d.range.start)
          const to = Math.max(from, fromLsp(doc, d.range.end))
          const diag: CMDiagnostic = {
            from,
            to: to === from ? Math.min(from + 1, doc.length) : to,
            severity: d.severity === 1 ? 'error' : d.severity === 2 ? 'warning' : 'info',
            message: d.message,
            source: d.source || undefined,
          }
          const quickFix: Action = { name: 'Quick fix…', apply: (v, at) => void codeActions(v, at, ['quickfix']) }
          return diag.severity === 'info' ? diag : { ...diag, actions: [quickFix, ...agentActions(path, diag, list.length)] }
        })
        // Dispatching inside an update is not allowed; defer to the next tick.
        queueMicrotask(() => this.view.dispatch(setDiagnostics(this.view.state, diags)))
      }
      destroy() {
        clearTimeout(this.timer)
        clearTimeout(this.signatureTimer)
        actionsByView.delete(this.view)
        this.unsubscribe()
        void LSP.Close(path)
      }
    },
  )

  const flushed = async (view: EditorView) => view.plugin(sync)?.flush()

  /** Runs a request that answers with locations and shows them (one: jump; several: a list). */
  const navigate = async (view: EditorView, request: typeof LSP.Definition, nothing: string) => {
    const pos = view.state.selection.main.head
    await flushed(view)
    const { line, character } = toLsp(view.state.doc, pos)
    try {
      const locs = await request(path, line, character)
      if (locs.length === 0) hooks.notify(nothing)
      else if (locs.length === 1) hooks.goTo(locs[0])
      else hooks.showUsages(locs, symbolAt(view.state, pos))
    } catch (err) {
      hooks.notify(errorText(err))
    }
  }

  /** Applies an edit of this file here (undoable) and the rest through the app. */
  const applyHere = async (view: EditorView, edit: lsp.WorkspaceEdit, what: string): Promise<boolean> => {
    if (!edit.files?.length) return false
    return hooks.applyEdit(edit, what)
  }

  const startRename = async (view: EditorView) => {
    const pos = view.state.selection.main.head
    await flushed(view)
    const doc = view.state.doc
    const { line, character } = toLsp(doc, pos)
    let from: number, to: number, value: string
    try {
      const target = await LSP.PrepareRename(path, line, character)
      if (view.state.doc !== doc) return // edited meanwhile
      if (target.known) {
        from = fromLsp(doc, target.range.start)
        to = fromLsp(doc, target.range.end)
        value = target.placeholder || doc.sliceString(from, to)
      } else {
        const word = view.state.wordAt(pos)
        if (!word) return hooks.notify('Nothing to rename here: put the cursor on a name')
        from = word.from
        to = word.to
        value = doc.sliceString(from, to)
      }
    } catch (err) {
      return hooks.notify(errorText(err))
    }
    view.dispatch({ effects: openRename.of({ from, to, pos, value }) })
  }

  const submitRename = async (view: EditorView, state: RenameState, name: string) => {
    view.dispatch({ effects: openRename.of(null) })
    view.focus()
    if (!name || name === state.value) return
    const { line, character } = toLsp(view.state.doc, state.pos)
    try {
      const edit = await LSP.Rename(path, line, character, name)
      if (!edit.files?.length) return hooks.notify('The language server found nothing to rename')
      await applyHere(view, edit, `Renamed ${state.value} to ${name}`)
    } catch (err) {
      hooks.notify(errorText(err))
    }
  }

  const renameTooltip = showTooltip.from(renameField, (state) =>
    state && {
      pos: state.from,
      above: false,
      create: (view) => {
        const dom = document.createElement('div')
        dom.className = 'cm-lsp-rename'
        const input = dom.appendChild(document.createElement('input'))
        input.value = state.value
        input.spellcheck = false
        input.setAttribute('aria-label', 'New name')
        input.size = Math.max(12, state.value.length + 4)
        let done = false
        const close = () => {
          if (done) return
          done = true
          view.dispatch({ effects: openRename.of(null) })
        }
        input.addEventListener('keydown', (e) => {
          e.stopPropagation()
          if (e.key === 'Enter') {
            e.preventDefault()
            done = true
            void submitRename(view, state, input.value.trim())
          } else if (e.key === 'Escape') {
            e.preventDefault()
            close()
            view.focus()
          }
        })
        input.addEventListener('blur', close)
        return { dom, mount: () => (input.focus(), input.select()) }
      },
    },
  )

  /**
   * The quick-fix menu at a position (the selection when it isn't empty). Without a
   * narrower `only` it asks for fixes, refactorings and the two source actions that
   * edit the file; servers also offer actions that open web pages or their own UI
   * (gopls: "Browse documentation…"), which Flint has nowhere to show.
   */
  async function codeActions(view: EditorView, at?: number, only: string[] = MENU_KINDS) {
    const sel = view.state.selection.main
    const from = at ?? sel.from
    const to = at ?? sel.to
    await flushed(view)
    const doc = view.state.doc
    let actions: lsp.CodeAction[]
    try {
      actions = await LSP.CodeActions(path, { start: toLsp(doc, from), end: toLsp(doc, to) }, only)
    } catch (err) {
      return hooks.notify(errorText(err))
    }
    if (view.state.doc !== doc) return
    if (!actions.length) return hooks.notify(only === MENU_KINDS ? 'No quick fixes or refactorings here' : 'The language server has no quick fix here')
    const coords = view.coordsAtPos(to) ?? view.coordsAtPos(from)
    if (!coords) return
    const items: MenuItem[] = sortCodeActions(actions).map((a) => ({
      label: a.title,
      hint: a.disabled || undefined,
      disabled: !!a.disabled,
      onSelect: () => void runCodeAction(view, a),
    }))
    hooks.menu(coords.left, coords.bottom + 2, items, () => view.focus())
  }

  const runCodeAction = async (view: EditorView, action: lsp.CodeAction) => {
    const doc = view.state.doc
    try {
      const edit = await LSP.ApplyCodeAction(path, action.id)
      if (view.state.doc !== doc) return hooks.notify('The file changed while the language server was working. Try again')
      if (edit.files?.length) await applyHere(view, edit, action.title)
    } catch (err) {
      hooks.notify(errorText(err))
    }
  }

  async function format(view: EditorView, quiet = false): Promise<boolean> {
    await flushed(view)
    const doc = view.state.doc
    try {
      const { tabSize, insertSpaces } = hooks.indent()
      const edits = await LSP.Format(path, tabSize, insertSpaces)
      if (view.state.doc !== doc) return false // typed meanwhile: the edits no longer fit
      applyEditsToView(view, edits ?? [], 'lsp.format')
      return true
    } catch (err) {
      if (!quiet) hooks.notify(errorText(err))
      return false
    }
  }

  async function organizeImports(view: EditorView, quiet = false): Promise<boolean> {
    await flushed(view)
    const doc = view.state.doc
    try {
      const edit = await LSP.OrganizeImports(path)
      if (view.state.doc !== doc) return false
      // Imports live in this file: edits elsewhere would be a surprise on save.
      const here = (edit.files ?? []).filter((f) => f.path === path)
      for (const f of here) applyEditsToView(view, f.edits ?? [], 'lsp.organizeImports')
      if (!here.length && !quiet) hooks.notify('Imports are already in order')
      return true
    } catch (err) {
      if (!quiet) hooks.notify(errorText(err))
      return false
    }
  }

  async function showSignature(view: EditorView, explicit: boolean) {
    const pos = view.state.selection.main.head
    await flushed(view)
    const doc = view.state.doc
    const { line, character } = toLsp(doc, pos)
    let help: lsp.SignatureHelp | null = null
    try {
      help = await LSP.SignatureHelp(path, line, character)
    } catch {
      // not ready, not installed: no popup
    }
    if (view.state.doc !== doc || view.state.selection.main.head !== pos) return
    const open = view.state.field(signatureField, false)
    if (!help?.signatures?.length) {
      if (open) view.dispatch({ effects: setSignature.of(null) })
      if (explicit) hooks.notify('No parameter info here')
      return
    }
    view.dispatch({ effects: setSignature.of({ pos, help }) })
  }

  const outline = async (view: EditorView) => {
    await flushed(view)
    try {
      const symbols = await LSP.DocumentSymbols(path)
      if (!symbols.length) hooks.notify('The language server found no symbols in this file')
      else hooks.showOutline(symbols)
    } catch (err) {
      hooks.notify(errorText(err))
    }
  }

  const complete = async (ctx: CompletionContext): Promise<CompletionResult | null> => {
    const word = ctx.matchBefore(/[\w$]*/)
    const charBefore = ctx.state.sliceDoc(ctx.pos - 1, ctx.pos)
    if (!ctx.explicit && !word?.text && !['.', ':', '>', '"', "'", '/'].includes(charBefore)) return null
    const view = (ctx as CompletionContext & { view?: EditorView }).view
    if (view) await flushed(view)
    const { line, character } = toLsp(ctx.state.doc, ctx.pos)
    let list: lsp.CompletionList
    try {
      list = await LSP.Completion(path, line, character)
    } catch {
      return null // server not ready / not installed: plain editing still works
    }
    if (ctx.aborted || !list.items?.length) return null
    const from = list.items[0].edit ? fromLsp(ctx.state.doc, list.items[0].edit.range.start) : (word?.from ?? ctx.pos)
    const options: Completion[] = list.items.map((it, i) => ({
      label: it.label,
      detail: it.detail || undefined,
      info: it.documentation || undefined,
      type: KIND_TYPES[it.kind] ?? 'text',
      apply: it.edit ? it.edit.newText : it.insertText,
      boost: -Math.min(99, i / 10), // keep the server's ordering among equal matches
    }))
    return { from, options, validFor: list.incomplete ? undefined : /^[\w$]*$/ }
  }

  const goToDefinition = async (view: EditorView, pos: number) => {
    await flushed(view)
    const { line, character } = toLsp(view.state.doc, pos)
    try {
      const locs = await LSP.Definition(path, line, character)
      if (locs.length === 0) hooks.notify('No definition found')
      else if (locs.length === 1) hooks.goTo(locs[0])
      else hooks.showUsages(locs, symbolAt(view.state, pos))
    } catch (err) {
      hooks.notify(String(err))
    }
  }

  const findUsages = async (view: EditorView) => {
    const pos = view.state.selection.main.head
    await flushed(view)
    const { line, character } = toLsp(view.state.doc, pos)
    try {
      const locs = await LSP.References(path, line, character)
      if (locs.length === 0) hooks.notify('No usages found')
      else hooks.showUsages(locs, symbolAt(view.state, pos))
    } catch (err) {
      hooks.notify(String(err))
    }
  }

  const hover = hoverTooltip(async (view, pos) => {
    await flushed(view)
    const { line, character } = toLsp(view.state.doc, pos)
    let text = ''
    try {
      text = await LSP.Hover(path, line, character)
    } catch {
      return null
    }
    if (!text) return null
    return {
      pos,
      create: () => {
        const dom = document.createElement('div')
        dom.className = 'cm-lsp-hover'
        dom.textContent = stripFences(text)
        return { dom }
      },
    }
  }, { hoverTime: 350 })

  // The server's own trigger characters replace the defaults once it is up.
  const triggers = ViewPlugin.define((view) => {
    void LSP.SignatureTriggers(path)
      .then((chars) => {
        const plugin = view.plugin(sync)
        if (plugin && chars?.length) plugin.triggers = new Set(chars)
      })
      .catch(() => undefined)
    return {}
  })

  return [
    sync,
    triggers,
    renameField,
    renameTooltip,
    signatureField,
    EditorState.languageData.of(() => [{ autocomplete: complete }]),
    hover,
    lintGutter(),
    keymap.of([
      // Esc closes parameter info (and falls through when there is none).
      {
        key: 'Escape',
        run: (v) => {
          if (!v.state.field(signatureField, false)) return false
          v.dispatch({ effects: setSignature.of(null) })
          return true
        },
      },
      // IntelliJ ⇧F6 / VS Code F2: rename the symbol everywhere.
      { key: 'Shift-F6', run: (v) => (void startRename(v), true) },
      { key: 'F2', run: (v) => (void startRename(v), true) },
      // IntelliJ ⌥↵ / VS Code ⌘.: quick fixes and refactorings.
      { key: 'Alt-Enter', run: (v) => (void codeActions(v), true) },
      { key: 'Mod-.', run: (v) => (void codeActions(v), true) },
      // IntelliJ ⌥⌘L / VS Code ⇧⌥F: reformat the file.
      { key: 'Mod-Alt-l', run: (v) => (void format(v), true) },
      { key: 'Shift-Alt-f', run: (v) => (void format(v), true) },
      { key: 'Shift-Alt-o', run: (v) => (void organizeImports(v), true) },
      // IntelliJ ⌥⌘B implementations, ⇧⌘B type declaration, ⌘F12 file structure.
      { key: 'Mod-Alt-b', run: (v) => (void navigate(v, LSP.Implementation, 'No implementations found'), true) },
      { key: 'Mod-Shift-b', run: (v) => (void navigate(v, LSP.TypeDefinition, 'No type definition found'), true) },
      { key: 'Mod-F12', run: (v) => (void outline(v), true) },
      { key: 'Mod-Shift-Space', run: (v) => (void showSignature(v, true), true) },
      ...lintKeymap, // ⇧⌘M problems panel, F8 next problem
      // ⌥⇧⌘L: fix all problems in the file with the agent.
      { key: 'Mod-Alt-Shift-l', run: (v) => fixFileProblemsWithAgent(v, path) },
      // IntelliJ: ⌘B / F12 go to declaration, ⌥F7 / ⇧F12 find usages.
      { key: 'Mod-b', run: (v) => (void goToDefinition(v, v.state.selection.main.head), true) },
      { key: 'F12', run: (v) => (void goToDefinition(v, v.state.selection.main.head), true) },
      { key: 'Alt-F7', run: (v) => (void findUsages(v), true) },
      { key: 'Shift-F12', run: (v) => (void findUsages(v), true) },
    ]),
    EditorView.domEventHandlers({
      mousedown(e, view) {
        if (!modKey(e) || e.button !== 0) return false
        const pos = view.posAtCoords({ x: e.clientX, y: e.clientY })
        if (pos === null) return false
        e.preventDefault()
        void goToDefinition(view, pos)
        return true
      },
    }),
  ]
}

/** A request's error as one line for a toast (Wails wraps Go errors in an object). */
function errorText(err: unknown): string {
  if (err instanceof Error) return err.message
  if (typeof err === 'object' && err && 'message' in err) return String((err as { message: unknown }).message)
  return String(err)
}

function docText(view: EditorView): string {
  return view.state.doc.toString() // LSP wants \n-joined text; offsets match the editor
}

function symbolAt(state: EditorState, pos: number): string {
  const word = state.wordAt(pos)
  return word ? state.sliceDoc(word.from, word.to) : ''
}

function stripFences(md: string): string {
  return md.replace(/```[a-z]*\n?/g, '').trim()
}

// LSP CompletionItemKind → CodeMirror completion type (drives the icon).
const KIND_TYPES: Record<number, string> = {
  1: 'text', 2: 'method', 3: 'function', 4: 'function', 5: 'property', 6: 'variable', 7: 'class',
  8: 'interface', 9: 'namespace', 10: 'property', 11: 'constant', 12: 'constant', 13: 'enum',
  14: 'keyword', 15: 'text', 16: 'constant', 17: 'text', 18: 'text', 19: 'namespace', 20: 'constant',
  21: 'constant', 22: 'class', 23: 'keyword', 24: 'keyword', 25: 'type',
}
