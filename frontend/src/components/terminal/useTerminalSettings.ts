// Keeps an xterm.js instance in step with Settings ▸ Terminal and the theme.
import { useEffect, type RefObject } from 'react'
import type { Terminal } from '@xterm/xterm'
import { useSettings } from '../../store/settings'
import { terminalOptions, type TerminalOptions } from '../../lib/terminalTheme'
import { fitTerminal } from '../../lib/terminalFit'

/** Options for a new terminal from the current settings. */
export function currentTerminalOptions(sizeDelta = 0): TerminalOptions {
  const { settings, theme } = useSettings.getState()
  return terminalOptions(settings.terminal, theme, sizeDelta)
}

/**
 * Applies font and color changes to the terminal while it is visible (a hidden one
 * can't measure its new cell size; it catches up when shown) and refits it to keep
 * the PTY's rows and columns right.
 */
export function useTerminalSettings(term: RefObject<Terminal | null>, active: boolean, sizeDelta = 0): void {
  const font = useSettings((s) => s.settings.terminal)
  const theme = useSettings((s) => s.theme)
  useEffect(() => {
    const xterm = term.current
    if (!xterm || !active) return
    if (apply(xterm, terminalOptions(font, theme, sizeDelta))) fitTerminal(xterm)
  }, [term, active, font, theme, sizeDelta])
}

/** Sets the options that changed; true when the cell size may have. */
function apply(xterm: Terminal, next: TerminalOptions): boolean {
  if (xterm.options.theme !== next.theme) xterm.options.theme = next.theme
  let metrics = false
  if (xterm.options.fontFamily !== next.fontFamily) {
    xterm.options.fontFamily = next.fontFamily
    metrics = true
  }
  if (xterm.options.fontSize !== next.fontSize) {
    xterm.options.fontSize = next.fontSize
    metrics = true
  }
  if (xterm.options.lineHeight !== next.lineHeight) {
    xterm.options.lineHeight = next.lineHeight
    metrics = true
  }
  return metrics
}
