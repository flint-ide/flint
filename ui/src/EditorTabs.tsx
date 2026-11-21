import { IconButton } from '@wunderui/react'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'

export type EditorTab = {
  id: string
  title: string
  icon?: ReactNode
  /** Unsaved changes: the close button shows a dot until hovered. */
  dirty?: boolean
}

export type EditorTabsProps = {
  tabs: readonly EditorTab[]
  active: string
  onSelect: (id: string) => void
  onClose?: (id: string) => void
}

/** A strip of editor tabs with an accent line on the active one. */
export function EditorTabs({ tabs, active, onSelect, onClose }: EditorTabsProps) {
  return (
    <div role="tablist" className="fui-tabs">
      {tabs.map((t) => {
        const selected = t.id === active
        return (
          <div
            key={t.id}
            role="tab"
            aria-selected={selected}
            tabIndex={selected ? 0 : -1}
            className="fui-tab"
            onClick={() => onSelect(t.id)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') onSelect(t.id)
            }}
          >
            {t.icon}
            <span className="fui-tab-title">{t.title}</span>
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
