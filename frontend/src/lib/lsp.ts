import type { Extension, Text } from '@codemirror/state'
import { EditorView, ViewPlugin, hoverTooltip, keymap, type ViewUpdate } from '@codemirror/view'
import type { Completion, CompletionContext, CompletionResult } from '@codemirror/autocomplete'
import { EditorState } from '@codemirror/state'
import { lintGutter, lintKeymap, setDiagnostics, type Diagnostic as CMDiagnostic } from '@codemirror/lint'
import * as LSP from '../../bindings/flint/lspapi'
import type { lsp } from '../wails/models'
import { onDiagnostics, currentDiagnostics } from '../store/lsp'
import { agentActions, fixFileProblemsWithAgent } from './lspAgent'
import { modKey } from './platform'

export type Location = lsp.Location

const LSP_EXTENSIONS = new Set(['go', 'ts', 'tsx', 'mts', 'cts', 'js', 'jsx', 'mjs', 'cjs', 'py', 'pyi', 'rs'])
const CHANGE_DEBOUNCE_MS = 200

/** Whether Flint has a language server for this file type. */
export function lspSupported(path: string): boolean {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  return LSP_EXTENSIONS.has(ext)
}

/** CodeMirror offset → LSP position (0-based line, UTF-16 character). */
export function toLsp(doc: Text, pos: number): { line: number; character: number } {
  const line = doc.lineAt(pos)
  return { line: line.number - 1, character: pos - line.from }
}

/** LSP position → CodeMirror offset, clamped to the document. */
export function fromLsp(doc: Text, p: { line: number; character: number }): number {
  if (p.line >= doc.lines) return doc.length
  const line = doc.line(Math.max(1, p.line + 1))
  return Math.min(line.from + p.character, line.to)
}

export interface LspHooks {
  /** Jump to a definition (opens another file when needed). */
  goTo: (loc: Location) => void
  /** Show usages of a symbol. */
  showUsages: (locs: Location[], symbol: string) => void
  /** Report a user-facing problem (no server, request failed). */
  notify: (message: string) => void
}

/** Language-server features for one file: sync, completion, hover, navigation, diagnostics. */
export function lspExtension(root: string, path: string, hooks: LspHooks): Extension {
  const sync = ViewPlugin.fromClass(
    class {
      timer: ReturnType<typeof setTimeout> | undefined
      pending = false
      unsubscribe: () => void
      constructor(readonly view: EditorView) {
        void LSP.Open(root, path, docText(view))
        this.unsubscribe = onDiagnostics(path, () => this.applyDiagnostics())
        this.applyDiagnostics()
      }
      update(u: ViewUpdate) {
        if (!u.docChanged) return
        this.pending = true
        clearTimeout(this.timer)
        this.timer = setTimeout(() => void this.flush(), CHANGE_DEBOUNCE_MS)
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
          return diag.severity === 'info' ? diag : { ...diag, actions: agentActions(path, diag, list.length) }
        })
        // Dispatching inside an update is not allowed; defer to the next tick.
        queueMicrotask(() => this.view.dispatch(setDiagnostics(this.view.state, diags)))
      }
      destroy() {
        clearTimeout(this.timer)
        this.unsubscribe()
        void LSP.Close(path)
      }
    },
  )

  const flushed = async (view: EditorView) => view.plugin(sync)?.flush()

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

  return [
    sync,
    EditorState.languageData.of(() => [{ autocomplete: complete }]),
    hover,
    lintGutter(),
    keymap.of([
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
