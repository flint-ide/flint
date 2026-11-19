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
