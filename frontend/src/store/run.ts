import { createStore } from '../lib/store'
import * as RunAPI from '../../bindings/flint/runapi'
import { EventsOn } from '../wails/runtime'
import { runconfig, testrun } from '../wails/models'
import { Workspace, errorMessage } from '../api/backend'
import { createReorderer } from '../lib/reorder'
import {
  applyNodes, countTests, emptyTree, failedNodes, nodeRequest, rerunFailedRequest, statusOf,
  type TestNode, type TestRequest, type TestTree,
} from '../lib/runTree'
import { testLang, type TestGutterHooks, type TestMark } from '../lib/testFiles'
import { extensionTestRunnerFor } from '../lib/extensionTools'
import { resolvePath } from '../lib/pathLinks'
import { showToast } from './dialog'
import { useEditor, viewFor } from './editor'
import { onFileChanged } from './fsEvents'
import { decodeBase64 } from './terminal'
import { useWorkspace } from './workspace'
import { followedRoot, rootFor } from './activeRoot'
import { askAgent } from '../lib/askAgent'
import { failedTestsPrompt } from '../lib/agentPrompt'
import { runSummary } from '../lib/alerts'
import { alertDone } from './alerts'
import { parseRunAdoption } from '../lib/runAdopt'

// Run configurations (.flint/run.json + detected ones) and test runs. Every
// action is a plain exported function so the toolbar, shortcuts, the editor
// gutter, a command palette or a "fix failed tests with the agent" button can
// all drive the same thing. Running processes are never part of session
// restore; only the selected configuration is remembered (store/session.ts).
// With several workspace folders the configurations shown are those of the active
// tab's folder (like the Commit panel); each folder keeps its own selection.

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
  /** Workspace folder a configuration runs in. */
  root?: string
  request?: TestRequest
  /** Directories relative paths in the console are tried against. */
  bases: string[]
  running: boolean
  exit: RunExit | null
  tests: TestTree
  startedAt: number
  /** Rerun of a custom test launch (profiling); plain runs rerun `request`. */
  again?: () => Promise<void>
}

/** A test run started some other way than RunAPI.StartTests (profiling, see store/profile.ts). */
export interface TestLaunch {
  /** Starts the process under runId; returns the run's title. */
  start: (runId: string) => Promise<string>
  /** Called once the process ended; null when it was replaced, closed or failed to start first. */
  onExit: (exit: RunExit | null) => void
  /** Starts the same launch again (Rerun). */
  again: () => Promise<void>
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
  /** Workspace folder whose configurations are loaded (follows the active tab's folder). */
  root: string | null
  saved: RunConfig[]
  detected: RunConfig[]
  capabilities: Record<string, runconfig.Capabilities>
  /** Why .flint/run.json could not be used. */
  loadError: string
  /** Selected configuration id per workspace folder. */
  selected: Record<string, string>
  sessions: Record<string, RunSession>
  /** Session that Stop / Rerun act on when none is given. */
  lastKey: string | null
  /** Open configuration dialog: the draft and the id it replaces (null: new). */
  editing: { config: RunConfig; replaces: string | null } | null
}

export const TESTS_KEY = 'tests'
export const runTabId = (key: string) => `run:${key}`
/** Session key of a configuration; ids are only unique within one folder. */
export const configKey = (root: string, id: string) => `cfg:${id}@${root}`

export const useRun = createStore<RunState>(() => ({
  root: null,
  saved: [],
  detected: [],
  capabilities: {},
  loadError: '',
  selected: {},
  sessions: {},
  lastKey: null,
  editing: null,
}))

// ---------- configurations ----------

export function allConfigs(s: RunState = useRun.getState()): RunConfig[] {
  return [...s.saved, ...s.detected]
}

export const isDetected = (c: RunConfig) => c.id.startsWith('detected:')

/** The selected configuration of the shown folder, falling back to the first one. */
export function selectedRunConfig(s: RunState = useRun.getState()): RunConfig | null {
  const all = allConfigs(s)
  const id = s.root ? s.selected[s.root] : undefined
  return all.find((c) => c.id === id) ?? all[0] ?? null
}

/** Selects id in root (default: the shown folder); null forgets the selection. */
export function selectRunConfig(id: string | null, root: string | null = useRun.getState().root): void {
  if (!root) return
  useRun.setState((s) => {
    if ((s.selected[root] ?? null) === id) return {}
    const { [root]: _, ...rest } = s.selected
    return { selected: id ? { ...rest, [root]: id } : rest }
  })
}

/** Selections restored from a session (folder → configuration id), on top of the current ones. */
export function restoreRunSelections(selected: Record<string, string>): void {
  useRun.setState((s) => ({ selected: { ...s.selected, ...selected } }))
}

