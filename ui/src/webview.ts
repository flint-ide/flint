// Types and helpers for extension webview pages (site/extensions/protocol.md ▸
// Webviews). Flint defines window.flint before the page's scripts run.
import { useEffect, useState } from 'react'
import { applyTheme } from './theme'

export type FlintWebviewTheme = {
  /** Flint's theme variables (--bg-editor, --fg, --accent…), also set on :root. */
  vars: Record<string, string>
  dark: boolean
}

export type FlintWebview = {
  request<T = unknown>(method: string, params?: unknown): Promise<T>
  onMessage(fn: (message: unknown) => void): () => void
  readonly view: string
  readonly key: string
  readonly data: unknown
  readonly theme: FlintWebviewTheme
  onTheme(fn: (theme: FlintWebviewTheme) => void): () => void
  openTab(view: string, key: string, title: string, data?: unknown): void
  setTitle(title: string): void
  setDirty(dirty: boolean): void
  setData(data: unknown): void
  runCommand(id: string, args?: unknown): void
  copy(text: string): void
  showMessage(type: 'info' | 'warning' | 'error', text: string): void
  openFile(path: string, line?: number): void
}

declare global {
  interface Window {
    flint?: FlintWebview
  }
}

/** window.flint, or an error when the page runs outside Flint. */
export function flint(): FlintWebview {
  const api = window.flint
  if (!api) throw new Error('window.flint is missing: this page runs inside a Flint webview')
  return api
}

/**
 * Keeps WunderUI's dark/light tokens in step with Flint's theme (Flint sets
 * data-theme and the variables itself, not the .dark class). Call once at startup;
 * returns the unsubscribe function, a no-op outside Flint.
 */
export function syncFlintTheme(): () => void {
  const api = window.flint
  if (!api) return () => undefined
  applyTheme(api.theme.dark ? 'dark' : 'light')
  return api.onTheme((t) => applyTheme(t.dark ? 'dark' : 'light'))
}

/** Messages the extension's process posts to this view (webview/post), newest last, at most `limit`. */
export function useFlintMessages<T = unknown>(limit = 100): readonly T[] {
  const [messages, setMessages] = useState<readonly T[]>([])
  useEffect(() => {
    const api = window.flint
    if (!api) return undefined
    return api.onMessage((m) => setMessages((all) => [...all, m as T].slice(-limit)))
  }, [limit])
  return messages
}
