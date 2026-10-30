import { createStore } from '../lib/store'
import * as RunAPI from '../../wailsjs/go/main/RunAPI'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { runconfig, testrun } from '../../wailsjs/go/models'
import { Workspace, errorMessage } from '../api/backend'
import { createReorderer } from '../lib/reorder'
import {
  applyNodes, emptyTree, failedNodes, nodeRequest, rerunFailedRequest, statusOf,
  type TestNode, type TestRequest, type TestTree,
} from '../lib/runTree'
import { testLang, type TestGutterHooks, type TestMark } from '../lib/testFiles'
import { resolvePath } from '../lib/pathLinks'
import { showToast } from './dialog'
import { useEditor, viewFor } from './editor'
import { onFileChanged } from './fsEvents'
import { decodeBase64 } from './terminal'
import { useWorkspace } from './workspace'
import { askAgent } from '../lib/askAgent'
import { failedTestsPrompt } from '../lib/agentPrompt'

// Run configurations (.flint/run.json + detected ones) and test runs. Every
// action is a plain exported function so the toolbar, shortcuts, the editor
// gutter, a command palette or a "fix failed tests with the agent" button can
// all drive the same thing. Running processes are never part of session
// restore; only the selected configuration is remembered (store/session.ts).

export type RunConfig = runconfig.Config
export type RunType = 'command' | 'go' | 'npm' | 'make'

/** How a run ended (runner.Result). */
export interface RunExit {
  code: number
  signal?: string
  stopped: boolean
  elapsedMs: number
  error?: string
}

/** One run tab: a configuration or the test runner, with its latest process. */
export interface RunSession {
  key: string
  /** Current process; a rerun gets a new id so late events of the old one are dropped. */
  runId: string
  kind: 'config' | 'tests'
  title: string
  config?: RunConfig
  request?: TestRequest
  /** Directories relative paths in the console are tried against. */
  bases: string[]
  running: boolean
  exit: RunExit | null
  tests: TestTree
  startedAt: number
}

/** A failed test, as handed to an agent ("Fix with agent"). */
export interface FailedTest {
  name: string
  lang: TestRequest['lang']
  output: string
  dir?: string
  location?: TestNode['location']
}

interface RunState {
  root: string | null
  saved: RunConfig[]
  detected: RunConfig[]
  capabilities: Record<string, runconfig.Capabilities>
  /** Why .flint/run.json could not be used. */
  loadError: string
  selectedId: string | null
  sessions: Record<string, RunSession>
  /** Session that Stop / Rerun act on when none is given. */
  lastKey: string | null
  /** Open configuration dialog: the draft and the id it replaces (null: new). */
  editing: { config: RunConfig; replaces: string | null } | null
}

export const TESTS_KEY = 'tests'
export const runTabId = (key: string) => `run:${key}`
const configKey = (id: string) => `cfg:${id}`

export const useRun = createStore<RunState>(() => ({
  root: null,
  saved: [],
  detected: [],
  capabilities: {},
  loadError: '',
  selectedId: null,
  sessions: {},
  lastKey: null,
  editing: null,
}))

// ---------- configurations ----------

export function allConfigs(s: RunState = useRun.getState()): RunConfig[] {
  return [...s.saved, ...s.detected]
}

export const isDetected = (c: RunConfig) => c.id.startsWith('detected:')

/** The selected configuration, falling back to the first one. */
export function selectedRunConfig(s: RunState = useRun.getState()): RunConfig | null {
  const all = allConfigs(s)
  return all.find((c) => c.id === s.selectedId) ?? all[0] ?? null
}

export function selectRunConfig(id: string | null): void {
  useRun.setState({ selectedId: id })
}

export async function loadRunConfigs(root: string | null = useWorkspace.getState().root): Promise<void> {
  if (!root) {
    useRun.setState({ root: null, saved: [], detected: [], capabilities: {}, loadError: '' })
    return
  }
  try {
    const res = await RunAPI.Configs(root)
    if (useWorkspace.getState().root !== root) return
    useRun.setState({
      root,
      saved: res.saved ?? [],
      detected: res.detected ?? [],
      capabilities: res.capabilities ?? {},
      loadError: res.error,
    })
  } catch (err) {
    showToast(`Run configurations: ${errorMessage(err)}`, 'error')
  }
}

