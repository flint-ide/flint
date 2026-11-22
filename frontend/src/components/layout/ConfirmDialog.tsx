import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@flint-ide/ui'
import { useRef } from 'react'
import { confirmVariant, splitConfirm } from '../../lib/confirmText'
import { useDialog } from '../../store/dialog'

/**
 * What confirmAction() and chooseAction() ask, as WunderUI's AlertDialog: the
 * message's question as the title, the rest below it, Cancel, an optional
 * alternate action and the confirm button (red when it loses something). The
 * confirm button has the focus; Escape cancels.
 */
export function ConfirmDialog() {
  const confirm = useDialog((s) => s.confirm)
  const confirmRef = useRef<HTMLButtonElement>(null)
  if (!confirm) return null
  const { title, description } = splitConfirm(confirm.message)
  return (
    <AlertDialog open onOpenChange={(open) => open || confirm.resolve(false)}>
      <AlertDialogContent className="confirm-dialog" initialFocus={confirmRef}>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description && <AlertDialogDescription>{description}</AlertDialogDescription>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          {confirm.alternateLabel && (
            <AlertDialogAction variant="secondary" onClick={() => confirm.resolve(false, true)}>
              {confirm.alternateLabel}
            </AlertDialogAction>
          )}
          <AlertDialogAction ref={confirmRef} variant={confirmVariant(confirm.confirmLabel)} onClick={() => confirm.resolve(true)}>
            {confirm.confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
