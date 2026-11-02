// Routes webview/post messages (Go's extensions:webview event) to the open frames
// of a view, and opens extension tab views. Framework-free.

type Listener = (message: unknown) => void

const frames = new Map<string, Set<{ key: string; fn: Listener }>>()
const viewKey = (ext: string, view: string) => `${ext}\n${view}`

/** Registers a frame of a view (key '' for a panel); returns the unregister. */
export function subscribeWebview(ext: string, view: string, key: string, fn: Listener): () => void {
  const id = viewKey(ext, view)
  const entry = { key, fn }
  const set = frames.get(id) ?? new Set()
  set.add(entry)
  frames.set(id, set)
  return () => {
    set.delete(entry)
    if (set.size === 0) frames.delete(id)
  }
}

/** Delivers a process's message: to the tab with that key, or (key '') to every frame of the view. */
export function deliverWebview(e: { extension: string; view: string; key?: string; message: unknown }): number {
  let n = 0
  for (const f of frames.get(viewKey(e.extension, e.view)) ?? []) {
    if (!e.key || f.key === e.key) {
      f.fn(e.message)
      n++
    }
  }
  return n
}

/** The editor tab id of an extension tab view's tab (lib: openContributed). */
export const webviewTabId = (view: string, key: string) => `contrib:${view}:${key}`

/** The key of a contributed tab, from its id (view ids never contain ':'). */
export function tabKeyOf(tab: { id: string; view: string }): string {
  const prefix = `contrib:${tab.view}:`
  return tab.id.startsWith(prefix) ? tab.id.slice(prefix.length) : ''
}