/** Opens the dialog for config (default: the selected one). Detected ones are saved as new. */
export function editRunConfig(config: RunConfig | null = selectedRunConfig()): void {
  if (!config) return newRunConfig()
  useRun.setState({ editing: { config: runconfig.Config.createFrom(config), replaces: isDetected(config) ? null : config.id } })
}

export function newRunConfig(type: RunType = 'command'): void {
  useRun.setState({ editing: { config: runconfig.Config.createFrom({ id: '', name: '', type }), replaces: null } })
}

export function closeRunConfigDialog(): void {
  useRun.setState({ editing: null })
}

/** Writes config into .flint/run.json (replacing `replaces`) and selects it. Throws on invalid input. */
export async function saveRunConfig(config: RunConfig, replaces: string | null): Promise<RunConfig> {
  const { root, saved } = useRun.getState()
  if (!root) throw new Error('No project open')
  const list = replaces !== null && saved.some((c) => c.id === replaces)
    ? saved.map((c) => (c.id === replaces ? config : c))
    : [...saved, config]
  const index = replaces !== null ? Math.max(0, list.indexOf(config)) : list.length - 1
  const result = (await RunAPI.SaveConfigs(root, list)) ?? []
  const stored = result[index] ?? result[result.length - 1]
  useRun.setState({ saved: result, selectedId: stored?.id ?? null })
  void loadRunConfigs(root) // capabilities and detection
  return stored
}

export async function deleteRunConfig(id: string): Promise<void> {
  const { root, saved, selectedId } = useRun.getState()
  if (!root) return
  const result = (await RunAPI.SaveConfigs(root, saved.filter((c) => c.id !== id))) ?? []
  useRun.setState({ saved: result, selectedId: selectedId === id ? null : selectedId })
}

// ---------- running ----------

/** ▶ / ⌃R: runs (or restarts) the selected configuration. */
export async function runSelected(): Promise<void> {
  const config = selectedRunConfig()
  if (!config) return newRunConfig()
  await runConfiguration(config)
}

export async function runConfiguration(config: RunConfig): Promise<void> {
  const root = useWorkspace.getState().root
  if (!root) return
  useRun.setState({ selectedId: config.id })
  await startSession(
    configKey(config.id),
    { kind: 'config', title: config.name, config, bases: [resolvePath(root, config.cwd || '.')] },
    async (runId, cols, rows) => {
      await RunAPI.Start(runId, root, runconfig.Config.createFrom(config), cols, rows)
      return config.name
    },
  )
}

/** Runs tests in the Tests tab (replacing the previous test run). */
export async function runTests(req: TestRequest): Promise<void> {
  const root = useWorkspace.getState().root ?? ''
  await startSession(
    TESTS_KEY,
    { kind: 'tests', title: 'Tests', request: req, bases: [...req.dirs, root].filter(Boolean) },
    (runId) => RunAPI.StartTests(runId, testrun.Request.createFrom(req)),
  )
}

const dirname = (p: string) => p.slice(0, Math.max(p.lastIndexOf('/'), 1))

/** Runs one test (full name as in the gutter / results tree) of a test file. */
export function runTestAt(path: string, name: string): Promise<void> {
  const lang = testLang(path)
  if (!lang) return Promise.resolve()
  return runTests({ lang, dirs: [dirname(path)], file: lang === 'js' ? path : undefined, tests: [name] })
}

export function runTestsInFile(path: string): Promise<void> {
  const lang = testLang(path)
  if (!lang) return Promise.resolve()
  return runTests({ lang, dirs: [dirname(path)], file: path })
}

/** Go: the file's package; JS: every test of the file's package.json project. */
export function runTestsInPackage(path: string): Promise<void> {
  const lang = testLang(path)
  if (!lang) return Promise.resolve()
  return runTests({ lang, dirs: [dirname(path)] })
}

export function rerunFailedTests(): Promise<void> {
  const session = useRun.getState().sessions[TESTS_KEY]
  const req = session?.request && rerunFailedRequest(session.tests, session.request)
  if (!req) {
    showToast('No failed tests to rerun')
    return Promise.resolve()
  }
  return runTests(req)
}

