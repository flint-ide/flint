// Vim key bindings (@replit/codemirror-vim). Imported lazily by lib/editorSetup
// only when Settings ▸ Editor ▸ Vim mode is on, so the package is its own chunk
// and costs nothing otherwise.
//
// Ex commands go through the command registry, so :w and :q do exactly what
// Save and Close Tab do for the active tab; only editors that are that tab's
// document (lib/vimFacet.ts) allow them. Flint's global shortcuts keep working
// (their dispatcher runs before CodeMirror sees a key, lib/commands.ts), except
// the ⌃ keys Vim needs, which a Vim editor keeps (VIM_EDITOR, vimOwnsKey).
import { getCM, Vim, vim } from '@replit/codemirror-vim'
import { EditorView, ViewPlugin } from '@codemirror/view'
import type { Extension } from '@codemirror/state'
import { getCommand, isAvailable, runCommand, VIM_EDITOR } from './commands'
import { vimFileCommands } from './vimFacet'
import { vimModeLabel, vimStateLabel, type VimModeEvent } from './vimMode'

type ExCM = Parameters<Parameters<typeof Vim.defineEx>[2]>[0]

/**
 * Runs registry commands in order, each after the previous one finished (:wq
 * saves, then closes), when the editor is the active tab's own document.
 */
function exec(...ids: string[]): (cm: ExCM) => void {
  return (cm) => {
    if (!cm.cm6.state.facet(vimFileCommands)) {
      const note = document.createElement('span')
      note.textContent = 'Only file and scratchpad editors can be saved or closed with :w and :q'
      cm.openNotification(note, { bottom: true, duration: 4000 })
      return
    }
    runCommand({
      id: `vim.${ids.join('+')}`,
      title: ids.join(' + '),
      run: async () => {
        for (const id of ids) {
          const cmd = getCommand(id)
          if (cmd && isAvailable(cmd)) await cmd.run()
        }
      },
    })
  }
}

let exDefined = false
function defineExCommands(): void {
  if (exDefined) return
  exDefined = true
  Vim.defineEx('write', 'w', exec('editor.save'))
  Vim.defineEx('quit', 'q', exec('editor.closeTab'))
  Vim.defineEx('wq', 'wq', exec('editor.save', 'editor.closeTab'))
  Vim.defineEx('xit', 'x', exec('editor.save', 'editor.closeTab'))
}

/**
 * The Vim extension; onMode gets the mode of the focused editor ('NORMAL',
 * 'INSERT', …) whenever it changes or another editor takes focus.
 */
export function vimExtension(onMode: (label: string) => void): Extension {
  defineExCommands()
  const reporter = ViewPlugin.define((view) => {
    const cm = getCM(view)
    const changed = (e: VimModeEvent) => {
      if (view.hasFocus) onMode(vimModeLabel(e))
    }
    cm?.on('vim-mode-change', changed)
    return { destroy: () => cm?.off('vim-mode-change', changed) }
  })
  const focus = EditorView.domEventHandlers({
    focus: (_e, view) => {
      onMode(vimStateLabel(getCM(view)?.state.vim))
      return false
    },
  })
  // Marks the editor so the global dispatcher leaves Vim its ⌃ keys.
  const marker = EditorView.editorAttributes.of({ class: VIM_EDITOR.slice(1) })
  return [vim(), reporter, focus, marker]
}
