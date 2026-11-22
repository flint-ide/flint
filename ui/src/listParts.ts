// Pure helpers of ListItem and TaskCard, kept apart so they can be unit tested without a DOM.

/** The meta parts worth a slot (and a separator): no null, undefined, false or empty string. 0 stays. */
export function visibleParts<T>(parts: readonly (T | null | undefined | false | '')[] | undefined): T[] {
  if (!parts) return []
  return parts.filter((p): p is T => p !== null && p !== undefined && p !== false && p !== '')
}

/** Enter and Space activate a focused row, like a button. */
export function isActivationKey(key: string): boolean {
  return key === 'Enter' || key === ' '
}

export type CheckState = 'pending' | 'success' | 'failure'

/** A CI rollup in BadgeProcessing's vocabulary (it says "error" for a failure). */
export function processingState(state: CheckState): 'pending' | 'success' | 'error' {
  return state === 'failure' ? 'error' : state
}