/** Reruns one node of the results tree (a package, suite or test). */
export function runTestNode(node: TestNode): Promise<void> {
  const req = useRun.getState().sessions[TESTS_KEY]?.request
  return req ? runTests(nodeRequest(node, req)) : Promise.resolve()
}

/** The failures of the last test run, innermost first (for "Fix with agent"). */
export function failedTests(): FailedTest[] {
  const session = useRun.getState().sessions[TESTS_KEY]
  if (!session?.request) return []
  const lang = session.request.lang
  return failedNodes(session.tests).map((n) => ({ name: n.test ?? n.name, lang, output: n.output ?? '', dir: n.dir, location: n.location }))
}

/** Hands the last run's failures to the agent chat (draft, or sent right away). */
export async function fixFailedTestsWithAgent(send = false): Promise<void> {
  const failed = failedTests()
  if (failed.length === 0) return showToast('No failed tests')
  const root = useWorkspace.getState().root ?? ''
  const attachments = failed.flatMap((t) => (t.location ? [{ path: t.location.path, startLine: t.location.line, endLine: t.location.line }] : []))
  await askAgent({ prompt: failedTestsPrompt(failed, root), attachments, send })
}

/** ⌃⇧R: the test at the cursor in a test file, else the selected configuration. */
export async function runContext(): Promise<void> {
  const { tabs, activeId } = useEditor.getState()
  const tab = tabs.find((t) => t.id === activeId)
  if (tab?.kind !== 'file' || !testLang(tab.path)) return runSelected()
  const view = viewFor(tab.id)
  if (!view) return runTestsInFile(tab.path)
  const line = view.state.doc.lineAt(view.state.selection.main.head).number
  const marks = ((await RunAPI.TestMarks(tab.path, view.state.doc.toString()).catch(() => [])) ?? []) as TestMark[]
  const at = marks.filter((m) => m.name && m.line <= line).pop()
  return at ? runTestAt(tab.path, at.name) : runTestsInFile(tab.path)
}

/** Stops a session (default: the last one used). */
export async function stopRun(key: string | null = currentKey()): Promise<void> {
  const session = key ? useRun.getState().sessions[key] : undefined
  if (session?.running) await RunAPI.Stop(session.runId)
}

/** Runs a session again with what it ran last (a configuration edited meanwhile runs as saved). */
export async function rerun(key: string | null = currentKey()): Promise<void> {
  const session = key ? useRun.getState().sessions[key] : undefined
  if (!session) return runSelected()
  if (session.kind === 'tests' && session.request) return runTests(session.request)
  if (session.config) {
    const latest = allConfigs().find((c) => c.id === session.config!.id) ?? session.config
    return runConfiguration(latest)
  }
}

/** Closes a run tab, stopping its process. */
export async function closeRunSession(key: string): Promise<void> {
  const session = useRun.getState().sessions[key]
  if (session?.running) await RunAPI.Stop(session.runId).catch(() => undefined)
  if (session) dropRun(session.runId)
  consoles.delete(key)
  backlog.delete(key)
  sizes.delete(key)
  useRun.setState((s) => {
    const { [key]: _, ...sessions } = s.sessions
    return { sessions, lastKey: s.lastKey === key ? null : s.lastKey }
  })
  useEditor.getState().closeTab(runTabId(key))
}

/** The session Stop / Rerun target: the active run tab, else the last started one. */
export function currentKey(): string | null {
  const { activeId } = useEditor.getState()
  const { sessions, lastKey } = useRun.getState()
  const active = activeId?.startsWith('run:') ? activeId.slice(4) : null
  return active && sessions[active] ? active : lastKey
}

/** Process ids → the session (key) they belong to. */
const runKeys = new Map<string, string>()

/** Forgets a process (tab closed or rerun): its late events are dropped. */
function dropRun(runId: string): void {
  runKeys.delete(runId)
  pendingNodes.delete(runId)
  inputs.delete(runId)
  deliver.forget(runId)
}

