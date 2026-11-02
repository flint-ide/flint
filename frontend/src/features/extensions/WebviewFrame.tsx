import { useEffect, useMemo, useRef, useState } from 'react'
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import { errorMessage } from '../../api/backend'
import { THEME_VARIABLES } from '../../lib/settings'
import { showToast } from '../../store/dialog'
import { useEditor } from '../../store/editor'
import { useSettings } from '../../store/settings'
import { useExtensions } from './store'
import { LIMITS, WEBVIEW_SANDBOX, parseFrameMessage, webviewDocument, type FrameMessage, type WebviewTheme } from './webviewBridge'
import { subscribeWebview } from './webviewEvents'

/** Theme values a webview gets besides the custom-theme colors: fonts and the edit marks. */
const EXTRA_VARS = ['--font-ui', '--font-mono', '--editor-font-size', '--edited', '--edited-bg']

/** Flint's current theme as CSS variables, as the frame's :root gets them. */
function currentTheme(): WebviewTheme {
  const root = document.documentElement
  const style = getComputedStyle(root)
  const vars: Record<string, string> = {}
  for (const name of [...THEME_VARIABLES, ...EXTRA_VARS]) {
    const v = style.getPropertyValue(name).trim()
    if (v) vars[name] = v
  }
  // Variables Flint sets inline (fonts, sizes, a custom theme) come along too.
  for (let i = 0; i < root.style.length; i++) {
    const name = root.style[i]
    if (name.startsWith('--')) vars[name] = root.style.getPropertyValue(name).trim()
  }
  return { vars, dark: root.dataset.theme !== 'light' }
}

interface Props {
  ext: string
  view: string
  /** The tab's key; '' for a panel. */
  tabKey: string
  /** The tab's saved data (the frame reads it as flint.data when it loads). */
  data: unknown
  active: boolean
  /** The editor tab it is in, for titles, the unsaved dot and saved data. */
  tabId?: string
}

/**
 * An extension's own UI (docs/design/extensions.md, "Webviews"): its HTML in a sandboxed
 * frame with an opaque origin and no network, talking to Flint only through the bridge.
 */
export function WebviewFrame({ ext, view, tabKey, data, active, tabId }: Props) {
  const frame = useRef<HTMLIFrameElement>(null)
  const [html, setHtml] = useState<string | null>(null)
  const [error, setError] = useState('')
  const info = useExtensions((s) => s.list.find((i) => i.id === ext))
  const version = info?.version
  const theme = useSettings((s) => s.theme)

  useEffect(() => {
    let cancelled = false
    setHtml(null)
    setError('')
    API.WebviewHTML(ext, view).then(
      (text) => !cancelled && setHtml(text),
      (err: unknown) => !cancelled && setError(errorMessage(err)),
    )
    return () => {
      cancelled = true
    }
  }, [ext, view, version])

  // The document is built once per load: later data and theme changes go over the bridge.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const doc = useMemo(() => (html === null ? null : webviewDocument(html, { view, key: tabKey, data, theme: currentTheme() })), [html])

  useEffect(() => {
    const send = (m: Record<string, unknown>) => frame.current?.contentWindow?.postMessage({ ...m, flint: 1 }, '*')
    const handle = (m: FrameMessage) => {
      switch (m.type) {
        case 'request':
          API.WebviewRequest(ext, view, tabKey, m.method, m.params as never).then(
            (result) => send({ type: 'response', id: m.id, result }),
            (err: unknown) => send({ type: 'response', id: m.id, error: errorMessage(err) }),
          )
          return
        case 'openTab':
          useEditor.getState().openContributed(m.view, m.key, m.title, m.data)
          return
        case 'setTitle':
          if (tabId) useEditor.getState().retitleTab(tabId, m.title.slice(0, LIMITS.title))
          return
        case 'setDirty':
          if (tabId) useEditor.getState().setDirty(tabId, m.dirty)
          return
        case 'setData':
          if (tabId) useEditor.getState().setTabData(tabId, m.data)
          return
        case 'runCommand':
          API.Execute(ext, m.id, m.args as never).catch((err: unknown) => showToast(errorMessage(err), 'error'))
          return
        case 'copy':
          navigator.clipboard.writeText(m.text).then(
            () => showToast(`Copied ${m.text.length > 60 ? `${m.text.slice(0, 57)}…` : m.text}`),
            (err: unknown) => showToast(`Copy failed: ${errorMessage(err)}`, 'error'),
          )
          return
        case 'showMessage':
          showToast(`${info?.name ?? ext}: ${m.text}`, m.level === 'error' ? 'error' : 'info')
          return
        case 'openFile':
          if (info?.permissions.includes('workspace.read')) {
            useEditor.getState().openFile(m.path, m.line ? { line: m.line, from: 0, to: 0 } : undefined)
          }
          return
        case 'key':
          // Into Flint's keymap (App's capture listener on window), as if pressed here.
          frame.current?.dispatchEvent(
            new KeyboardEvent('keydown', { key: m.key, code: m.code, metaKey: m.meta, ctrlKey: m.ctrl, altKey: m.alt, shiftKey: m.shift, bubbles: true, cancelable: true }),
          )
          return
      }
    }
    const onMessage = (e: MessageEvent) => {
      if (!frame.current || e.source !== frame.current.contentWindow) return
      const m = parseFrameMessage(e.data, ext)
      if (m) handle(m)
    }
    window.addEventListener('message', onMessage)
    const off = subscribeWebview(ext, view, tabKey, (message) => send({ type: 'message', message }))
    return () => {
      window.removeEventListener('message', onMessage)
      off()
    }
  }, [ext, view, tabKey, tabId, info])

  // A theme change reaches an open frame live.
  useEffect(() => {
    frame.current?.contentWindow?.postMessage({ flint: 1, type: 'theme', theme: currentTheme() }, '*')
  }, [theme])

  useEffect(() => {
    API.SetWebviewVisible(ext, view, tabKey, active).catch(() => {})
    return () => void API.SetWebviewVisible(ext, view, tabKey, false).catch(() => {})
  }, [ext, view, tabKey, active])

  if (error) return <div className="placeholder error selectable">{error}</div>
  if (doc === null) return <div className="placeholder muted">Loading…</div>
  return (
    <iframe
      ref={frame}
      className="extension-webview"
      title={`${info?.name ?? ext}: ${view}`}
      sandbox={WEBVIEW_SANDBOX}
      allow="clipboard-write"
      referrerPolicy="no-referrer"
      srcDoc={doc}
    />
  )
}
