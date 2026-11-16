// Editor decorations of external extensions: the link between the editors
// (lib/editorDecorations.ts) and Go's extension host. Go decides which
// extension hears of which file (a decoration set that is on and matches, in
// a trusted folder, never .git or a credential file); this side reports the
// documents Go is interested in and draws what Go passes on.
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import { EventsOn } from '../../wails/runtime'
import { clearDecorations, openDocuments, publishDecorations, watchDocuments, type OpenDocument } from '../../lib/editorDecorations'
import { coerceDecorations, type QuickFixRequest, type TextEdit } from '../../lib/decorationModel'

/** Go passes a document's text on only up to this size (host.maxEditorText). */
export const MAX_TEXT = 1 << 20

/** The documents Go follows, and whether it wants their text. */
const tracked = new Map<string, { text: boolean }>()
/** Calls to Go, one after another per path, so they arrive in order. */
const queues = new Map<string, Promise<void>>()

function enqueue(path: string, fn: () => Promise<unknown>): void {
  const next = (queues.get(path) ?? Promise.resolve()).then(fn).then(
    () => undefined,
    () => undefined, // decorations are best-effort: a failed report is retried with the next change
  )
  queues.set(path, next)
  void next.then(() => {
    if (queues.get(path) === next) queues.delete(path)
  })
}

const isOpen = (path: string) => openDocuments().some((d) => d.path === path)

/** What EditorOpened and EditorChanged get: the text only when Go wants it and it fits. */
export function textFor(doc: OpenDocument, wanted: boolean): [string, boolean] {
  if (!wanted) return ['', false]
  const text = doc.text()
  return text.length > MAX_TEXT ? ['', false] : [text, true]
}

/** Asks Go whether anyone decorates the document, and reports it or takes it back accordingly. */
async function sync(doc: OpenDocument): Promise<void> {
  if (!isOpen(doc.path)) return
  const interest = await API.EditorInterest(doc.path)
  if (!isOpen(doc.path)) return
  if (interest.tracked) {
    tracked.set(doc.path, { text: interest.text })
    await API.EditorOpened(doc.path, doc.version, ...textFor(doc, interest.text))
  } else if (tracked.delete(doc.path)) {
    await API.EditorClosed(doc.path)
  }
}

/** Reports every open document again: the extensions, their sets or the trusted folders changed. */
export function resyncDocuments(): void {
  for (const doc of openDocuments()) enqueue(doc.path, () => sync(doc))
}

interface DecorationsEvent {
  extension: string
  set: string
  path: string
  version: number
  decorations: unknown
}

interface ClearEvent {
  extension: string
  set?: string
  path?: string
}

/** The quick fix of an extension's decoration: its edits, from Go. */
export async function quickFix(ext: string, req: QuickFixRequest): Promise<TextEdit[]> {
  const res = await API.EditorAction(ext, req)
  return res.version === req.version ? (res.edits ?? []) : []
}

/** Starts reporting documents and drawing extensions' decorations; returns the stop. */
export function subscribeDocuments(): () => void {
  const offs = [
    watchDocuments({
      opened: (doc) => enqueue(doc.path, () => sync(doc)),
      changed: (doc) =>
        enqueue(doc.path, async () => {
          const t = tracked.get(doc.path)
          if (t && isOpen(doc.path)) await API.EditorChanged(doc.path, doc.version, ...textFor(doc, t.text))
        }),
      closed: (path) =>
        enqueue(path, async () => {
          if (tracked.delete(path)) await API.EditorClosed(path)
        }),
    }),
    EventsOn('extensions:decorations', (e: DecorationsEvent) => {
      // Only a set of the extension that sent it.
      if (e.set.startsWith(`${e.extension}.`)) publishDecorations(e.set, e.path, e.version, coerceDecorations(e.decorations))
    }),
    EventsOn('extensions:decorationsCleared', (e: ClearEvent) =>
      clearDecorations((set) => (e.set ? set === e.set : true) && set.startsWith(`${e.extension}.`), e.path || undefined),
    ),
  ]
  return () => {
    offs.forEach((off) => off())
    for (const path of tracked.keys()) void API.EditorClosed(path)
    tracked.clear()
  }
}
