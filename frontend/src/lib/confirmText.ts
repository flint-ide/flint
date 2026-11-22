// How a confirmAction()/chooseAction() message is laid out in the alert dialog
// (components/layout/ConfirmDialog.tsx). Callers pass one string, usually a
// question followed by its consequence: 'Delete connection "prod"? Its password
// is removed from the keychain.'

export interface ConfirmText {
  readonly title: string
  readonly description?: string
}

/** Longest question that still reads as a title. */
const MAX_TITLE = 120

/**
 * Splits a message after its first question: the question becomes the title, the
 * rest the description. A message whose question comes last, or is too long, is
 * all title.
 */
export function splitConfirm(message: string): ConfirmText {
  const text = message.trim()
  const end = text.search(/\?(\s+)\S/)
  if (end < 0 || end + 1 > MAX_TITLE) return { title: text }
  return { title: text.slice(0, end + 1), description: text.slice(end + 1).trim() }
}

const DESTRUCTIVE = /\b(delete|remove|discard|revert|clear|drop|reset|force|overwrite|kill|stop|uninstall|close|abort)\b/i

/** A confirm button that loses something is red; one that goes ahead (OK, Trust Folder, Choose Folder…) is the accent. */
export function confirmVariant(confirmLabel: string): 'destructive' | 'primary' {
  return DESTRUCTIVE.test(confirmLabel) ? 'destructive' : 'primary'
}
