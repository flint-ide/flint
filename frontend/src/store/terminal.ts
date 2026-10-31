import { createStore } from '../lib/store'
import * as Terminal from '../../bindings/flint/terminalapi'
import { EventsOn } from '../wails/runtime'
import { createReorderer } from '../lib/reorder'
import { useEditor } from './editor'
import { terminalTitle } from '../lib/terminalTitle'

export interface TerminalTab {
  id: string
  title: string
  /** The title was given (Docker logs, an agent task) and stays; otherwise it follows what runs. */
  pinned: boolean
  /** The running command, '' at the prompt (term:title). */
  command: string
  /** Title the running program set (OSC 0/2), dropped when the command ends. */
  programTitle?: string
  cwd: string
  exitCode: number | null
  /** Typed into the shell once it starts, e.g. a `claude '…'` command. */
  initialInput?: string
  /** Output from before a restart, shown dimmed above the new shell. */
  restored?: string
  /** Project the terminal was opened for (its cwd may be elsewhere after a restore). */
  project?: string
}

export interface CreateOptions {
  /** A fixed tab name; without it the tab is named after what the terminal runs. */
  title?: string
  input?: string
  restored?: string
  /** Open the tab without switching to it (session restore). */
  background?: boolean
  project?: string
}

interface TerminalState {
  tabs: TerminalTab[]
  create: (cwd: string, opts?: CreateOptions) => string
  close: (id: string) => void
}

/** Editor tab id of a terminal (terminals live among the editor tabs). */
export const terminalTabId = (termId: string) => `term:${termId}`

export const useTerminal = createStore<TerminalState>((set, get) => ({
  tabs: [],

  create: (cwd, opts = {}) => {
    const id = crypto.randomUUID()
    const title = opts.title ?? terminalTitle({ command: '', cwd })
    const tab: TerminalTab = {
      id, title, pinned: opts.title !== undefined, command: '', cwd, exitCode: null, initialInput: opts.input, restored: opts.restored, project: opts.project ?? cwd,
    }
    set((s) => ({ tabs: [...s.tabs, tab] }))
    useEditor.getState().openTerminal(id, title, opts.background)
    return id
  },

  close: (id) => {
    const exited = get().tabs.find((t) => t.id === id)?.exitCode != null
    void Terminal.Close(id)
    markClosed(id)
    // A live shell is forgotten when it reports its exit (term:exit below).
    if (exited) forgetClosedLater(id)
    started.delete(id)
    sinks.delete(id)
    backlog.delete(id)
    inputs.delete(id)
    readers.delete(id)
    selections.delete(id)
    set((s) => ({ tabs: s.tabs.filter((t) => t.id !== id) }))
    useEditor.getState().closeTab(terminalTabId(id))
  },
}))

/**
 * ⌘J: jump to the most recently used terminal, or back to where you were when a
 * terminal is already active. Creates one when there is none.
 */
export function toggleTerminal(cwd: string): void {
  const editor = useEditor.getState()
  const active = editor.tabs.find((t) => t.id === editor.activeId)
  if (active?.kind === 'terminal') {
    editor.back()
    return
  }
  const recent = [...editor.backStack].reverse().find((id) => id.startsWith('term:') && editor.tabs.some((t) => t.id === id))
  const any = editor.tabs.find((t) => t.kind === 'terminal')?.id
  const target = recent ?? any
  if (target) editor.setActive(target)
  else useTerminal.getState().create(cwd)
}

/** Applies a change to a terminal and renames its tab when the name follows what runs. */
function updateTab(id: string, change: (t: TerminalTab) => Partial<TerminalTab>): void {
  const tab = useTerminal.getState().tabs.find((t) => t.id === id)
  if (!tab) return
  const next = { ...tab, ...change(tab) }
  if (!next.pinned) next.title = terminalTitle(next)
  useTerminal.setState((s) => ({ tabs: s.tabs.map((t) => (t.id === id ? next : t)) }))
  if (next.title !== tab.title) useEditor.getState().retitleTab(terminalTabId(id), next.title)
}

/** A title set by the program in the terminal (OSC 0/2); shells setting one at the prompt are ignored. */
export function setProgramTitle(id: string, title: string): void {
  updateTab(id, (t) => (t.command ? { programTitle: title } : {}))
}

/** Recent plain-text output of each mounted terminal, for session snapshots. */
const readers = new Map<string, (maxLines: number) => string>()

export function registerOutputReader(id: string, read: (maxLines: number) => string): () => void {
  readers.set(id, read)
  return () => {
    if (readers.get(id) === read) readers.delete(id)
  }
}

export function recentOutput(id: string, maxLines: number): string {
  return readers.get(id)?.(maxLines) ?? ''
}

