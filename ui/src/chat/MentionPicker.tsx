// The @-mention popup: a listbox over the composer, driven by useMentionPicker.
// The textarea keeps focus the whole time (aria-activedescendant points at the
// active row), so this is a plain portal rather than the kit's Popover, whose
// popup manages focus and treats the textarea as an outside press. It lines up
// with the whole composer, not the caret: caret coordinates in a WebKit textarea
// are fragile, the composer's box is not.
import { Spinner } from '@wunderui/react'
import { type CSSProperties, type ReactNode, type RefObject, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { MentionGroup, MentionItem, MentionRange } from './mention'
import type { MentionPickerState } from './useMentionPicker'

export type MentionPickerProps<T extends MentionItem> = {
  readonly picker: MentionPickerState<T>
  /** The textarea (or any element inside the composer). */
  readonly anchor: RefObject<HTMLElement | null>
  /** The anchor's ancestor the popup spans; '.fui-chat-composer' by default, the anchor itself when there is none. */
  readonly anchorClosest?: string
  readonly emptyText?: ReactNode
  readonly loadingText?: ReactNode
  /** Shown when the search fails; the error's message follows it. */
  readonly errorText?: ReactNode
  /** The popup's height limit in px (less when the window has no room). */
  readonly maxHeight?: number
  readonly className?: string
}

type Place = { readonly left: number; readonly width: number; readonly top?: number; readonly bottom?: number; readonly maxHeight: number }

const GAP = 4
const MARGIN = 8
const MIN_ROOM = 120

function anchorBox(anchor: HTMLElement, closest: string): HTMLElement {
  return (closest && anchor.closest<HTMLElement>(closest)) || anchor
}

/** Above the box, unless there is little room above and more below. */
function placeAt(rect: DOMRect, viewHeight: number, max: number): Place {
  const above = rect.top - GAP - MARGIN
  const below = viewHeight - rect.bottom - GAP - MARGIN
  const base = { left: rect.left, width: rect.width }
  if (above >= Math.min(max, MIN_ROOM) || above >= below) {
    return { ...base, bottom: viewHeight - rect.top + GAP, maxHeight: Math.max(0, Math.min(max, above)) }
  }
  return { ...base, top: rect.bottom + GAP, maxHeight: Math.max(0, Math.min(max, below)) }
}

function samePlace(a: Place | null, b: Place): boolean {
  return a !== null && a.left === b.left && a.width === b.width && a.top === b.top && a.bottom === b.bottom && a.maxHeight === b.maxHeight
}

/** Follows the composer while open: window resizes, any scroll, the composer growing. */
function usePlacement(open: boolean, anchor: RefObject<HTMLElement | null>, closest: string, max: number): Place | null {
  const [place, setPlace] = useState<Place | null>(null)
  useLayoutEffect(() => {
    const el = anchor.current
    if (!open || !el) return undefined
    const box = anchorBox(el, closest)
    let frame = 0
    const measure = () => {
      const next = placeAt(box.getBoundingClientRect(), window.innerHeight, max)
      setPlace((p) => (samePlace(p, next) ? p : next))
    }
    const schedule = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(measure)
    }
    measure()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(schedule)
    observer?.observe(box)
    window.addEventListener('resize', schedule)
    window.addEventListener('scroll', schedule, true)
    return () => {
      cancelAnimationFrame(frame)
      observer?.disconnect()
      window.removeEventListener('resize', schedule)
      window.removeEventListener('scroll', schedule, true)
    }
  }, [open, anchor, closest, max])
  return open ? place : null
}

/** Closes the picker on a pointer press outside both the popup and the textarea. */
function useOutsidePress(open: boolean, popup: RefObject<HTMLElement | null>, anchor: RefObject<HTMLElement | null>, close: () => void) {
  const latest = useRef(close)
  useLayoutEffect(() => {
    latest.current = close
  })
  useEffect(() => {
    if (!open) return undefined
    const onDown = (e: PointerEvent) => {
      const target = e.target instanceof Node ? e.target : null
      if (target && (popup.current?.contains(target) || anchor.current?.contains(target))) return
      latest.current()
    }
    document.addEventListener('pointerdown', onDown, true)
    return () => document.removeEventListener('pointerdown', onDown, true)
  }, [open, popup, anchor])
}

