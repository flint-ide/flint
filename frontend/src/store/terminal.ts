import { create } from 'zustand'
import * as Terminal from '../../wailsjs/go/main/TerminalAPI'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { createReorderer } from '../lib/reorder'

export interface TerminalTab {
  id: string
  title: string
  cwd: string
  exitCode: number | null
  /** Typed into the shell once it starts, e.g. a `claude '…'` command. */
  initialInput?: string
}

export interface CreateOptions {
  title?: string
  input?: string
}

interface TerminalState {
  tabs: TerminalTab[]
  activeId: string | null
  open: boolean
  create: (cwd: string, opts?: CreateOptions) => string
  close: (id: string) => void
  setActive: (id: string) => void
  toggle: () => void
  setOpen: (open: boolean) => void
}

let seq = 0

export const useTerminal = create<TerminalState>((set, get) => ({
  tabs: [],
  activeId: null,
  open: true,

  create: (cwd, opts = {}) => {
    const id = crypto.randomUUID()
    const title = opts.title ?? `${cwd.split('/').pop() || '~'} ${++seq}`
    const tab: TerminalTab = { id, title, cwd, exitCode: null, initialInput: opts.input }
    set((s) => ({ tabs: [...s.tabs, tab], activeId: id, open: true }))
    return id
  },

  close: (id) => {
    void Terminal.Close(id)
    closed.add(id)
    sinks.delete(id)
    backlog.delete(id)
    inputs.delete(id)
    set((s) => {
      const idx = s.tabs.findIndex((t) => t.id === id)
      const tabs = s.tabs.filter((t) => t.id !== id)
      const activeId = s.activeId === id ? (tabs[Math.max(0, idx - 1)]?.id ?? null) : s.activeId
      return { tabs, activeId }
    })
  },

  setActive: (id) => set({ activeId: id }),
  toggle: () => set({ open: !get().open }),
  setOpen: (open) => set({ open }),
}))

type Sink = (data: Uint8Array) => void

/** Output routing: xterm instances register a sink; output arriving earlier is buffered. */
const sinks = new Map<string, Sink>()
const backlog = new Map<string, Uint8Array[]>()
const closed = new Set<string>() // output still in flight for closed tabs is dropped

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
  for (const chunk of backlog.get(id) ?? []) sink(chunk)
  backlog.delete(id)
  return () => {
    if (sinks.get(id) === sink) sinks.delete(id)
  }
}

function decodeBase64(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

let subscribed = false
export function subscribeTerminalEvents(): void {
  if (subscribed) return
  subscribed = true
  const deliver = createReorderer<{ id: string; seq?: number; data: string }>((_, msg) => {
    if (closed.has(msg.id)) return
    const bytes = decodeBase64(msg.data)
    const sink = sinks.get(msg.id)
    if (sink) {
      sink(bytes)
      return
    }
    const pending = backlog.get(msg.id)
    if (pending) pending.push(bytes)
    else backlog.set(msg.id, [bytes])
  })
  EventsOn('term:data', (msg: { id: string; seq?: number; data: string }) => deliver(msg.id, msg))
  EventsOn('term:exit', (msg: { id: string; code: number }) => {
    useTerminal.setState((s) => ({
      tabs: s.tabs.map((t) => (t.id === msg.id ? { ...t, exitCode: msg.code } : t)),
    }))
  })
}

export { Terminal as TerminalBackend }
