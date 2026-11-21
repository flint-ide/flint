import { Dialog, DialogContent, DialogTitle } from '@flint-ide/ui'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { modalDismiss } from '../../lib/modalDismiss'
import { focusOrigin, returnFocusIfLost } from '../../lib/returnFocus'

interface Props {
  title?: string
  onClose: () => void
  children: ReactNode
  className?: string
}

/**
 * Flint's dialog: the UI kit's Dialog (Base UI) under the app's `.modal` look,
 * opened near the top like a palette. It traps focus, hides the page from
 * assistive tech and gives focus back to what had it before when it closes.
 *
 * It closes the way the dialog it replaces did: Escape that nothing inside used
 * (stopping an agent, closing a menu) while it is on top and focus is in it, or a
 * press on its own backdrop. A click in a toast, a context menu or a dialog over it leaves it open.
 */
export function Modal({ title, onClose, children, className = '' }: Props) {
  const popup = useRef<HTMLDivElement>(null)
  // What had focus before it opened (read on the first render, before autoFocus moves
  // it). Focus goes back there only if it is lost when the dialog unmounts; the kit's
  // own return (finalFocus) is off: it would pick a stale element, or undo a dialog
  // action that focused something on purpose.
  const [opener] = useState(focusOrigin)
  useEffect(() => () => queueMicrotask(() => returnFocusIfLost(opener)), [opener])

  return (
    <Dialog
      open
      onOpenChange={(open, details) => {
        if (open) return
        const dismiss = modalDismiss(details.reason, details.event, popup.current, document.activeElement, document.body, isTopmost(popup.current))
        if (dismiss) onClose()
        else details.cancel()
      }}
    >
      <DialogContent
        ref={popup}
        className={`modal ${className}`}
        overlayClassName="modal-backdrop"
        showCloseButton={false}
        finalFocus={false}
        // Focus a dialog already put inside (autoFocus, an effect focusing its input) stays;
        // otherwise the kit focuses the first control.
        initialFocus={() => !popup.current?.contains(document.activeElement)}
        aria-label={title ? undefined : 'Dialog'}
      >
        {title && <DialogTitle className="modal-title">{title}</DialogTitle>}
        {children}
      </DialogContent>
    </Dialog>
  )
}

/** Dialogs open later sit later in the body (each in its own portal): the last one is on top. */
function isTopmost(popup: HTMLElement | null): boolean {
  const open = document.querySelectorAll('[data-slot="dialog-content"], [data-slot="alert-dialog-content"]')
  return popup !== null && open[open.length - 1] === popup
}