/** Keeps the active row (and its group header, for a group's first row) inside the list's scroll box. */
function useActiveInView(list: RefObject<HTMLElement | null>, index: number, open: boolean) {
  useLayoutEffect(() => {
    const box = list.current
    const row = box?.querySelector<HTMLElement>(`[data-index="${index}"]`)
    if (!box || !row) return
    const header = row.previousElementSibling instanceof HTMLElement && row.previousElementSibling.dataset.header ? row.previousElementSibling : null
    const top = (header ?? row).offsetTop
    const bottom = row.offsetTop + row.offsetHeight
    if (top < box.scrollTop) box.scrollTop = top
    else if (bottom > box.scrollTop + box.clientHeight) box.scrollTop = bottom - box.clientHeight
  }, [list, index, open])
}

function Highlighted({ text, hit }: { readonly text: string; readonly hit: MentionRange | null }) {
  if (!hit || hit.start >= hit.end) return <>{text}</>
  return (
    <>
      {text.slice(0, hit.start)}
      <mark className="fui-mention-hit">{text.slice(hit.start, hit.end)}</mark>
      {text.slice(hit.end)}
    </>
  )
}

function Group<T extends MentionItem>({ group, picker }: { readonly group: MentionGroup<T>; readonly picker: MentionPickerState<T> }) {
  const headerId = `${picker.listId}-g${group.offset}`
  return (
    <div role="group" aria-labelledby={headerId} className="fui-mention-group">
      <div id={headerId} role="presentation" data-header="true" className="fui-mention-header">
        {group.name}
      </div>
      {group.matches.map(({ item, hit }, k) => {
        const index = group.offset + k
        const active = index === picker.activeIndex
        return (
          <div
            key={`${index}:${item.id}`}
            id={picker.optionId(index)}
            role="option"
            aria-selected={active}
            data-index={index}
            className="fui-mention-row"
            onMouseMove={() => (active ? undefined : picker.setActiveIndex(index))}
            onClick={() => picker.select(item)}
          >
            {item.icon && <span className="fui-mention-icon">{item.icon}</span>}
            <span className="fui-mention-label">
              <Highlighted text={item.label} hit={hit} />
            </span>
            {item.detail && <span className="fui-mention-detail">{item.detail}</span>}
          </div>
        )
      })}
    </div>
  )
}

function Status<T extends MentionItem>({ picker, emptyText, loadingText, errorText }: Pick<MentionPickerProps<T>, 'picker' | 'emptyText' | 'loadingText' | 'errorText'>) {
  if (picker.loading) {
    return (
      <div role="status" className="fui-mention-status">
        <Spinner aria-hidden />
        {loadingText}
      </div>
    )
  }
  if (picker.error) {
    return (
      <div role="alert" className="fui-mention-status" data-tone="error" title={picker.error.message}>
        {errorText} {picker.error.message}
      </div>
    )
  }
  if (picker.items.length > 0) return null
  return (
    <div role="status" className="fui-mention-status">
      {emptyText}
    </div>
  )
}

/**
 * The popup for useMentionPicker: grouped rows (icon, label with the match
 * emphasised, muted detail), a loading line while a search runs, an empty or
 * error line. Rows activate on hover and insert on click; pressing anywhere in it
 * keeps the textarea's focus. It closes on a press outside it and the textarea.
 */
export function MentionPicker<T extends MentionItem>({
  picker,
  anchor,
  anchorClosest = '.fui-chat-composer',
  emptyText = 'No matches',
  loadingText = 'Searching…',
  errorText = 'Search failed:',
  maxHeight = 320,
  className,
}: MentionPickerProps<T>) {
  const popup = useRef<HTMLDivElement | null>(null)
  const place = usePlacement(picker.open, anchor, anchorClosest, maxHeight)
  useOutsidePress(picker.open, popup, anchor, picker.close)
  useActiveInView(popup, picker.activeIndex, place !== null)
  if (!place || typeof document === 'undefined') return null
  const style: CSSProperties = { left: place.left, width: place.width, top: place.top, bottom: place.bottom, maxHeight: place.maxHeight }
  return createPortal(
    <div
      ref={popup}
      className={className ? `fui-mention-popup ${className}` : 'fui-mention-popup'}
      data-side={place.top === undefined ? 'top' : 'bottom'}
      style={style}
      onMouseDown={(e) => e.preventDefault()}
    >
      <div
        id={picker.listId}
        role="listbox"
        aria-label="Mentions"
        aria-busy={picker.loading}
        data-stale={picker.stale || undefined}
      >
        {picker.groups.map((g) => (
          <Group key={g.name} group={g} picker={picker} />
        ))}
      </div>
      <Status picker={picker} emptyText={emptyText} loadingText={loadingText} errorText={errorText} />
    </div>,
    document.body,
  )
}
