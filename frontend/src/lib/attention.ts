// "Something needs you": the one model every signal reads (the desktop
// notification, the Dock badge, the in-app notice). Pure, so every rule is
// testable; store/alerts.ts feeds it events and acts on what it returns.
//
// Two kinds of entries:
//  - needs: an agent (or Flint) waits for the user's answer: a chat's approval
//    request, an agent CLI in a terminal that waits, a native question of
//    Flint's. A need stays until it is answered, expires or its chat closes;
//    it is announced at most once: when it arrives while Flint is in the
//    background, or when Flint goes there while it is still open.
//  - finished work: a turn, run, CI result or project event that ended while
//    Flint was in the background. It counts until the user is back.
import type { AgentEvent, PermissionRequest } from './agentReducer'
import { approvalNotice } from './approvalAlerts'

export type NeedKind = 'approval' | 'terminal' | 'question'

export interface Need {
  key: string
  kind: NeedKind
  /** The desktop notification's text. */
  title: string
  body: string
  /** The editor tab it is about ("chat:<id>", "term:<id>"), '' for none. */
  tab: string
  /** The in-app notice shown when its tab is not the active one ('' for none). */
  inApp: string
  /** Announced already: a notification was sent (Go announces its questions itself). */
  announced: boolean
}

export interface Attention {
  /** Waiting for the user, oldest first. */
  needs: readonly Need[]
  /** Finished work announced since the user was last in front. */
  finished: number
  /**
   * Flint is the app in front, as Go says ("attention:front"): the one source,
   * in which a dialog of Flint's (which takes the web view's focus) counts as Flint.
   */
  front: boolean
  /** The newest front event applied (events may arrive out of order). */
  frontSeq: number
  /** When each terminal tab last notified (terminalChanged), in ms. */
  terminals: Readonly<Record<string, TerminalSent>>
}

export interface TerminalSent {
  waiting?: number
  finished?: number
}

export const emptyAttention: Attention = { needs: [], finished: 0, front: true, frontSeq: 0, terminals: {} }

/** The settings of Settings ▸ Agents ▸ Notifications. */
export interface NotifyPrefs {
  /** Notifications and the badge at all (finished work, failures). */
  notifyInBackground: boolean
  /** Also for what waits for an answer (needs notifyInBackground). */
  notifyOnApproval: boolean
}

export interface Notice {
  title: string
  body: string
  tab: string
}

/** A new state and the notifications to send now. */
export interface Step {
  state: Attention
  notify: Notice[]
}

const notice = (n: Pick<Need, 'title' | 'body' | 'tab'>): Notice => ({ title: n.title, body: n.body, tab: n.tab })
const needsAllowed = (p: NotifyPrefs) => p.notifyInBackground && p.notifyOnApproval

/**
 * Something started waiting for the user. It is announced at once when Flint is
 * in the background and the settings allow it; a key already open is not
 * announced again. Go announces its own questions: they are only counted.
 */
export function needOpened(a: Attention, need: Omit<Need, 'announced'>, background: boolean, prefs: NotifyPrefs): Step {
  if (a.needs.some((n) => n.key === need.key)) return { state: a, notify: [] }
  const announce = need.kind !== 'question' && background && needsAllowed(prefs)
  return {
    state: { ...a, needs: [...a.needs, { ...need, announced: need.kind === 'question' || announce }] },
    notify: announce ? [notice(need)] : [],
  }
}

/** A need was answered, expired or withdrawn. */
export function needClosed(a: Attention, key: string): Attention {
  return a.needs.some((n) => n.key === key) ? { ...a, needs: a.needs.filter((n) => n.key !== key) } : a
}

/** Every need of a tab closed (its turn ended, its chat or terminal closed). */
export function needsOfTabClosed(a: Attention, tab: string): Attention {
  return a.needs.some((n) => n.tab === tab) ? { ...a, needs: a.needs.filter((n) => n.tab !== tab) } : a
}

/** Work finished: announced (and counted) only while Flint is in the background. */
export function workFinished(a: Attention, n: Notice, background: boolean, prefs: NotifyPrefs): Step {
  if (!background || !prefs.notifyInBackground) return { state: a, notify: [] }
  return { state: { ...a, finished: a.finished + 1 }, notify: [n] }
}

/** Flint went to the background: what still waits and was not announced is announced now, once. */
export function wentToBackground(a: Attention, prefs: NotifyPrefs): Step {
  if (!needsAllowed(prefs)) return { state: a, notify: [] }
  const due = a.needs.filter((n) => !n.announced)
  if (due.length === 0) return { state: a, notify: [] }
  return { state: { ...a, needs: a.needs.map((n) => (n.announced ? n : { ...n, announced: true })) }, notify: due.map(notice) }
}

