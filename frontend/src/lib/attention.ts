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
}

export const emptyAttention: Attention = { needs: [], finished: 0 }

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
  title: string
  cwd: string
  /** The running command ("claude --resume"). */
  command: string
}

/** The signals of a terminal agent moving from prev to next (Go: terminal.Agent* states; undefined: none). */
export function terminalSignals(t: TerminalInfo, prev: string | undefined, next: string | undefined): TerminalSignal[] {
  if (prev === next) return []
  const agent = t.command.split(' ')[0] || 'The agent'
  const out: TerminalSignal[] = []
  if (prev === 'waiting') out.push({ kind: 'closed', key: t.tab })
  if (next === 'waiting') {
    out.push({
      kind: 'need',
      need: {
        key: t.tab, kind: 'terminal', title: `${agent} is waiting for you`, body: `${folderName(t.cwd)}: ${t.title}`, tab: t.tab,
        inApp: `${agent} is waiting for you in the terminal ${t.title}`,
      },
    })
  }
  if (next === 'done' && (prev === 'working' || prev === 'waiting')) {
    out.push({ kind: 'finished', notice: { title: t.title, body: `${agent} finished its turn`, tab: t.tab } })
  }
  return out
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
