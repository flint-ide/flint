import { useEffect, type ReactNode } from 'react'

interface Props {
  title?: string
  onClose: () => void
  children: ReactNode
  className?: string
}

export function Modal({ title, onClose, children, className = '' }: Props) {
  useEffect(() => {
    // Esc that something inside already used (stopping an agent in a chat, closing a menu) does not also close the dialog.
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !e.defaultPrevented && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`modal ${className}`} role="dialog" aria-label={title}>
        {title && <div className="modal-title">{title}</div>}
        {children}
      </div>
    </div>
  )
}
