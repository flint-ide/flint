import { useEffect, useRef } from 'react'
import { Terminal as XTerm, type ILink } from '@xterm/xterm'
import { WebglAddon } from '@xterm/addon-webgl'
import '@xterm/xterm/css/xterm.css'
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
import { TerminalBackend, attachSink, registerOutputReader, sendInput, type TerminalTab } from '../../store/terminal'
import { useWorkspace } from '../../store/workspace'
import { useEditor } from '../../store/editor'
import { findPathLinks, resolvePath } from '../../lib/pathLinks'
import { showToast } from '../../store/dialog'
import { errorMessage } from '../../api/backend'
import { focusSoon } from '../../lib/focusSoon'
import { fitTerminal } from '../../lib/terminalFit'
import { registerUrlLinks } from '../../lib/terminalLinks'

const THEME = {
  background: '#141518',
  foreground: '#d4d4d8',
  cursor: '#f59e0b',
  cursorAccent: '#141518',
  selectionBackground: '#3b3f4a',
  black: '#1f2026', red: '#f87171', green: '#86efac', yellow: '#fcd34d',
  blue: '#93c5fd', magenta: '#d8b4fe', cyan: '#67e8f9', white: '#d4d4d8',
  brightBlack: '#6b7280', brightRed: '#fca5a5', brightGreen: '#bbf7d0', brightYellow: '#fde68a',
  brightBlue: '#bfdbfe', brightMagenta: '#e9d5ff', brightCyan: '#a5f3fc', brightWhite: '#fafafa',
}

const started = new Set<string>()

interface Props {
  tab: TerminalTab
  active: boolean
}

/** One xterm.js instance bound to a backend PTY. Stays mounted while its tab exists. */
export function TerminalView({ tab, active }: Props) {
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<XTerm | null>(null)

  useEffect(() => {
    if (!host.current) return
    const xterm = new XTerm({
      theme: THEME,
      fontFamily: '"JetBrains Mono", "SF Mono", Menlo, monospace',
      fontSize: 13,
      lineHeight: 1.2,
      cursorBlink: true,
      scrollback: 10_000,
      allowProposedApi: true,
      macOptionIsMeta: false, // Option composes Polish diacritics (ą, ę, ż…)
      macOptionClickForcesSelection: true,
    })
    xterm.open(host.current)
    registerUrlLinks(xterm, BrowserOpenURL)
    try {
      const webgl = new WebglAddon()
      webgl.onContextLoss(() => webgl.dispose())
      xterm.loadAddon(webgl) // GPU rendering keeps agent output floods smooth
    } catch {
      // Falls back to the DOM renderer.
    }
    fitTerminal(xterm)
    term.current = xterm

    xterm.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true
      // Shift+Enter inserts a newline in claude / codex prompts (ESC + CR, like Option+Enter).
      if (e.key === 'Enter' && e.shiftKey && !e.metaKey) {
        sendInput(tab.id, '\x1b\r')
        return false
      }
      if (e.metaKey && e.key === 'k') {
        xterm.clear()
        return false
      }
      // Leave other ⌘ shortcuts to the app (⌘P, ⌘J, ...) and the Edit menu (⌘C, ⌘V).
      return !e.metaKey
    })

    xterm.registerLinkProvider({
      provideLinks: (lineNo, callback) => {
        const line = xterm.buffer.active.getLine(lineNo - 1)?.translateToString(true) ?? ''
        const links: ILink[] = findPathLinks(line).map((l) => ({
          range: { start: { x: l.start + 1, y: lineNo }, end: { x: l.end, y: lineNo } },
          text: line.slice(l.start, l.end),
          decorations: { underline: true, pointerCursor: true },
          activate: (e) => {
            if (!e.metaKey) return
            const root = useWorkspace.getState().root ?? tab.cwd
            useEditor.getState().openFile(resolvePath(root, l.path), l.line ? { line: l.line, from: 0, to: 0 } : undefined)
          },
        }))
        callback(links)
      },
    })

    // Output from before a restart, dimmed, above the fresh shell (again after a
    // remount: a new xterm starts empty).
    if (tab.restored) {
      xterm.write(`\x1b[2m${tab.restored.replace(/\n/g, '\r\n')}\r\n── restored ──\x1b[0m\r\n`)
    }
    const unregisterReader = registerOutputReader(tab.id, (maxLines) => readBuffer(xterm, maxLines))
    const detach = attachSink(tab.id, (data) => xterm.write(data))
    const input = xterm.onData((data) => sendInput(tab.id, data))
    const resize = xterm.onResize(({ cols, rows }) => void TerminalBackend.Resize(tab.id, cols, rows))

    // The PTY outlives this view (remounts, StrictMode); it is closed with the tab.
    if (!started.has(tab.id)) {
      started.add(tab.id)
      TerminalBackend.Start(tab.id, tab.cwd, xterm.cols, xterm.rows)
        .then(() => tab.initialInput && sendInput(tab.id, `${tab.initialInput}\r`))
        .catch((err) => showToast(`Terminal failed: ${errorMessage(err)}`, 'error'))
    } else {
      void TerminalBackend.Resize(tab.id, xterm.cols, xterm.rows)
    }

    const observer = new ResizeObserver(() => {
      if (host.current && host.current.offsetWidth > 0) fitTerminal(xterm)
    })
    observer.observe(host.current)

    return () => {
      observer.disconnect()
      input.dispose()
      resize.dispose()
      unregisterReader()
      detach()
      xterm.dispose()
      term.current = null
    }
    // One PTY per tab for the component's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.id])

  useEffect(() => {
    if (!active) return
    return focusSoon(() => {
      if (!term.current) return
      fitTerminal(term.current)
      term.current.focus()
    })
  }, [active])

  return <div className="terminal-view" ref={host} />
}

/**
 * The last maxLines lines of the normal screen buffer as plain text (full-screen
 * apps like claude draw on the alternate screen, which is not worth keeping).
 * Soft-wrapped rows are joined back into one line.
 */
function readBuffer(xterm: XTerm, maxLines: number): string {
  const buf = xterm.buffer.normal
  const lines: string[] = []
  for (let i = Math.max(0, buf.length - maxLines * 2); i < buf.length; i++) {
    const line = buf.getLine(i)
    if (!line) continue
    const text = line.translateToString(true)
    if (line.isWrapped && lines.length > 0) lines[lines.length - 1] += text
    else lines.push(text)
  }
  while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop()
  return lines.slice(-maxLines).join('\n')
}
