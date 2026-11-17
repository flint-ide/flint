// "An agent waits for your approval" notifications, pure so they are testable.
// The text never carries more than the tool's title; Go redacts secrets from
// every notification again before it reaches the OS (api_alerts.go).
import type { PermissionRequest } from './agentReducer'

const MAX_BODY = 140

const folderName = (p: string) => {
  const trimmed = p.replace(/\/+$/, '')
  return trimmed.slice(trimmed.lastIndexOf('/') + 1)
}

/** Title and body of the notification for one permission request. */
export function approvalNotice(agent: string, cwd: string, req: PermissionRequest): { title: string; body: string } {
  const what = (req.toolCall.title || req.toolCall.kind || 'a tool call').replace(/\s+/g, ' ').trim()
  const body = `${folderName(cwd)}: ${what}`
  return {
    title: `${agent} is waiting for your approval`,
    body: body.length > MAX_BODY ? `${body.slice(0, MAX_BODY - 1)}…` : body,
  }
}

/**
 * Requests that were notified and are not settled yet (so one is never
 * announced twice), and those counted in the Dock badge since the window last
 * had focus.
 */
export interface Approvals {
  open: ReadonlySet<string>
  counted: ReadonlySet<string>
}

export const emptyApprovals: Approvals = { open: new Set(), counted: new Set() }

export const approvalKey = (sessionId: string, requestId: string) => `${sessionId}|${requestId}`

/** A request arrived; notify says whether to announce it (once, and only in the background). */
export function opened(a: Approvals, key: string, background: boolean): { state: Approvals; notify: boolean } {
  if (a.open.has(key)) return { state: a, notify: false }
  return {
    state: { open: new Set(a.open).add(key), counted: background ? new Set(a.counted).add(key) : a.counted },
    notify: background,
  }
}

const without = (s: ReadonlySet<string>, drop: (k: string) => boolean) => new Set([...s].filter((k) => !drop(k)))

/** A request was answered or expired. */
export function settled(a: Approvals, key: string): Approvals {
  return { open: without(a.open, (k) => k === key), counted: without(a.counted, (k) => k === key) }
}

/** A turn ended: whatever it still asked is gone. */
export function settledSession(a: Approvals, sessionId: string): Approvals {
  const mine = (k: string) => k.startsWith(`${sessionId}|`)
  return { open: without(a.open, mine), counted: without(a.counted, mine) }
}

/** The window got focus: the user has seen the badge. */
export function focused(a: Approvals): Approvals {
  return a.counted.size === 0 ? a : { open: a.open, counted: new Set() }
}

/** The Dock badge: finished work plus requests still waiting. */
export const badgeCount = (missed: number, a: Approvals): number => missed + a.counted.size