type Loaded = Pick<RunState, 'saved' | 'detected' | 'capabilities' | 'loadError'>
const EMPTY: Loaded = { saved: [], detected: [], capabilities: {}, loadError: '' }
/** Last configurations per folder, so switching folders shows them before the reload. */
const loadedByRoot = new Map<string, Loaded>()
/** The folder configurations follow (see subscribeRunEvents). */
let following: string | null = null

export async function loadRunConfigs(root: string | null = following): Promise<void> {
  if (!root) {
    useRun.setState({ root: null, ...EMPTY })
    return
  }
  try {
    const res = await RunAPI.Configs(root)
    const loaded: Loaded = {
      saved: res.saved ?? [],
      detected: res.detected ?? [],
      capabilities: (res.capabilities ?? {}) as Loaded['capabilities'],
      loadError: res.error,
    }
    loadedByRoot.set(root, loaded)
    if (following === root) useRun.setState({ root, ...loaded })
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
  if (useRun.getState().root === root) useRun.setState({ saved: result })
  selectRunConfig(stored?.id ?? null, root)
  void loadRunConfigs(root) // capabilities and detection
  return stored
}

export async function deleteRunConfig(id: string): Promise<void> {
  const { root, saved, selected } = useRun.getState()
  if (!root) return
  const result = (await RunAPI.SaveConfigs(root, saved.filter((c) => c.id !== id))) ?? []
  if (useRun.getState().root === root) useRun.setState({ saved: result })
  if (selected[root] === id) selectRunConfig(null, root)
}

// ---------- running ----------

/** ▶ / ⌃R: runs (or restarts) the selected configuration. */
export async function runSelected(): Promise<void> {
  const config = selectedRunConfig()
  if (!config) return newRunConfig()
  await runConfiguration(config)
}

/** Runs config of root (default: the folder whose configurations are shown). */
export async function runConfiguration(config: RunConfig, root: string | null = useRun.getState().root): Promise<void> {
  if (!root) return
  selectRunConfig(config.id, root)
  await startSession(
    configKey(root, config.id),
    { kind: 'config', title: config.name, config, root, bases: [resolvePath(root, config.cwd || '.')] },
    async (runId, cols, rows) => {
      await RunAPI.Start(runId, root, runconfig.Config.createFrom(config), cols, rows)
      return config.name
    },
  )
}

/** Runs tests in the Tests tab (replacing the previous test run); launch: a custom start (profiling). */
export async function runTests(req: TestRequest, launch?: TestLaunch): Promise<void> {
  const root = rootFor(req.file ?? req.dirs[0])
  await startSession(
    TESTS_KEY,
    { kind: 'tests', title: 'Tests', request: req, bases: [...req.dirs, root].filter(Boolean), again: launch?.again },
    launch?.start ?? ((runId) => RunAPI.StartTests(runId, testrun.Request.createFrom(req))),
    launch?.onExit,
  )
}

const dirname = (p: string) => p.slice(0, Math.max(p.lastIndexOf('/'), 1))

/** A file whose tests an extension's test runner runs (Flint's own test files excluded). */
export const extensionTestFile = (path: string | null | undefined): path is string =>
  !!path && !testLang(path) && !!extensionTestRunnerFor(path)

/** An extension runner's request for path: the project folder, the file and maybe some tests. */
const extensionRequest = (path: string, tests?: string[]): TestRequest => ({ lang: 'ext', dirs: [rootFor(path) || dirname(path)], file: path, tests })

/** Runs one test (full name as in the gutter / results tree) of a test file. */
export function runTestAt(path: string, name: string): Promise<void> {
  const lang = testLang(path)
  if (!lang) return extensionTestFile(path) ? runTests(extensionRequest(path, [name])) : Promise.resolve()
  return runTests({ lang, dirs: [dirname(path)], file: lang === 'js' ? path : undefined, tests: [name] })
}

export function runTestsInFile(path: string): Promise<void> {
  const lang = testLang(path)
  if (!lang) return extensionTestFile(path) ? runTests(extensionRequest(path)) : Promise.resolve()
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
  const req = useRun.getState().sessions[TESTS_KEY]?.request
  const root = rootFor(req?.file ?? req?.dirs[0])
  const attachments = failed.flatMap((t) => (t.location ? [{ path: t.location.path, startLine: t.location.line, endLine: t.location.line }] : []))
  await askAgent({ prompt: failedTestsPrompt(failed, root), attachments, send, root })
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
  if (session.again) return session.again()
  if (session.kind === 'tests' && session.request) return runTests(session.request)
  if (session.config) {
    const shown = session.root === useRun.getState().root
    const latest = (shown && allConfigs().find((c) => c.id === session.config!.id)) || session.config
    return runConfiguration(latest, session.root)
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
/** Process ids → what to do once they ended (TestLaunch.onExit). */
const exitHooks = new Map<string, (exit: RunExit | null) => void>()

function settleExit(runId: string, exit: RunExit | null): void {
  const hook = exitHooks.get(runId)
  exitHooks.delete(runId)
  hook?.(exit)
}

/** Forgets a process (tab closed or rerun): its late events are dropped. */
function dropRun(runId: string): void {
  settleExit(runId, null)
  runKeys.delete(runId)
  pendingNodes.delete(runId)
  inputs.delete(runId)
  deliver.forget(runId)
}

async function startSession(
  key: string,
  base: Pick<RunSession, 'kind' | 'title' | 'config' | 'root' | 'request' | 'bases' | 'again'>,
  start: (runId: string, cols: number, rows: number) => Promise<string>,
  onExit?: (exit: RunExit | null) => void,
): Promise<void> {
  const prev = useRun.getState().sessions[key]
  if (prev?.running) await RunAPI.Stop(prev.runId).catch(() => undefined)
  if (prev) dropRun(prev.runId)
  const runId = crypto.randomUUID()
  runKeys.set(runId, key)
  if (onExit) exitHooks.set(runId, onExit)
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
    settleExit(runId, null)
    showToast(message, 'error')
  }
}

/**
 * Shows a run the backend started by itself (an agent's run_tests or
 * run_configuration tool) in its tab, like one started here. It runs
 * synchronously in the event handler: the run's first output follows at once.
 */
function adoptRun(payload: unknown): void {
  const a = parseRunAdoption(payload)
  if (!a || runKeys.has(a.id)) return
  const key = a.kind === 'tests' ? TESTS_KEY : configKey(a.root, a.config.id)
  const base: Pick<RunSession, 'kind' | 'title' | 'config' | 'root' | 'request' | 'bases'> = a.kind === 'tests'
    ? { kind: 'tests', title: 'Tests', request: a.request, bases: [...a.request.dirs, a.root].filter(Boolean) }
    : { kind: 'config', title: a.config.name, config: runconfig.Config.createFrom(a.config), root: a.root, bases: [resolvePath(a.root, a.config.cwd || '.')] }
  const prev = useRun.getState().sessions[key]
  if (prev?.running) void RunAPI.Stop(prev.runId).catch(() => undefined)
  if (prev) dropRun(prev.runId)
  runKeys.set(a.id, key)
  resetConsole(key)
  const session: RunSession = { key, runId: a.id, running: true, exit: null, tests: emptyTree, startedAt: Date.now(), ...base }
  useRun.setState((s) => ({ sessions: { ...s.sessions, [key]: session }, lastKey: key }))
  notifyResults()
  // In the background: the user is in the chat, and the tab is there to look at.
  useEditor.getState().openRun(key, a.title || base.title, true)
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
  const root = session?.root ?? rootFor(session?.bases[0])
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
  const session = key ? useRun.getState().sessions[key] : undefined
  const summary = session && runSummary({ kind: session.kind, exit: ev.exit, tests: countTests(session.tests) })
  if (session && summary) alertDone(session.title, summary, runTabId(session.key))
  settleExit(runId, ev.exit)
  inputs.delete(runId)
  deliver.forget(runId)
}

const deliver = createReorderer<RunEventMsg>(applyRunEvent)

let subscribed = false
export function subscribeRunEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('run:event', (ev: RunEventMsg) => deliver(ev.id, ev))
  EventsOn('run:adopt', adoptRun)

  // Configurations follow the active tab's workspace folder and edits of its .flint/run.json.
  let unwatch: (() => void) | null = null
  const follow = () => {
    const root = followedRoot(following)
    if (root === following) return
    following = root
    unwatch?.()
    unwatch = root ? onFileChanged(`${root}/.flint/run.json`, () => void loadRunConfigs(root)) : null
    // Show what is known about the folder at once; the reload refreshes it.
    useRun.setState({ root, ...((root && loadedByRoot.get(root)) || EMPTY) })
    void loadRunConfigs(root)
  }
  useWorkspace.subscribe((s, prev) => {
    if (s.roots !== prev.roots) {
      for (const r of loadedByRoot.keys()) if (!s.roots.includes(r)) loadedByRoot.delete(r)
      follow()
    }
  })
  useEditor.subscribe((s, prev) => {
    if (s.activeId !== prev.activeId) follow()
  })
  follow()
}
