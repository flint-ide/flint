import { useEffect, useRef } from 'react'
import { Terminal as XTerm, type ILink } from '@xterm/xterm'
import { WebglAddon } from '@xterm/addon-webgl'
import '@xterm/xterm/css/xterm.css'
import { BrowserOpenURL, ClipboardGetText, ClipboardSetText } from '../../wails/runtime'
import {
  TerminalBackend,
  attachSink,
  claimStart,
  registerOutputReader,
  registerSelectionReader,
  sendInput,
  setProgramTitle,
  type TerminalTab,
} from '../../store/terminal'
import { useWorkspace } from '../../store/workspace'
import { useEditor } from '../../store/editor'
import * as DockerBackend from '../../../bindings/flint/internal/features/docker/api'
import { findPathLinks, resolvePath } from '../../lib/pathLinks'
import { showToast } from '../../store/dialog'
import { errorMessage } from '../../api/backend'
import { focusSoon } from '../../lib/focusSoon'
import { fitTerminal } from '../../lib/terminalFit'
import { registerUrlLinks } from '../../lib/terminalLinks'
import { NEWLINE_SEQUENCE, isNewlineKey, terminalClipboardKey } from '../../lib/terminalKeys'
import { sendTerminalSelectionToAgent } from '../../lib/agentContext'
import type { DropdownItem } from '../ui/Dropdown'
import { openContextMenu } from '../../store/contextMenu'
import { currentTerminalOptions, useTerminalSettings } from './useTerminalSettings'
import { formatKeys } from '../../lib/commands'
import { modKey, isMac, modifierName } from '../../lib/platform'

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
      ...currentTerminalOptions(),
      cursorBlink: true,
      scrollback: 10_000,
      allowProposedApi: true,
      macOptionIsMeta: false, // Option composes Polish diacritics (ą, ę, ż…)
      macOptionClickForcesSelection: true,
      // OSC 8 hyperlinks printed by programs: only http(s), opened by the
      // system browser (xterm's default would window.open any scheme).
      linkHandler: {
        allowNonHttpProtocols: false,
        activate: (event, uri) => {
          if (modKey(event) && /^https?:\/\//i.test(uri)) BrowserOpenURL(uri)
        },
      },
    })
    xterm.open(host.current)
    registerUrlLinks(xterm, BrowserOpenURL)
    fitTerminal(xterm)
    term.current = xterm

    xterm.attachCustomKeyEventHandler((e) => {
      // Shift+Enter inserts a newline in claude / codex prompts (ESC + CR, like Option+Enter).
      // The whole key is swallowed: the keypress after an unhandled keydown would
      // otherwise reach xterm, which sends a plain CR and submits the prompt.
      if (isNewlineKey(e)) {
        if (e.type === 'keydown') sendInput(tab.id, NEWLINE_SEQUENCE)
        e.preventDefault()
        return false
      }
      if (e.type !== 'keydown') return true
      // Linux terminals copy and paste with Ctrl+Shift+C / V; Ctrl+C stays an interrupt.
      if (!isMac()) {
        const action = terminalClipboardKey(e, xterm.hasSelection())
        if (!action) return true
        if (action === 'copy') void ClipboardSetText(xterm.getSelection())
        if (action === 'paste') void ClipboardGetText().then((text) => text && xterm.paste(text))
        e.preventDefault()
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
            if (!modKey(e)) return
            // Relative paths in agent output are relative to the terminal's project.
            const root = tab.project || useWorkspace.getState().root || tab.cwd
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
    const unregisterSelection = registerSelectionReader(tab.id, () => xterm.getSelection())
    const detach = attachSink(tab.id, (data) => xterm.write(data))
    const input = xterm.onData((data) => sendInput(tab.id, data))
    const resize = xterm.onResize(({ cols, rows }) => void TerminalBackend.Resize(tab.id, cols, rows))
    const retitle = xterm.onTitleChange((title) => setProgramTitle(tab.id, title))

    // The PTY outlives this view (remounts, StrictMode); it is closed with the tab.
    if (claimStart(tab.id)) {
      startSession(tab, xterm.cols, xterm.rows)
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
      retitle.dispose()
      unregisterReader()
      unregisterSelection()
      detach()
      xterm.dispose()
      term.current = null
    }
    // One PTY per tab for the component's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.id])

  useTerminalSettings(term, active)

  // GPU rendering keeps agent output floods smooth, but each WebGL context holds a
  // canvas and texture atlas: only the visible terminal gets one. Hidden ones use the
  // DOM renderer, which redraws from the same buffer, so no output is lost.
  useEffect(() => {
    const xterm = term.current
    if (!active || !xterm) return
    let webgl: WebglAddon | null = null
    const release = () => {
      const addon = webgl
      webgl = null
      try {
        addon?.dispose()
      } catch {
        // Already gone with its terminal.
      }
    }
    try {
      webgl = new WebglAddon()
      webgl.onContextLoss(release)
      xterm.loadAddon(webgl)
    } catch {
      release() // falls back to the DOM renderer
    }
    return release
  }, [active, tab.id])

  useEffect(() => {
    if (!active) return
    return focusSoon(() => {
      if (!term.current) return
      fitTerminal(term.current)
      term.current.focus()
    })
  }, [active])

  const menuItems = (): DropdownItem[] => {
    const selected = !!term.current?.hasSelection()
    return [
      {
        label: 'Send to agent',
        hint: `${modifierName('Alt')} sends now`,
        kbd: isMac() ? formatKeys('Mod+L') : undefined, // Ctrl+L belongs to the shell on Linux
        disabled: !selected,
        onSelect: (e) => void sendTerminalSelectionToAgent(tab.id, e.altKey),
      },
      {
        label: 'Copy',
        kbd: formatKeys(isMac() ? 'Mod+C' : 'Mod+Shift+C'),
        disabled: !selected,
        onSelect: () => void navigator.clipboard.writeText(term.current?.getSelection() ?? '').catch(() => undefined),
      },
    ]
  }

  return (
    <div
      className="terminal-view"
      ref={host}
      onContextMenu={(e) => {
        e.preventDefault()
        openContextMenu(e.clientX, e.clientY, menuItems())
      }}
    />
  )
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

/** Starts what the tab shows: a local shell, or a Docker log / container shell stream. */
function startSession(tab: TerminalTab, cols: number, rows: number): Promise<void> {
  if (tab.docker?.kind === 'logs') return DockerBackend.OpenLogs(tab.id, tab.docker.container)
  if (tab.docker?.kind === 'shell') return DockerBackend.OpenShell(tab.id, tab.docker.container, cols, rows)
  return TerminalBackend.Start(tab.id, tab.cwd, cols, rows, tab.env ?? {})
}
