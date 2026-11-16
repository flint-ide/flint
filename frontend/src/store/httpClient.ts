import { createStore } from '../lib/store'
import { Workspace, errorMessage } from '../api/backend'
import * as HTTP from '../../bindings/flint/httpapi'
import { httpfile, main } from '../wails/models'
import { ClipboardSetText, EventsOn } from '../wails/runtime'
import { HTTP_FILE_TEMPLATE, envDir, freeHttpFileName, isHttpFile, suggestedFileName } from '../lib/httpFiles'
import { rootOf } from '../lib/workspaceRoots'
import { httpTabId, useEditor } from './editor'
import { useWorkspace } from './workspace'
import { activeRoot } from './activeRoot'
import { activeFilePath } from './gitHistory'
import { openContextMenu } from './contextMenu'
import { showToast } from './dialog'

// HTTP client for .http / .rest files (IntelliJ's HTTP Client): requests go out only
// from Send (⌘↵ / ▶ / the palette), through Go. Responses stay in memory: the shown
// one here, a bounded history in Go.

export type HttpResponse = httpfile.Response

/** What a file's response tab shows. */
export interface HttpView {
  /** The request in flight (its id cancels it). */
  pending: { id: string; label: string; startedAt: number } | null
  response: HttpResponse | null
  error: string | null
  /** Kept responses of the file, newest first, without bodies. */
  history: HttpResponse[]
}

interface HttpClientState {
  /** By .http file path. */
  files: Record<string, HttpView>
  /** Selected environment by the folder holding the environment files ("" = none). */
  envs: Record<string, string>
  followRedirects: boolean
}

export const useHttpClient = createStore<HttpClientState>(() => ({ files: {}, envs: {}, followRedirects: true }))

const EMPTY: HttpView = { pending: null, response: null, error: null, history: [] }

function update(path: string, patch: Partial<HttpView>): void {
  useHttpClient.setState((s) => ({ files: { ...s.files, [path]: { ...(s.files[path] ?? EMPTY), ...patch } } }))
}

const viewOf = (path: string): HttpView => useHttpClient.getState().files[path] ?? EMPTY

/** The environment selected for a request file ("" = none). */
export function envOf(path: string): string {
  return useHttpClient.getState().envs[envDir(path)] ?? ''
}

export function selectEnvironment(path: string, env: string): void {
  useHttpClient.setState((s) => ({ envs: { ...s.envs, [envDir(path)]: env } }))
}

export function setFollowRedirects(follow: boolean): void {
  useHttpClient.setState({ followRedirects: follow })
}

const sendOptions = () => main.SendOptions.createFrom({ followRedirects: useHttpClient.getState().followRedirects, timeoutMs: 0 })
const targetOf = (path: string, text: string, line: number) => httpfile.Target.createFrom({ path, text, line, env: envOf(path) })

/**
 * Sends the request whose block contains line (1-based) of text, the editor content
 * of the file at path, and shows the response in the file's response tab.
 */
export async function sendRequest(path: string, text: string, line: number, label: string): Promise<void> {
  const previous = viewOf(path).pending
  if (previous) void HTTP.Cancel(previous.id) // one request per file at a time
  const id = crypto.randomUUID()
  update(path, { pending: { id, label, startedAt: Date.now() }, error: null })
  useEditor.getState().openHttpResponse(path)
  const current = () => viewOf(path).pending?.id === id
  try {
    const response = await HTTP.Send(id, targetOf(path, text, line), sendOptions())
    const history = await HTTP.History(path).catch(() => viewOf(path).history)
    if (current()) update(path, { pending: null, response, history })
  } catch (err) {
    if (current()) update(path, { pending: null, error: errorMessage(err) })
  }
}

export function cancelRequest(path: string): void {
  const pending = viewOf(path).pending
  if (!pending) return
  void HTTP.Cancel(pending.id)
  update(path, { pending: null, error: 'Request cancelled' })
}