async function startSession(
  key: string,
  base: Pick<RunSession, 'kind' | 'title' | 'config' | 'request' | 'bases'>,
  start: (runId: string, cols: number, rows: number) => Promise<string>,
): Promise<void> {
  const prev = useRun.getState().sessions[key]
  if (prev?.running) await RunAPI.Stop(prev.runId).catch(() => undefined)
  if (prev) dropRun(prev.runId)
  const runId = crypto.randomUUID()
  runKeys.set(runId, key)
  resetConsole(key)
  const session: RunSession = { key, runId, running: true, exit: null, tests: emptyTree, startedAt: Date.now(), ...base }
  useRun.setState((s) => ({ sessions: { ...s.sessions, [key]: session }, lastKey: key }))
  notifyResults()
  useEditor.getState().openRun(key, base.title)
  const size = sizes.get(key) ?? { cols: 120, rows: 30 }
  try {
    const title = await start(runId, size.cols, size.rows)
    if (title && title !== base.title) updateSession(key, runId, () => ({ title }))
    if (title) useEditor.getState().openRun(key, title, true)
  } catch (err) {
    const message = errorMessage(err)
    writeConsole(key, new TextEncoder().encode(`\x1b[31m${message}\x1b[0m\r\n`))
    updateSession(key, runId, () => ({ running: false, exit: { code: -1, stopped: false, elapsedMs: 0, error: message } }))
    showToast(message, 'error')
  }
}

/** Updates a session only while runId is still its current process. */
function updateSession(key: string, runId: string, patch: (s: RunSession) => Partial<RunSession>): void {
  useRun.setState((s) => {
    const session = s.sessions[key]
    if (!session || session.runId !== runId) return {}
    return { sessions: { ...s.sessions, [key]: { ...session, ...patch(session) } } }
  })
}

// ---------- console ----------

export interface RunConsole {
  write: (data: Uint8Array) => void
  reset: () => void
}

const consoles = new Map<string, RunConsole>()
const backlog = new Map<string, Uint8Array[]>()
const sizes = new Map<string, { cols: number; rows: number }>()
const MAX_BACKLOG_CHUNKS = 2000

/** A mounted run view registers its xterm; output from before is replayed. */
export function attachConsole(key: string, sink: RunConsole): () => void {
  consoles.set(key, sink)
  for (const chunk of backlog.get(key) ?? []) sink.write(chunk)
  backlog.delete(key)
  return () => {
    if (consoles.get(key) === sink) consoles.delete(key)
  }
}

function writeConsole(key: string, data: Uint8Array): void {
  const sink = consoles.get(key)
  if (sink) return sink.write(data)
  // Appended in place (a private buffer, not store state): copying it per chunk made a
  // long run without a mounted console quadratic.
  const pending = backlog.get(key)
  if (!pending) backlog.set(key, [data])
  else if (pending.push(data) > MAX_BACKLOG_CHUNKS) pending.shift()
}

function resetConsole(key: string): void {
  backlog.delete(key)
  consoles.get(key)?.reset()
}

export function setConsoleSize(key: string, cols: number, rows: number): void {
  sizes.set(key, { cols, rows })
  const session = useRun.getState().sessions[key]
  if (session?.running) void RunAPI.Resize(session.runId, cols, rows).catch(() => undefined)
}

interface InputQueue {
  pending: string
  inFlight: boolean
}
const inputs = new Map<string, InputQueue>()

/** Keystrokes for a running configuration, one write in flight at a time (like terminals). */
export function sendRunInput(key: string, data: string): void {
  const session = useRun.getState().sessions[key]
  if (!session?.running || session.kind !== 'config') return
  const q = inputs.get(session.runId) ?? { pending: '', inFlight: false }
  inputs.set(session.runId, q)
  q.pending += data
  if (!q.inFlight) void flushInput(session.runId, q)
}

async function flushInput(runId: string, q: InputQueue): Promise<void> {
  q.inFlight = true
  while (q.pending) {
    const chunk = q.pending
    q.pending = ''
    try {
      await RunAPI.Write(runId, chunk)
    } catch {
      q.pending = ''
    }
  }
  q.inFlight = false
}

