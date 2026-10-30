// Editor support for .http / .rest files, loaded with the first such file: a ▶ on
// the margin per request, ⌘↵ to send the request under the cursor, parse errors
// as diagnostics and a bar with the environment and redirect settings. Requests are
// found by the Go parser (HTTPAPI.Outline) off the typing path, like test markers.
import { Prec, RangeSet, StateEffect, StateField, type Extension } from '@codemirror/state'
import { EditorView, GutterMarker, ViewPlugin, gutter, keymap, showPanel, type Panel, type ViewUpdate } from '@codemirror/view'
import { setDiagnostics, type Diagnostic } from '@codemirror/lint'
import * as HTTP from '../../wailsjs/go/main/HTTPAPI'
import type { httpfile } from '../../wailsjs/go/models'
import { envOf, pickEnvironment, sendRequest, setFollowRedirects, useHttpClient } from '../store/httpClient'

type Mark = httpfile.Mark

const DEBOUNCE_MS = 300

const setMarks = StateEffect.define<Mark[]>()

/** Requests of the document, by 1-based request line (mapped through edits until the next outline). */
const marksField = StateField.define<{ marks: Mark[]; markers: RangeSet<GutterMarker> }>({
  create: () => ({ marks: [], markers: RangeSet.empty }),
  update(value, tr) {
    for (const e of tr.effects) {
      if (!e.is(setMarks)) continue
      const doc = tr.state.doc
      const ranges = e.value.flatMap((m) => (m.line >= 1 && m.line <= doc.lines ? [new SendMarker(m).range(doc.line(m.line).from)] : []))
      return { marks: e.value, markers: RangeSet.of(ranges, true) }
    }
    return tr.docChanged ? { ...value, markers: value.markers.map(tr.changes) } : value
  },
})

class SendMarker extends GutterMarker {
  constructor(readonly mark: Mark) {
    super()
  }
  eq(other: SendMarker) {
    return other.mark.line === this.mark.line && other.mark.method === this.mark.method && other.mark.url === this.mark.url
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = 'cm-test-run idle'
    el.textContent = '▶'
    el.title = `Send ${labelOf(this.mark)} (⌘↵)`
    return el
  }
}

function labelOf(m: Mark): string {
  return m.name ? `${m.name} — ${m.method} ${m.url}` : `${m.method} ${m.url}`
}

/** The request around a document position: the last request line at or above it. */
function markAt(view: EditorView, pos: number): Mark | undefined {
  const line = view.state.doc.lineAt(pos).number
  const marks = view.state.field(marksField).marks
  return [...marks].reverse().find((m) => m.line <= line) ?? marks[0]
}

function send(view: EditorView, path: string, line: number): boolean {
  const mark = markAt(view, view.state.doc.line(line).from)
  void sendRequest(path, view.state.doc.toString(), line, mark ? labelOf(mark) : 'request')
  return true
}

/** Sends the request under the cursor (palette "Send Request", same as ⌘↵). */
export function sendAtCursor(view: EditorView, path: string): void {
  send(view, path, view.state.doc.lineAt(view.state.selection.main.head).number)
}

/** Parses the document in Go after edits: gutter markers and diagnostics. */
const outline = ViewPlugin.fromClass(
  class {
    timer: ReturnType<typeof setTimeout> | undefined
    destroyed = false
    constructor(readonly view: EditorView) {
      this.schedule(0)
    }
    update(u: ViewUpdate) {
      if (u.docChanged) this.schedule(DEBOUNCE_MS)
    }
    schedule(delay: number) {
      clearTimeout(this.timer)
      this.timer = setTimeout(() => void this.parse(), delay)
    }
    async parse() {
      const doc = this.view.state.doc
      let result: httpfile.Outline
      try {
        result = await HTTP.Outline(doc.toString())
      } catch {
        return // markers are best-effort
      }
      if (this.destroyed || this.view.state.doc !== doc) return // a newer edit scheduled another run
      const diagnostics: Diagnostic[] = (result.errors ?? [])
        .filter((e) => e.line >= 1 && e.line <= doc.lines)
        .map((e) => {
          const line = doc.line(e.line)
          return { from: line.from, to: Math.max(line.to, line.from), severity: 'error', message: e.message, source: 'http' }
        })
      this.view.dispatch({ effects: setMarks.of(result.requests ?? []) })
      this.view.dispatch(setDiagnostics(this.view.state, diagnostics))
    }
    destroy() {
      this.destroyed = true
      clearTimeout(this.timer)
    }
  },
)

/** The bar above the editor: environment, redirects and Send. Plain DOM, no React. */
function settingsPanel(path: string): (view: EditorView) => Panel {
  return (view) => {
    const dom = document.createElement('div')
    dom.className = 'http-bar'

    const env = document.createElement('button')
    env.type = 'button'
    env.className = 'select sm'
    env.title = 'Variables from http-client.env.json / http-client.private.env.json next to this file'
    env.onclick = () => {
      const r = env.getBoundingClientRect()
      void pickEnvironment(path, r.left, r.bottom + 2)
    }

    const redirects = document.createElement('label')
    const follow = document.createElement('input')
    follow.type = 'checkbox'
    follow.onchange = () => setFollowRedirects(follow.checked)
    redirects.append(follow, 'Follow redirects')

    const sendButton = document.createElement('button')
    sendButton.type = 'button'
    sendButton.className = 'primary'
    sendButton.textContent = '▶ Send'
    sendButton.title = 'Send the request under the cursor (⌘↵)'
    sendButton.onclick = () => sendAtCursor(view, path)

    const hint = document.createElement('span')
    hint.className = 'muted hint'
    hint.textContent = '⌘↵ sends the request under the cursor'

    const spacer = document.createElement('span')
    spacer.className = 'spacer'
    dom.append(env, redirects, spacer, hint, sendButton)

    const render = () => {
      const name = envOf(path)
      env.textContent = `env: ${name || 'none'}`
      env.classList.toggle('empty', !name)
      follow.checked = useHttpClient.getState().followRedirects
    }
    render()
    const unsubscribe = useHttpClient.subscribe((s, prev) => {
      if (s.envs !== prev.envs || s.followRedirects !== prev.followRedirects) render()
    })
    return { dom, top: true, destroy: unsubscribe }
  }
}

/** Everything a .http file's editor gets on top of its language. */
export function httpEditor(path: string): Extension {
  return [
    marksField,
    outline,
    gutter({
      class: 'cm-test-gutter',
      markers: (view) => view.state.field(marksField).markers,
      domEventHandlers: {
        mousedown(view, line) {
          let found = false
          view.state.field(marksField).markers.between(line.from, line.from, () => {
            found = true
          })
          if (!found) return false
          return send(view, path, view.state.doc.lineAt(line.from).number)
        },
      },
    }),
    // Above the editor's own keymap: in .http files ⌘↵ sends (SQL consoles keep Run Query).
    Prec.high(keymap.of([{ key: 'Mod-Enter', preventDefault: true, run: (v) => (sendAtCursor(v, path), true) }])),
    showPanel.of(settingsPanel(path)),
  ]
}
