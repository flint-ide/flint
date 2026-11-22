/** The part of a DOM node the decision needs (kept small so it is testable without a DOM). */
export interface NodeLike {
  contains(other: unknown): boolean
  readonly parentElement?: NodeLike | null
}

interface DismissEvent {
  readonly defaultPrevented: boolean
  readonly target: unknown
}

/**
 * Whether a Modal closes when the UI kit's Dialog asks to (onOpenChange(false)).
 * Escape: only for the topmost dialog, if nothing inside already used it and focus
 * is in it (or nowhere); with focus in a menu or a toast, that one gets the key.
 * A press outside: only on the dialog's own backdrop, the popup's sibling in its
 * portal. A close button or a programmatic close closes it; any other reason
 * (focus moving out, should the dialog ever stop trapping it) does not.
 */
export function modalDismiss(
  reason: string,
  event: DismissEvent | undefined,
  popup: NodeLike | null,
  active: unknown,
  body: unknown,
  topmost: boolean,
): boolean {
  if (reason === 'escape-key') {
    if (!topmost || event?.defaultPrevented) return false
    return active == null || active === body || (popup?.contains(active) ?? false)
  }
  if (reason === 'outside-press') {
    const portal = popup?.parentElement
    const target = event?.target
    if (!popup || !portal || target == null) return false
    return portal.contains(target) && !popup.contains(target)
  }
  return reason === 'close-press' || reason === 'imperative-action'
}
