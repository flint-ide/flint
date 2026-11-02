// Webviews (docs/design/extensions.md, "Webviews"): the document Flint loads into a
// sandboxed frame, and the checks on what the frame sends back. Framework-free.

/** The policy put first in the webview's <head>: inline code only, no network, no loads. */
export const WEBVIEW_CSP =
  "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; " +
  "media-src data: blob:; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'"

/** The frame's sandbox: scripts and forms, no same-origin (an opaque origin), no top navigation. */
export const WEBVIEW_SANDBOX = 'allow-scripts allow-forms allow-popups-to-escape-sandbox'

/** Limits on what a frame sends (the host checks requests again). */
export const LIMITS = { message: 4 << 20, data: 64 << 10, copy: 1 << 20, title: 100, text: 1000, method: 128 } as const

export interface WebviewTheme {
  vars: Readonly<Record<string, string>>
  dark: boolean
}

export interface WebviewInit {
  view: string
  key: string
  data: unknown
  theme: WebviewTheme
}

/**
 * The bridge, run in the frame before the extension's code: window.flint (request,
 * onMessage, theme, tabs, clipboard, toasts, commands) over postMessage to Flint, and
 * forwarding of shortcuts the page left alone. Written as plain ES2017; `init` is
 * replaced with the view's JSON.
 */
function bridge(init: WebviewInit): void {
  const parent = window.parent
  let seq = 0
  const pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>()
  let listeners: ((m: unknown) => void)[] = []
  let themeListeners: ((t: WebviewTheme) => void)[] = []
  const post = (m: Record<string, unknown>) => parent.postMessage({ ...m, flint: 1 }, '*')
  const applyTheme = (t: WebviewTheme) => {
    const root = document.documentElement
    for (const [k, v] of Object.entries(t.vars)) root.style.setProperty(k, v)
    root.setAttribute('data-theme', t.dark ? 'dark' : 'light')
  }
  const api = {
    view: init.view,
    key: init.key,
    data: init.data,
    theme: init.theme,
    request: (method: string, params?: unknown) =>
      new Promise((resolve, reject) => {
        const id = ++seq
        pending.set(id, { resolve, reject })
        post({ type: 'request', id, method, params: params === undefined ? null : params })
      }),
    onMessage: (fn: (m: unknown) => void) => {
      listeners.push(fn)
      return () => void (listeners = listeners.filter((f) => f !== fn))
    },
    onTheme: (fn: (t: WebviewTheme) => void) => {
      themeListeners.push(fn)
      return () => void (themeListeners = themeListeners.filter((f) => f !== fn))
    },
    openTab: (view: string, key: string, title: string, data?: unknown) => post({ type: 'openTab', view, key, title, data: data ?? null }),
    setTitle: (title: string) => post({ type: 'setTitle', title }),
    setDirty: (dirty: boolean) => post({ type: 'setDirty', dirty: !!dirty }),
    setData: (data: unknown) => post({ type: 'setData', data }),
    runCommand: (id: string, args?: unknown) => post({ type: 'runCommand', id, args: args ?? null }),
    copy: (text: string) => post({ type: 'copy', text: String(text) }),
    showMessage: (type: string, text: string) => post({ type: 'showMessage', level: type, text: String(text) }),
    openFile: (path: string, line?: number) => post({ type: 'openFile', path, line: line ?? 0 }),
  }
  window.addEventListener('message', (e) => {
    if (e.source !== parent) return
    const m = e.data
    if (!m || m.flint !== 1) return
    if (m.type === 'response') {
      const p = pending.get(m.id)
      if (!p) return
      pending.delete(m.id)
      if (typeof m.error === 'string') p.reject(new Error(m.error))
      else p.resolve(m.result)
    } else if (m.type === 'message') {
      for (const fn of listeners.slice()) {
        try {
          fn(m.message)
        } catch (err) {
          console.error(err)
        }
      }
    } else if (m.type === 'theme') {
      api.theme = m.theme
      applyTheme(m.theme)
      for (const fn of themeListeners.slice()) fn(m.theme)
    }
  })
  // Editing keys stay with the page; other shortcuts reach Flint's keymap.
  const nativeKeys = new Set(['a', 'c', 'v', 'x', 'z', 'y'])
  window.addEventListener('keydown', (e) => {
    const mod = e.metaKey || e.ctrlKey
    if (mod && !e.altKey && nativeKeys.has(e.key.toLowerCase())) return
    if (!(mod || e.altKey || e.key === 'Escape' || /^F\d{1,2}$/.test(e.key))) return
    // This listener runs before the page's own (it was added first): decide once the
    // event has been through all of them, so a page that handles ⌘S keeps it.
    setTimeout(() => {
      if (!e.defaultPrevented) post({ type: 'key', key: e.key, code: e.code, meta: e.metaKey, ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey })
    }, 0)
  })
  applyTheme(init.theme)
  ;(window as unknown as { flint: unknown }).flint = Object.freeze(api)
}

