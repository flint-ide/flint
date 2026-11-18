import { createStore } from '../lib/store'

interface ConfirmRequest {
  message: string
  confirmLabel: string
  /** A second way forward next to the confirm button (chooseAction). */
  alternateLabel?: string
  resolve: (ok: boolean, alternate?: boolean) => void
}

interface DialogState {
  confirm: ConfirmRequest | null
  toast: { message: string; kind: 'info' | 'error' } | null
}

export const useDialog = createStore<DialogState>(() => ({ confirm: null, toast: null }))

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

let toastTimer: ReturnType<typeof setTimeout> | undefined

export function showToast(message: string, kind: 'info' | 'error' = 'info', ms = kind === 'error' ? 6000 : 2500): void {
  clearTimeout(toastTimer)
  useDialog.setState({ toast: { message, kind } })
  toastTimer = setTimeout(() => useDialog.setState({ toast: null }), ms)
}
