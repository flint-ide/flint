import { createStore } from '../lib/store'
import { createToastChannel, type ToastKind, type ToastSink, toastDuration } from '../lib/toastChannel'

interface ConfirmRequest {
  message: string
  confirmLabel: string
  /** A second way forward next to the confirm button (chooseAction). */
  alternateLabel?: string
  resolve: (ok: boolean, alternate?: boolean) => void
}

interface DialogState {
  confirm: ConfirmRequest | null
}

export const useDialog = createStore<DialogState>(() => ({ confirm: null }))

/** Shows an in-app confirmation (native window.confirm isn't available in the webview). */
export function confirmAction(message: string, confirmLabel = 'OK'): Promise<boolean> {
  return new Promise((resolve) => {
    useDialog.setState({
      confirm: {
        message,
        confirmLabel,
        resolve: (ok) => {
          useDialog.setState({ confirm: null })
          resolve(ok)
        },
      },
    })
  })
}

/** A confirmation with two ways forward: the confirm button, an alternate one, or Cancel. */
export function chooseAction(message: string, confirmLabel: string, alternateLabel: string): Promise<'confirm' | 'alternate' | 'cancel'> {
  return new Promise((resolve) => {
    useDialog.setState({
      confirm: {
        message,
        confirmLabel,
        alternateLabel,
        resolve: (ok, alternate) => {
          useDialog.setState({ confirm: null })
          resolve(alternate ? 'alternate' : ok ? 'confirm' : 'cancel')
        },
      },
    })
  })
}

const toasts = createToastChannel()

/** Attaches the toast stack (components/layout/Toasts.tsx); returns the detach function. */
export function connectToasts(sink: ToastSink): () => void {
  return toasts.connect(sink)
}

/** A passing notification in the bottom right corner. Toasts stack; the same message twice shows once. */
export function showToast(message: string, kind: ToastKind = 'info', ms = toastDuration(kind)): void {
  toasts.send({ message, kind, ms })
}
