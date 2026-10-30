import { createStore } from '../lib/store'

interface ConfirmRequest {
  message: string
  confirmLabel: string
  resolve: (ok: boolean) => void
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

let toastTimer: ReturnType<typeof setTimeout> | undefined

export function showToast(message: string, kind: 'info' | 'error' = 'info'): void {
  clearTimeout(toastTimer)
  useDialog.setState({ toast: { message, kind } })
  toastTimer = setTimeout(() => useDialog.setState({ toast: null }), kind === 'error' ? 6000 : 2500)
}
