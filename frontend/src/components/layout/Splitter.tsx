import { useRef } from 'react'

interface Props {
  direction: 'horizontal' | 'vertical'
  /** Called with the pointer position as a ratio (0..1) of the parent's size. */
  onResize: (ratio: number) => void
  hidden?: boolean
}

/** Draggable divider between two flex children. */
export function Splitter({ direction, onResize, hidden }: Props) {
  const ref = useRef<HTMLDivElement>(null)

  const onPointerDown = (e: React.PointerEvent) => {
    const parent = ref.current?.parentElement
    if (!parent) return
    e.preventDefault()
    const rect = parent.getBoundingClientRect()
    const move = (ev: PointerEvent) => {
      const ratio =
        direction === 'vertical' ? (ev.clientY - rect.top) / rect.height : (ev.clientX - rect.left) / rect.width
      onResize(ratio)
    }
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      document.body.classList.remove('resizing')
    }
    document.body.classList.add('resizing')
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
  }

  return <div ref={ref} className={`splitter ${direction}`} onPointerDown={onPointerDown} hidden={hidden} />
}
