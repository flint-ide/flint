import * as SessionAPI from '../../wailsjs/go/main/SessionAPI'
import { Scratch, errorMessage } from '../api/backend'
import { showToast } from './dialog'
import { SNAPSHOT_VERSION, chatsNeedSave, inside, parseSnapshot, trimChat, type SavedTab, type Snapshot } from '../lib/sessionSnapshot'
import { diffTabId, fileTabId, historyTabId, revisionDiffTabId, scratchTabId, useEditor, viewFor, type Tab } from './editor'
import { recentOutput, terminalTabId, TerminalBackend, useTerminal } from './terminal'
import { chatTabId, useAgentChat } from './agentChat'
import { selectRunConfig, useRun } from './run'
import { setAllBreakpoints, useDebug } from './debug'
import { breakpointsUnder } from '../lib/breakpoints'

// What was open in a project (tabs, terminals, agent chats) is saved per project
// so a restart comes back as it was left. Terminals reopen as new shells in their
// last directory with the earlier output dimmed above; chats keep their
// transcript and reconnect to the agent's conversation on the next message.

const SAVE_DELAY_MS = 1500
const SAVE_MAX_WAIT_MS = 10_000
const SCROLLBACK_LINES = 400

async function capture(root: string): Promise<Snapshot> {
  const { tabs, activeId } = useEditor.getState()
  const saved: (SavedTab | null)[] = await Promise.all(tabs.map((t) => captureTab(t, root)))
  const kept = tabs.flatMap((t, i) => (saved[i] ? [{ id: t.id, tab: saved[i]! }] : []))
  return {
    version: SNAPSHOT_VERSION,
    active: kept.findIndex((k) => k.id === activeId),
    tabs: kept.map((k) => k.tab),
    runConfig: useRun.getState().selectedId ?? undefined,
    breakpoints: breakpointsUnder(useDebug.getState().breakpoints, root),
  }
}

async function captureTab(t: Tab, root: string): Promise<SavedTab | null> {
  switch (t.kind) {
    case 'file': {
      if (!inside(t.path, root)) return null
      const view = viewFor(t.id)
      if (!view) return { kind: 'file', path: t.path }
      const head = view.state.selection.main.head
      const line = view.state.doc.lineAt(head)
      return { kind: 'file', path: t.path, line: line.number, col: head - line.from }
    }
    case 'scratch':
      return { kind: 'scratch', scratchId: t.scratchId, scratchKind: t.scratchKind, title: t.title }
    case 'diff': {
      if (!inside(t.path, root)) return null
      const { path, basePath, repoRoot, title, baseRev, rev, baseLabel, label } = t
      return { kind: 'diff', path, basePath, repoRoot, title, baseRev, rev, baseLabel, label }
    }
    case 'history':
      return inside(t.path, root) ? { kind: 'history', path: t.path } : null
    case 'terminal': {
      const term = useTerminal.getState().tabs.find((x) => x.id === t.termId)
      if (!term || (term.project !== root && !inside(term.cwd, root))) return null
      const cwd = term.exitCode === null ? await TerminalBackend.Cwd(t.termId).catch(() => '') : ''
      return { kind: 'terminal', title: t.title, cwd: cwd || term.cwd, output: recentOutput(t.termId, SCROLLBACK_LINES) }
    }
    case 'chat': {
      const s = useAgentChat.getState().sessions[t.sessionId]
      if (!s || !inside(s.cwd, root)) return null
      return {
        kind: 'chat',
        title: t.title,
        chat: { id: s.id, agent: s.agent, cwd: s.cwd, model: s.model, draft: s.draft, chat: trimChat(s.chat) },
      }
    }
    case 'run':
    case 'debug':
      return null // processes are not restored; only the selected configuration is
  }
}

