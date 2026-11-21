/**
 * Puts focus back on `el` if focus was lost (it is on the body or nowhere), e.g.
 * when the menu or dialog that had it unmounted. Focus something else took on
 * purpose (an editor a dialog opened) is left alone.
 */
let menuReturn: Element | null = null

/** Menus note where focus goes back to as they close (their trigger, what had focus). */
export function noteMenuReturn(el: Element | null): void {
  menuReturn = el
}

/**
 * What a dialog gives focus back to: what has focus now, or, when that is a row of
 * a menu (the dialog was opened from a menu item and the row is about to unmount),
 * what that menu gives focus back to.
 */
export function focusOrigin(): Element | null {
  const active = document.activeElement
  return active?.closest('[role="menu"]') ? menuReturn : active
}

export function returnFocusIfLost(el: Element | null): void {
  const active = document.activeElement
  if (!(el instanceof HTMLElement) || !el.isConnected) return
  if (active && active !== document.body) return
  el.focus()
}
