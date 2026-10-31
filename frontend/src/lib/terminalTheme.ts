// xterm.js colors and font options per theme. Terminals draw on a canvas, so the
// palette can't come from CSS variables; these mirror --bg-editor / --fg / --accent
// of each palette in style.css.
import type { FontSettings, ResolvedTheme } from './settings'

export interface TerminalColors {
  background: string
  foreground: string
  cursor: string
  cursorAccent: string
  selectionBackground: string
  black: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  white: string
  brightBlack: string
  brightRed: string
  brightGreen: string
  brightYellow: string
  brightBlue: string
  brightMagenta: string
  brightCyan: string
  brightWhite: string
}

export const TERMINAL_THEMES: Readonly<Record<ResolvedTheme, Readonly<TerminalColors>>> = Object.freeze({
  dark: Object.freeze({
    background: '#141518',
    foreground: '#d4d4d8',
    cursor: '#f59e0b',
    cursorAccent: '#141518',
    selectionBackground: '#3b3f4a',
    black: '#1f2026', red: '#f87171', green: '#86efac', yellow: '#fcd34d',
    blue: '#93c5fd', magenta: '#d8b4fe', cyan: '#67e8f9', white: '#d4d4d8',
    brightBlack: '#6b7280', brightRed: '#fca5a5', brightGreen: '#bbf7d0', brightYellow: '#fde68a',
    brightBlue: '#bfdbfe', brightMagenta: '#e9d5ff', brightCyan: '#a5f3fc', brightWhite: '#fafafa',
  }),
  // ANSI colors dark enough to read on white (CLIs assume "white" is a dim gray there).
  light: Object.freeze({
    background: '#ffffff',
    foreground: '#1f2328',
    cursor: '#b45309',
    cursorAccent: '#ffffff',
    selectionBackground: '#c9d8f2',
    black: '#24292f', red: '#cf222e', green: '#116329', yellow: '#7d4e00',
    blue: '#0969da', magenta: '#8250df', cyan: '#1b7c83', white: '#6e7781',
    brightBlack: '#57606a', brightRed: '#a40e26', brightGreen: '#1a7f37', brightYellow: '#633c01',
    brightBlue: '#218bff', brightMagenta: '#a475f9', brightCyan: '#3192aa', brightWhite: '#8c959f',
  }),
})

export interface TerminalOptions {
  theme: Readonly<TerminalColors>
  fontFamily: string
  fontSize: number
  lineHeight: number
}

/** Options for an xterm instance; sizeDelta makes secondary consoles (runs) a bit smaller. */
export function terminalOptions(font: FontSettings, theme: ResolvedTheme, sizeDelta = 0): TerminalOptions {
  return {
    theme: TERMINAL_THEMES[theme],
    fontFamily: font.fontFamily,
    fontSize: Math.max(6, font.fontSize + sizeDelta),
    lineHeight: font.lineHeight,
  }
}