/** Selected text of each mounted terminal (⌘L / "Send to agent"). */
const selections = new Map<string, () => string>()

export function registerSelectionReader(id: string, read: () => string): () => void {
  selections.set(id, read)
  return () => {
    if (selections.get(id) === read) selections.delete(id)
  }
}

export function terminalSelection(id: string): string {
  return selections.get(id)?.() ?? ''
}

/** Terminals whose PTY was started; the PTY outlives remounts of its view. */
const started = new Set<string>()

/** True the first time it is called for a terminal: that caller starts the PTY. */
export function claimStart(id: string): boolean {
  if (started.has(id)) return false
  started.add(id)
  return true
}

type Sink = (data: Uint8Array) => void

interface Backlog {
  chunks: Uint8Array[]
  bytes: number
}

/** Output routing: xterm instances register a sink; output arriving earlier is buffered. */
const sinks = new Map<string, Sink>()
const backlog = new Map<string, Backlog>()
/** Output buffered for a terminal no view shows yet; the oldest is dropped beyond this. */
const MAX_BACKLOG_BYTES = 2 * 1024 * 1024

/** Output still in flight for closed tabs is dropped. */
const closed = new Set<string>()
/** Bound for `closed` should a shell never report its exit (insertion order: oldest first). */
const MAX_CLOSED = 256
/** How long after a closed terminal's exit its last (reordered) output may still arrive. */
const CLOSED_GRACE_MS = 1000

function markClosed(id: string): void {
  closed.add(id)
  for (const old of closed) {
    if (closed.size <= MAX_CLOSED) break
    closed.delete(old)
    deliver.forget(old)
  }
}

/** Exit is sent on its own channel, so output reordered behind it may still come. */
function forgetClosedLater(id: string): void {
  setTimeout(() => {
    closed.delete(id)
    deliver.forget(id)
  }, CLOSED_GRACE_MS)
}

function buffer(id: string, bytes: Uint8Array): void {
  const pending = backlog.get(id) ?? { chunks: [], bytes: 0 }
  backlog.set(id, pending)
  pending.chunks.push(bytes)
  pending.bytes += bytes.length
  while (pending.bytes > MAX_BACKLOG_BYTES && pending.chunks.length > 1) {
    pending.bytes -= pending.chunks.shift()!.length
  }
}

interface InputQueue {
  pending: string
  inFlight: boolean
}
const inputs = new Map<string, InputQueue>()

/**
 * Sends keystrokes in order. Wails runs every bound call on its own goroutine, so
 * one Write per key could reach the PTY reordered; instead one write is in flight
 * at a time and keys typed meanwhile are coalesced into the next one.
 */
export function sendInput(id: string, data: string): void {
  const q = inputs.get(id) ?? { pending: '', inFlight: false }
  inputs.set(id, q)
  q.pending += data
  if (!q.inFlight) void flushInput(id, q)
}

async function flushInput(id: string, q: InputQueue): Promise<void> {
  q.inFlight = true
  while (q.pending) {
    const chunk = q.pending
    q.pending = ''
    try {
      await Terminal.Write(id, chunk)
    } catch {
      q.pending = '' // terminal exited; drop what's left
    }
  }
  q.inFlight = false
}

export function attachSink(id: string, sink: Sink): () => void {
  sinks.set(id, sink)
  for (const chunk of backlog.get(id)?.chunks ?? []) sink(chunk)
  backlog.delete(id)
  return () => {
    if (sinks.get(id) === sink) sinks.delete(id)
  }
}

export function decodeBase64(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

const deliver = createReorderer<{ id: string; seq?: number; data: string }>((_, msg) => {
  if (closed.has(msg.id)) return
  const bytes = decodeBase64(msg.data)
  const sink = sinks.get(msg.id)
  if (sink) sink(bytes)
  else buffer(msg.id, bytes)
})

let subscribed = false
export function subscribeTerminalEvents(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('term:data', (msg: { id: string; seq?: number; data: string }) => deliver(msg.id, msg))
  EventsOn('term:title', (msg: { id: string; command: string; cwd: string }) => {
    if (closed.has(msg.id)) return
    updateTab(msg.id, (t) => ({
      command: msg.command,
      cwd: msg.cwd || t.cwd,
      programTitle: msg.command === t.command ? t.programTitle : undefined,
    }))
  })
  EventsOn('term:exit', (msg: { id: string; code: number }) => {
    if (closed.has(msg.id)) return forgetClosedLater(msg.id)
    useTerminal.setState((s) => ({
      tabs: s.tabs.map((t) => (t.id === msg.id ? { ...t, exitCode: msg.code } : t)),
    }))
  })
}

export { Terminal as TerminalBackend }