/** The user is back: finished work is seen; what still waits stays counted. */
export const cameBack = (a: Attention): Attention => (a.finished === 0 ? a : { ...a, finished: 0 })

/**
 * Go says Flint came to the front or left it (event seq; an older one than the
 * last applied is ignored). Leaving announces what waits (wentToBackground);
 * coming back clears the finished work (cameBack).
 */
export function frontChanged(a: Attention, front: boolean, seq: number, prefs: NotifyPrefs): Step {
  if (seq < a.frontSeq || (seq === a.frontSeq && front === a.front)) return { state: a, notify: [] }
  const moved = { ...a, front, frontSeq: seq }
  if (front === a.front) return { state: moved, notify: [] }
  return front ? { state: cameBack(moved), notify: [] } : wentToBackground(moved, prefs)
}

/** A tab closed (a chat, a terminal): what it asked is gone, and so is what is kept about it. */
export function tabGone(a: Attention, tab: string): Attention {
  const next = needsOfTabClosed(a, tab)
  if (!(tab in next.terminals)) return next
  const terminals = { ...next.terminals }
  delete terminals[tab]
  return { ...next, terminals }
}

/** The Dock badge: what still waits plus finished work not seen yet, as far as the settings show them. */
export function badgeCount(a: Attention, prefs: NotifyPrefs): number {
  if (!prefs.notifyInBackground) return 0
  return a.finished + (prefs.notifyOnApproval ? a.needs.length : 0)
}

/** The in-app notice: the newest need with one whose tab is not the active one and that was not dismissed. */
export function inAppNotice(a: Attention, activeTab: string | null, dismissed: ReadonlySet<string>): Need | null {
  for (let i = a.needs.length - 1; i >= 0; i--) {
    const n = a.needs[i]
    if (n.inApp && n.tab !== activeTab && !dismissed.has(n.key)) return n
  }
  return null
}

const folderName = (p: string) => {
  const trimmed = p.replace(/\/+$/, '')
  return trimmed.slice(trimmed.lastIndexOf('/') + 1)
}

// ---- Chats

/** What a chat's events need to know of it. */
export interface ChatInfo {
  agent: string
  cwd: string
  /** Its tab's title. */
  title: string
  /** The chat that started it, when another agent did ('' or absent otherwise). */
  parent?: string
}

/** The key of a chat's approval request. */
export const approvalKey = (sessionId: string, requestId: string) => `chat:${sessionId}|${requestId}`

/** Whether a chat is in a turn after event e. Only turn_start opens one: history a resumed chat replays is not a turn. */
export function turnAfter(inTurn: boolean, e: Pick<AgentEvent, 'kind'>): boolean {
  if (e.kind === 'turn_start') return true
  if (e.kind === 'turn_end') return false
  return inTurn
}

export type ChatSignal =
  | { kind: 'need'; need: Omit<Need, 'announced'> }
  | { kind: 'closed'; key: string }
  | { kind: 'ended'; tab: string }
  | { kind: 'finished'; notice: Notice }

/** The in-app notice of a request. */
export const approvalInApp = (agent: string, cwd: string) => `${agent} is waiting for your approval in ${folderName(cwd)}`

/**
 * What one event of a chat means for the user. inTurn is the chat's turn
 * before the event (turnAfter): a request outside a turn is replayed
 * history, and so is a turn end without a turn. A child agent (parent set)
 * asks like any agent, but its turn ends are its parent's to hear.
 */
export function chatSignals(sessionId: string, e: AgentEvent, inTurn: boolean, chat: ChatInfo): ChatSignal[] {
  const tab = `chat:${sessionId}`
  switch (e.kind) {
    case 'permission_request':
      if (!inTurn || !e.permission) return []
      return [{ kind: 'need', need: approvalNeed(sessionId, chat, e.permission) }]
    case 'permission_settled':
      return e.requestId ? [{ kind: 'closed', key: approvalKey(sessionId, e.requestId) }] : []
    case 'turn_end': {
      const out: ChatSignal[] = [{ kind: 'ended', tab }]
      if (inTurn && e.stopReason !== 'cancelled' && !chat.parent) {
        const body = e.stopReason === 'error' ? 'The agent stopped with an error' : 'The agent finished its turn'
        out.push({ kind: 'finished', notice: { title: chat.title || chat.agent, body, tab } })
      }
      return out
    }
  }
  return []
}

function approvalNeed(sessionId: string, chat: ChatInfo, req: PermissionRequest): Omit<Need, 'announced'> {
  const { title, body } = approvalNotice(chat.agent, chat.cwd, req)
  return { key: approvalKey(sessionId, req.id), kind: 'approval', title, body, tab: `chat:${sessionId}`, inApp: approvalInApp(chat.agent, chat.cwd) }
}