/** Re-opens what capture saved, after the tabs already open. */
async function apply(root: string, snap: Snapshot): Promise<void> {
  if (snap.runConfig) selectRunConfig(snap.runConfig)
  if (snap.breakpoints) {
    const others = Object.entries(useDebug.getState().breakpoints).filter(([path]) => !inside(path, root))
    setAllBreakpoints({ ...Object.fromEntries(others), ...snap.breakpoints })
  }
  const editor = useEditor.getState()
  const scratchIds = snap.tabs.some((t) => t.kind === 'scratch')
    ? new Set(((await Scratch.List().catch(() => [])) ?? []).map((s) => s.id))
    : new Set<string>()
  const dirs = new Map<string, boolean>()
  await Promise.all(
    snap.tabs.flatMap((t) => (t.kind === 'terminal' ? [SessionAPI.Exists(t.cwd).then((ok) => void dirs.set(t.cwd, ok))] : [])),
  )

  const ids: (string | null)[] = []
  for (const t of snap.tabs) {
    switch (t.kind) {
      case 'file':
        editor.addTab({
          id: fileTabId(t.path),
          kind: 'file',
          path: t.path,
          title: t.path.split('/').pop() ?? t.path,
          dirty: false,
          reveal: t.line ? { line: t.line, from: t.col ?? 0, to: t.col ?? 0, nonce: 0, quiet: true } : undefined,
        })
        ids.push(fileTabId(t.path))
        break
      case 'scratch':
        if (!scratchIds.has(t.scratchId)) {
          ids.push(null)
          break
        }
        editor.addTab({ id: scratchTabId(t.scratchId), kind: 'scratch', scratchId: t.scratchId, scratchKind: t.scratchKind, title: t.title, dirty: false })
        ids.push(scratchTabId(t.scratchId))
        break
      case 'diff': {
        const { path, basePath, repoRoot, title, baseRev, rev, baseLabel, label } = t
        const id = baseRev || rev ? revisionDiffTabId(t) : diffTabId(path)
        editor.addTab({ id, kind: 'diff', path, basePath, repoRoot, title, baseRev, rev, baseLabel, label, dirty: false })
        ids.push(id)
        break
      }
      case 'history':
        editor.addTab({ id: historyTabId(t.path), kind: 'history', path: t.path, title: `History: ${t.path.split('/').pop() ?? t.path}`, dirty: false })
        ids.push(historyTabId(t.path))
        break
      case 'terminal': {
        const cwd = dirs.get(t.cwd) ? t.cwd : root
        const id = useTerminal.getState().create(cwd, { title: t.title, restored: t.output || undefined, background: true, project: root })
        ids.push(terminalTabId(id))
        break
      }
      case 'chat':
        useAgentChat.getState().restore(t.chat)
        ids.push(chatTabId(t.chat.id))
        break
    }
  }
  const active = ids[snap.active]
  if (active && useEditor.getState().tabs.some((t) => t.id === active)) useEditor.getState().setActive(active)
}

let restoring = false
let timer: ReturnType<typeof setTimeout> | undefined
let firstPending = 0
let currentRoot: string | null = null
let restored: Promise<void> | null = null
let saveFailed = false // report a broken save once, not on every autosave
const lastSaved = new Map<string, string>() // JSON last written per root

/** Saves root's session now (pending autosaves are folded in). */
export async function saveSession(root = currentRoot): Promise<void> {
  clearTimeout(timer)
  timer = undefined
  firstPending = 0
  if (!root || restoring) return
  try {
    const json = JSON.stringify(await capture(root))
    if (lastSaved.get(root) === json) return
    await SessionAPI.Save(root, json)
    lastSaved.set(root, json)
    saveFailed = false
  } catch (err) {
    if (!saveFailed) showToast(`Could not save open tabs: ${errorMessage(err)}`, 'error')
    saveFailed = true
  }
}

/**
 * Opens root's saved session; autosave then follows root. Calls for the root
 * already restored share that restore (init runs twice under StrictMode).
 */
export function restoreSession(root: string): Promise<void> {
  if (currentRoot === root && restored) return restored
  currentRoot = root
  restored = (async () => {
    restoring = true
    try {
      const snap = parseSnapshot(await SessionAPI.Load(root))
      if (snap) await apply(root, snap)
    } catch (err) {
      showToast(`Could not restore open tabs: ${errorMessage(err)}`, 'error')
    } finally {
      restoring = false
    }
  })()
  return restored
}

/** Debounced save, at most SAVE_MAX_WAIT_MS behind while agents stream. */
function scheduleSave(): void {
  if (restoring || !currentRoot) return
  const now = Date.now()
  firstPending ||= now
  clearTimeout(timer)
  const wait = Math.max(0, Math.min(SAVE_DELAY_MS, firstPending + SAVE_MAX_WAIT_MS - now))
  timer = setTimeout(() => void saveSession(), wait)
}

let subscribed = false
export function subscribeSessionAutosave(): void {
  if (subscribed) return
  subscribed = true
  useEditor.subscribe((s, prev) => {
    if (s.tabs !== prev.tabs || s.activeId !== prev.activeId) scheduleSave()
  })
  useTerminal.subscribe((s, prev) => {
    if (s.tabs !== prev.tabs) scheduleSave()
  })
  useAgentChat.subscribe((s, prev) => {
    if (chatsNeedSave(prev.sessions, s.sessions)) scheduleSave()
  })
  useDebug.subscribe((s, prev) => {
    if (s.breakpoints !== prev.breakpoints) scheduleSave()
  })
  useRun.subscribe((s, prev) => {
    if (s.selectedId !== prev.selectedId) scheduleSave()
  })
  // Cursor moves and terminal output don't touch the stores; catch them when the
  // window loses focus.
  window.addEventListener('blur', () => scheduleSave())
}
