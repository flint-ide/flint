import { IconButton } from '@wunderui/react'
import { X } from 'lucide-react'
import type { MouseEvent, ReactNode } from 'react'

export type EditorTab = {
  id: string
  title: string
  icon?: ReactNode
  /** Unsaved changes: the close button shows a dot until hovered. */
  dirty?: boolean
  /** Native tooltip on the tab, e.g. the file's full path. */
  tip?: string
  /** After the title: small badges such as the machine or an agent's state. */
  extra?: ReactNode
  /** Extra class on the tab, for a state of the app's own (e.g. a dimmed, exited terminal). */
  className?: string
}

export type EditorTabsProps = {
  tabs: readonly EditorTab[]
  active: string
  onSelect: (id: string) => void
  /** Also called on a middle click, the way editors close tabs. */
  onClose?: (id: string) => void
  onContextMenu?: (id: string, e: MouseEvent<HTMLElement>) => void
  className?: string
}

/** A strip of editor tabs with an accent line on the active one. */
export function EditorTabs({ tabs, active, onSelect, onClose, onContextMenu, className }: EditorTabsProps) {
  return (
    <div role="tablist" className={className ? `fui-tabs ${className}` : 'fui-tabs'}>
      {tabs.map((t) => {
        const selected = t.id === active
        return (
          <div
            key={t.id}
            role="tab"
            aria-selected={selected}
            tabIndex={selected ? 0 : -1}
            className={t.className ? `fui-tab ${t.className}` : 'fui-tab'}
            title={t.tip}
            onClick={() => onSelect(t.id)}
            onMouseDown={(e) => {
              if (e.button === 1) onClose?.(t.id)
            }}
            onContextMenu={onContextMenu && ((e) => onContextMenu(t.id, e))}
            onKeyDown={(e) => {
              // Enter on the close button inside closes the tab; it doesn't select it first.
              if (e.target !== e.currentTarget) return
              if (e.key === 'Enter' || e.key === ' ') onSelect(t.id)
            }}
          >
            {t.icon}
            <span className="fui-tab-title">{t.title}</span>
            {t.extra}
            {onClose && (
              <IconButton
                size="sm"
                variant="ghost"
                className="fui-tab-close"
                data-dirty={t.dirty || undefined}
                aria-label={t.dirty ? `Close ${t.title} (unsaved)` : `Close ${t.title}`}
                onClick={(e) => {
                  e.stopPropagation()
                  onClose(t.id)
                }}
              >
                <span aria-hidden className="fui-tab-dot" />
                <X aria-hidden className="fui-tab-x" />
              </IconButton>
            )}
          </div>
        )
      })}
    </div>
  )
}
