// The way from showToast() (callable anywhere, React or not) to the toast stack
// (components/layout/Toasts.tsx, which holds WunderUI's toast manager). Toasts
// sent before the stack mounts wait, the last few of them, and show when it does.

export type ToastKind = 'info' | 'success' | 'warning' | 'error'

export interface ToastRequest {
  readonly message: string
  readonly kind: ToastKind
  /** How long it stays, in milliseconds. */
  readonly ms: number
}

export type ToastSink = (toast: ToastRequest) => void

export interface ToastChannel {
  send(toast: ToastRequest): void
  /** Attaches the stack; returns the function that detaches it. */
  connect(sink: ToastSink): () => void
}

/** How long a toast stays by default: errors longer, they need reading. */
export function toastDuration(kind: ToastKind): number {
  return kind === 'error' ? 6000 : kind === 'warning' ? 4000 : 2500
}

export function createToastChannel(maxPending = 4): ToastChannel {
  let sink: ToastSink | null = null
  let pending: readonly ToastRequest[] = []
  return {
    send(toast) {
      if (sink) sink(toast)
      else pending = [...pending, toast].slice(-maxPending)
    },
    connect(next) {
      sink = next
      const waiting = pending
      pending = []
      waiting.forEach(next)
      return () => {
        if (sink === next) sink = null
      }
    },
  }
}
