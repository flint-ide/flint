import { Toaster, useToast } from '@flint-ide/ui'
import { useEffect, useRef } from 'react'
import { connectToasts } from '../../store/dialog'

/**
 * The toast stack (WunderUI's Toaster, bottom right, above the status bar). It shows
 * what showToast() sends: toasts stack, hovering the stack expands it and pauses the
 * timers, each one can be closed. A message already on screen is replaced, not repeated.
 */
export function Toasts() {
  return (
    <Toaster position="bottom-right">
      <ToastBridge />
    </Toaster>
  )
}

function ToastBridge() {
  const manager = useToast()
  const managerRef = useRef(manager)
  managerRef.current = manager

  useEffect(() => {
    const shown = new Map<string, string>()
    return connectToasts(({ message, kind, ms }) => {
      const toast = managerRef.current
      const previous = shown.get(message)
      if (previous) toast.close(previous)
      const id = toast.add({
        title: message,
        type: kind,
        timeout: ms,
        // Errors interrupt a screen reader; the rest wait their turn.
        priority: kind === 'error' ? 'high' : 'low',
        onRemove: () => {
          if (shown.get(message) === id) shown.delete(message)
        },
      })
      shown.set(message, id)
    })
  }, [])
  return null
}