// ---- Agent CLIs in terminal tabs

/** What a terminal's agent state change means: a need while it waits, finished work when it is done. */
export type TerminalSignal = { kind: 'need'; need: Omit<Need, 'announced'> } | { kind: 'closed'; key: string } | { kind: 'finished'; notice: Notice }

export interface TerminalInfo {
  /** The editor tab id ("term:<id>"). */
  tab: string
  /** The folder the program runs in (Go reads it from the process, not from the output). */
  cwd: string
  /** The running command ("claude --resume"), as Go reads it from the process. */
  command: string
}

/**
 * The agent CLIs whose state a terminal tells the user about. The state comes
 * from sequences in the terminal's output, which any program can print: only
 * these, running in the foreground, may notify (the tab's own badge follows
 * every state).
 */
export const TERMINAL_AGENTS: readonly string[] = ['claude', 'codex', 'gemini', 'opencode']

/** The agent CLI a terminal runs ('' for none): the program of its command, if it is a known one. */
export function terminalAgentName(command: string): string {
  const program = command.split(' ')[0] ?? ''
  return TERMINAL_AGENTS.includes(program) ? program : ''
}

/** At most one "waiting" and one "finished" notification per terminal tab in this long. */
export const TERMINAL_NOTIFY_INTERVAL_MS = 60_000

/**
 * The signals of a terminal agent moving from prev to next (Go: terminal.Agent*
 * states; undefined: none). None for a program that is not a known agent CLI.
 * The texts name the program and the folder, both read by Go from the process:
 * nothing the terminal's output controls (its title).
 */
export function terminalSignals(t: TerminalInfo, prev: string | undefined, next: string | undefined): TerminalSignal[] {
  const agent = terminalAgentName(t.command)
  if (prev === next || !agent) return []
  const folder = folderName(t.cwd)
  const out: TerminalSignal[] = []
  if (prev === 'waiting') out.push({ kind: 'closed', key: t.tab })
  if (next === 'waiting') {
    out.push({
      kind: 'need',
      need: { key: t.tab, kind: 'terminal', title: `${agent} is waiting for you`, body: `In a terminal in ${folder}`, tab: t.tab, inApp: `${agent} is waiting for you in a terminal in ${folder}` },
    })
  }
  if (next === 'done' && (prev === 'working' || prev === 'waiting')) {
    out.push({ kind: 'finished', notice: { title: `${agent} finished its turn`, body: `In a terminal in ${folder}`, tab: t.tab } })
  }
  return out
}

/**
 * A terminal agent's state changed (at time now, in ms): its signals applied
 * to the model, with at most one "waiting" and one "finished" notification
 * per tab per TERMINAL_NOTIFY_INTERVAL_MS. A wait inside that interval still
 * counts (badge, in-app notice) but is not announced; a finish inside it is
 * not counted either.
 */
export function terminalChanged(a: Attention, t: TerminalInfo, prev: string | undefined, next: string | undefined, now: number, prefs: NotifyPrefs): Step {
  let state = a
  const notify: Notice[] = []
  const recent = (at: number | undefined) => at !== undefined && now - at < TERMINAL_NOTIFY_INTERVAL_MS
  const sent = (kind: keyof TerminalSent) => {
    state = { ...state, terminals: { ...state.terminals, [t.tab]: { ...state.terminals[t.tab], [kind]: now } } }
  }
  for (const s of terminalSignals(t, prev, next)) {
    if (s.kind === 'closed') {
      state = needClosed(state, s.key)
    } else if (s.kind === 'need') {
      const quiet = recent(state.terminals[t.tab]?.waiting)
      const step = needOpened(state, s.need, !state.front && !quiet, prefs)
      state = step.state
      if (quiet) state = { ...state, needs: state.needs.map((n) => (n.key === s.need.key ? { ...n, announced: true } : n)) }
      if (step.notify.length) sent('waiting')
      notify.push(...step.notify)
    } else if (!recent(state.terminals[t.tab]?.finished)) {
      const step = workFinished(state, s.notice, !state.front, prefs)
      state = step.state
      if (step.notify.length) sent('finished')
      notify.push(...step.notify)
    }
  }
  return { state, notify }
}

// ---- Settings ▸ Agents ▸ Notifications

/** What Settings shows for Go's NotificationStatus.state. */
export function notificationStatusLabel(state: string): string {
  switch (state) {
    case 'allowed':
      return 'Allowed'
    case 'denied':
      return 'Not allowed by the system'
    case 'failed':
      return 'Allowed, but the last one was not delivered'
    case 'unavailable':
      return 'Not available'
  }
  return 'Unknown'
}