/** Shows a response from the file's history. */
export async function showResponse(path: string, id: string): Promise<void> {
  try {
    update(path, { response: await HTTP.Response(id), error: null })
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/**
 * A response an agent's send_http_request tool got (Go: eventHTTPAgent): the
 * file's response tab shows it, without taking the focus, and its history
 * lists it. A request the user is sending for the file stays in front.
 */
async function showAgentResponse(path: string, id: string): Promise<void> {
  try {
    const [response, history] = await Promise.all([HTTP.Response(id), HTTP.History(path)])
    if (viewOf(path).pending) return update(path, { history })
    update(path, { response, history, error: null })
    const name = path.slice(path.lastIndexOf('/') + 1)
    useEditor.getState().addTab({ id: httpTabId(path), kind: 'http', path, title: `Response: ${name}`, dirty: false })
  } catch {
    // The response is no longer kept: nothing to show.
  }
}

let subscribed = false
/** Listens for responses agents got through Flint (once, at startup). */
export function subscribeHttpEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('http:agent-response', (ev: { file?: unknown; id?: unknown }) => {
    if (typeof ev?.file === 'string' && typeof ev.id === 'string' && isHttpFile(ev.file)) void showAgentResponse(ev.file, ev.id)
  })
}

async function copy(text: string, what: string): Promise<void> {
  if (await ClipboardSetText(text)) showToast(`Copied ${what}`)
  else showToast('Could not copy to the clipboard', 'error')
}

/** Copies the request behind a response as a curl command. */
export async function copyResponseAsCurl(id: string): Promise<void> {
  try {
    await copy(await HTTP.CurlOf(id), 'as curl')
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Copies the request at line of a file as a curl command, without sending it. */
export async function copyRequestAsCurl(path: string, text: string, line: number): Promise<void> {
  try {
    await copy(await HTTP.Curl(targetOf(path, text, line), sendOptions()), 'as curl')
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** Saves a response's whole body (also when the tab shows only its start). */
export async function saveResponseBody(r: HttpResponse): Promise<void> {
  try {
    const saved = await HTTP.SaveBody(r.id, suggestedFileName(r.finalUrl || r.url, r.contentType, r.kind))
    if (saved) showToast(`Saved ${saved}`)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export async function clearCookies(path: string): Promise<void> {
  await HTTP.ClearCookies(path)
  showToast('Cookies of this file cleared')
}

/** Menu of the environments next to a request file, at x/y (screen pixels). */
export async function pickEnvironment(path: string, x: number, y: number): Promise<void> {
  let names: string[]
  try {
    names = (await HTTP.Environments(path)) ?? []
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return
  }
  const selected = envOf(path)
  openContextMenu(x, y, [
    { type: 'section', label: 'HTTP environment' },
    { label: 'No environment', checked: selected === '', onSelect: () => selectEnvironment(path, '') },
    ...names.map((name) => ({ label: name, checked: name === selected, onSelect: () => selectEnvironment(path, name) })),
    ...(names.length === 0 ? [{ label: 'Define environments in http-client.env.json', disabled: true, onSelect: () => undefined }] : []),
  ])
}

/** Creates requests.http (or requests-N.http) next to the active file, else in the active project, and opens it. */
export async function newHttpFile(): Promise<void> {
  const { roots } = useWorkspace.getState()
  const file = activeFilePath()
  const dir = file && rootOf(roots, file) ? envDir(file) : activeRoot()
  if (!dir) return showToast('Open a folder first')
  try {
    const taken = new Set((await Workspace.ListDir(dir)).map((e) => e.name))
    const path = await Workspace.CreateFile(dir, freeHttpFileName(taken))
    await Workspace.WriteFile(path, HTTP_FILE_TEMPLATE)
    useEditor.getState().openFile(path, { line: 7, from: 0, to: 0 })
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

// A file's responses belong to its response tab: forget them when it closes (the
// request in flight is cancelled), so closed files don't pile up here.
useEditor.subscribe((s, prev) => {
  if (s.tabs === prev.tabs) return
  const files = useHttpClient.getState().files
  const open = new Set(s.tabs.flatMap((t) => (t.kind === 'http' ? [t.path] : [])))
  const gone = Object.keys(files).filter((p) => !open.has(p))
  if (gone.length === 0) return
  for (const p of gone) {
    const pending = files[p].pending
    if (pending) void HTTP.Cancel(pending.id)
  }
  useHttpClient.setState({ files: Object.fromEntries(Object.entries(files).filter(([p]) => open.has(p))) })
})

/** Whether the active tab is a request file. */
export function activeHttpFile(): string | null {
  const path = activeFilePath()
  return path && isHttpFile(path) ? path : null
}