/** Opens a path printed by a run, trying the session's directories for relative ones. */
export async function openRunLink(key: string, path: string, line?: number, column?: number, dir?: string): Promise<void> {
  const session = useRun.getState().sessions[key]
  const root = useWorkspace.getState().root ?? ''
  const dirs = new Set([dir ?? '', ...(session?.bases ?? []), ...Object.values(session?.tests.nodes ?? {}).flatMap((n) => (n.dir ? [n.dir] : [])), root])
  const candidates = path.startsWith('/') ? [path] : [...dirs].filter(Boolean).map((d) => resolvePath(d, path))
  for (const candidate of candidates) {
    if (await fileExists(candidate)) {
      useEditor.getState().openFile(candidate, line ? { line, from: Math.max(0, (column ?? 1) - 1), to: Math.max(0, (column ?? 1) - 1) } : undefined)
      return
    }
  }
  showToast(`${path} not found`, 'error')
}

async function fileExists(path: string): Promise<boolean> {
  const entries = await Workspace.ListDir(dirname(path)).catch(() => null)
  const name = path.slice(path.lastIndexOf('/') + 1)
  return !!entries?.some((e) => e.name === name && !e.isDir)
}

// ---------- test results for the editor gutter ----------

const resultListeners = new Set<() => void>()
let notifyQueued = false

/** Coalesced to one notification per frame while results stream in. */
function notifyResults(): void {
  if (notifyQueued) return
  notifyQueued = true
  requestAnimationFrame(() => {
    notifyQueued = false
    resultListeners.forEach((fn) => fn())
  })
}

export const testGutterHooks: TestGutterHooks = {
  run: (path, mark, alt) => {
    if (mark.kind === 'file') void (alt ? runTestsInPackage(path) : runTestsInFile(path))
    else void runTestAt(path, mark.name)
  },
  status: (path, name) => {
    const tree = useRun.getState().sessions[TESTS_KEY]?.tests
    return tree ? statusOf(tree, path, name) : undefined
  },
  onResults: (listener) => {
    resultListeners.add(listener)
    return () => resultListeners.delete(listener)
  },
}

// ---------- events ----------

interface RunEventMsg {
  id: string
  seq?: number
  data?: string
  tests?: TestNode[]
  exit?: RunExit
}

/**
 * Test nodes waiting for the next frame, by process id: a run reporting thousands of
 * tests updates the tree (and every view of it) once per frame, not once per event.
 */
const pendingNodes = new Map<string, { key: string; nodes: TestNode[] }>()
let nodesQueued = false

function queueNodes(key: string, runId: string, nodes: TestNode[]): void {
  const pending = pendingNodes.get(runId)
  if (pending) for (const n of nodes) pending.nodes.push(n)
  else pendingNodes.set(runId, { key, nodes: [...nodes] })
  if (nodesQueued) return
  nodesQueued = true
  requestAnimationFrame(() => {
    nodesQueued = false
    for (const id of [...pendingNodes.keys()]) flushNodes(id)
  })
}

function flushNodes(runId: string): void {
  const pending = pendingNodes.get(runId)
  if (!pending) return
  pendingNodes.delete(runId)
  updateSession(pending.key, runId, (s) => ({ tests: applyNodes(s.tests, pending.nodes) }))
  notifyResults()
}

function applyRunEvent(runId: string, ev: RunEventMsg): void {
  const key = runKeys.get(runId) // undefined: closed tab or replaced by a rerun
  if (key && ev.data) writeConsole(key, decodeBase64(ev.data))
  if (key && ev.tests?.length) queueNodes(key, runId, ev.tests)
  if (!ev.exit) return
  // The exit is the process's last event: settle its results first, then forget it.
  flushNodes(runId)
  if (key) updateSession(key, runId, () => ({ running: false, exit: ev.exit! }))
  inputs.delete(runId)
  deliver.forget(runId)
}

const deliver = createReorderer<RunEventMsg>(applyRunEvent)

let subscribed = false
export function subscribeRunEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('run:event', (ev: RunEventMsg) => deliver(ev.id, ev))

  // Configurations follow the open project and edits of .flint/run.json.
  let unwatch: (() => void) | null = null
  const follow = (root: string | null) => {
    unwatch?.()
    unwatch = root ? onFileChanged(`${root}/.flint/run.json`, () => void loadRunConfigs(root)) : null
    void loadRunConfigs(root)
  }
  useWorkspace.subscribe((s, prev) => {
    if (s.root !== prev.root) follow(s.root)
  })
  follow(useWorkspace.getState().root)
}