/** JSON that is safe inside a <script> element. */
function scriptJSON(v: unknown): string {
  return JSON.stringify(v).replace(/</g, '\\u003c').replace(/[\u2028\u2029]/g, (c) => `\\u${c.charCodeAt(0).toString(16)}`)
}

/** The frame's document: the policy and the bridge first in <head>, then the extension's HTML. */
export function webviewDocument(html: string, init: WebviewInit): string {
  const head =
    `<meta http-equiv="Content-Security-Policy" content="${WEBVIEW_CSP}">` +
    `<script>(${bridge.toString()})(${scriptJSON(init)});</script>`
  const m = /<head(\s[^>]*)?>/i.exec(html)
  if (m) return html.slice(0, m.index + m[0].length) + head + html.slice(m.index + m[0].length)
  const doctype = /^\s*<!doctype[^>]*>/i.exec(html)
  const at = doctype ? doctype[0].length : 0
  return `${html.slice(0, at)}<head>${head}</head>${html.slice(at)}`
}

/** What a frame may ask of Flint, once checked. */
export type FrameMessage =
  | { type: 'request'; id: number; method: string; params: unknown }
  | { type: 'openTab'; view: string; key: string; title: string; data: unknown }
  | { type: 'setTitle'; title: string }
  | { type: 'setDirty'; dirty: boolean }
  | { type: 'setData'; data: unknown }
  | { type: 'runCommand'; id: string; args: unknown }
  | { type: 'copy'; text: string }
  | { type: 'showMessage'; level: 'info' | 'warning' | 'error'; text: string }
  | { type: 'openFile'; path: string; line: number }
  | { type: 'key'; key: string; code: string; meta: boolean; ctrl: boolean; alt: boolean; shift: boolean }

const str = (v: unknown, max: number): v is string => typeof v === 'string' && v.length <= max
const jsonSize = (v: unknown): number => {
  try {
    return JSON.stringify(v ?? null).length
  } catch {
    return Infinity
  }
}

/**
 * Checks a message from a frame: its shape, sizes, and that ids name the extension's
 * own views and commands (prefix `<extension id>.`). Anything else is null.
 */
export function parseFrameMessage(raw: unknown, ext: string): FrameMessage | null {
  if (typeof raw !== 'object' || raw === null) return null
  const m = raw as Record<string, unknown>
  if (m.flint !== 1) return null
  const own = (id: unknown): id is string => str(id, 200) && id.startsWith(`${ext}.`)
  switch (m.type) {
    case 'request':
      if (!Number.isSafeInteger(m.id) || !str(m.method, LIMITS.method) || !m.method || jsonSize(m.params) > LIMITS.message) return null
      return { type: 'request', id: m.id as number, method: m.method, params: m.params ?? null }
    case 'openTab':
      if (!own(m.view) || !str(m.key, 256) || !m.key || !str(m.title, LIMITS.title) || jsonSize(m.data) > LIMITS.data) return null
      return { type: 'openTab', view: m.view, key: m.key, title: m.title, data: m.data ?? null }
    case 'setTitle':
      return str(m.title, LIMITS.title) && m.title.trim() ? { type: 'setTitle', title: m.title } : null
    case 'setDirty':
      return typeof m.dirty === 'boolean' ? { type: 'setDirty', dirty: m.dirty } : null
    case 'setData':
      return jsonSize(m.data) <= LIMITS.data ? { type: 'setData', data: m.data ?? null } : null
    case 'runCommand':
      return own(m.id) && jsonSize(m.args) <= 16 << 10 ? { type: 'runCommand', id: m.id, args: m.args ?? null } : null
    case 'copy':
      return str(m.text, LIMITS.copy) ? { type: 'copy', text: m.text } : null
    case 'showMessage': {
      const level = m.level === 'warning' || m.level === 'error' ? m.level : 'info'
      return str(m.text, LIMITS.text) && m.text ? { type: 'showMessage', level, text: m.text } : null
    }
    case 'openFile':
      return str(m.path, 4096) && m.path.startsWith('/') ? { type: 'openFile', path: m.path, line: Number.isSafeInteger(m.line) ? Math.max(0, m.line as number) : 0 } : null
    case 'key':
      if (!str(m.key, 32) || !str(m.code, 32)) return null
      return { type: 'key', key: m.key, code: m.code, meta: m.meta === true, ctrl: m.ctrl === true, alt: m.alt === true, shift: m.shift === true }
  }
  return null
}
