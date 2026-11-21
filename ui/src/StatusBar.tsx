import { Tooltip, TooltipContent, TooltipTrigger } from '@wunderui/react'
import type { ReactNode } from 'react'

/** The bar along the bottom of a window or panel. Put StatusItems in it; a StatusSpacer pushes the rest right. */
export function StatusBar({ children }: { children: ReactNode }) {
  return <footer className="fui-statusbar">{children}</footer>
}

export function StatusSpacer() {
  return <span className="fui-grow" />
}

export type StatusItemProps = {
  children: ReactNode
  /** Tooltip text; also the accessible name when the item shows only an icon. */
  tip: string
  onClick?: () => void
}

/** One clickable entry of a StatusBar, with a tooltip above it. */
export function StatusItem({ children, tip, onClick }: StatusItemProps) {
  return (
    <Tooltip>
      <TooltipTrigger render={<button type="button" className="fui-statusitem" onClick={onClick} />}>{children}</TooltipTrigger>
      <TooltipContent side="top">{tip}</TooltipContent>
    </Tooltip>
  )
}
