import { Tooltip, TooltipContent, TooltipTrigger } from '@wunderui/react'
import { type ReactNode, useId } from 'react'

/** The bar along the bottom of a window or panel. Put StatusItems in it; a StatusSpacer pushes the rest right. */
export function StatusBar({ children, className }: { children: ReactNode; className?: string }) {
  return <footer className={className ? `fui-statusbar ${className}` : 'fui-statusbar'}>{children}</footer>
}

export function StatusSpacer() {
  return <span className="fui-grow" />
}

export type StatusItemProps = {
  children: ReactNode
  /** Tooltip text (line breaks kept); also the accessible name when the item shows only an icon. */
  tip: string
  onClick?: () => void
  disabled?: boolean
  /** A toggle that is on, e.g. annotations shown. */
  pressed?: boolean
  /** Extra class, e.g. a colour for the item's state. */
  className?: string
}

/** One clickable entry of a StatusBar, with a tooltip above it. */
export function StatusItem({ children, tip, onClick, disabled, pressed, className }: StatusItemProps) {
  // The tooltip shows on hover and focus only; screen readers get the tip as the
  // button's description from a hidden copy.
  const tipId = useId()
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            className={className ? `fui-statusitem ${className}` : 'fui-statusitem'}
            disabled={disabled}
            aria-pressed={pressed}
            aria-describedby={tipId}
            onClick={onClick}
          />
        }
      >
        {children}
        <span id={tipId} hidden>
          {tip}
        </span>
      </TooltipTrigger>
      <TooltipContent side="top" className="fui-statustip">
        {tip}
      </TooltipContent>
    </Tooltip>
  )
}
