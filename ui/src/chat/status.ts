// Pure helpers behind the chat components: scroll following, tool status and the
// context gauge's level. Kept free of React and the DOM so node:test covers them.

/** How close to the bottom (px) still counts as "following the stream". */
export const FOLLOW_THRESHOLD = 40

export type ScrollBox = { scrollHeight: number; scrollTop: number; clientHeight: number }

/** True while the reader is at (or within the threshold of) the bottom of the transcript. */
export function isNearBottom(box: ScrollBox, threshold = FOLLOW_THRESHOLD): boolean {
  return box.scrollHeight - box.scrollTop - box.clientHeight < threshold
}

/** ACP tool-call states. `cancelled`: left running by a turn that was stopped or failed. */
export type ToolCallStatus = 'pending' | 'in_progress' | 'completed' | 'failed' | 'cancelled'

const STATUSES: readonly string[] = ['pending', 'in_progress', 'completed', 'failed', 'cancelled']

/** A status from the agent, with unknown or missing values treated as pending. */
export function toolStatus(status: string | null | undefined): ToolCallStatus {
  return status && STATUSES.includes(status) ? (status as ToolCallStatus) : 'pending'
}

/** Pending or running: the only states that may animate (and only while the turn runs). */
export function isRunning(status: ToolCallStatus): boolean {
  return status === 'pending' || status === 'in_progress'
}

export type ContextLevel = 'ok' | 'warn' | 'full'

/** The context gauge's colour: amber from 70 %, red from 90 %. */
export function contextLevel(percent: number): ContextLevel {
  if (percent >= 90) return 'full'
  if (percent >= 70) return 'warn'
  return 'ok'
}
