import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { commandTitle, groupByKind, importedKeymap, keybindingNote, type ImportItem } from './vscodeImport'
import type { Command } from './commands'

const run = () => undefined
const commands: Command[] = [
  { id: 'nav.goToFile', title: 'Go to File…', keys: 'Mod+P', run },
  { id: 'project.recent', title: 'Recent Projects…', keys: 'Mod+O', run },
  { id: 'terminal.toggle', title: 'Toggle Terminal', keys: 'Mod+J', run },
]

describe('import preview', () => {
  it('groups entries by kind in a fixed order', () => {
    const groups = groupByKind([
      { kind: 'keybinding', n: 1 },
      { kind: 'setting', n: 2 },
      { kind: 'weird', n: 3 },
      { kind: 'setting', n: 4 },
    ])
    assert.deepEqual(groups.map((g) => [g.title, g.entries.map((e) => e.n)]), [
      ['Settings', [2, 4]],
      ['Keyboard shortcuts', [1]],
      ['Other', [3]],
    ])
  })

  it('names commands by title', () => {
    assert.equal(commandTitle('nav.goToFile', commands), 'Go to File…')
    assert.equal(commandTitle('gone.command', commands), 'gone.command')
  })

  it('lays imported shortcuts over the current keymap', () => {
    const items: ImportItem[] = [
      { kind: 'keybinding', name: 'workbench.action.quickOpen', value: 'Mod+O', target: 'nav.goToFile' },
      { kind: 'setting', name: 'editor.fontSize', value: '14 px', target: 'Editor ▸ Font size' },
    ]
    assert.deepEqual(importedKeymap(items, { 'git.push': '' }), { 'git.push': '', 'nav.goToFile': 'Mod+O' })
  })

  it('warns about commands that would share the shortcut', () => {
    const item: ImportItem = { kind: 'keybinding', name: 'workbench.action.quickOpen', value: 'Mod+O', target: 'nav.goToFile' }
    assert.equal(keybindingNote(item, commands, importedKeymap([item], {})), '⌘O — also Recent Projects…')
    const free: ImportItem = { ...item, value: 'Mod+E' }
    assert.equal(keybindingNote(free, commands, importedKeymap([free], {})), '⌘E')
    // A command moved away by the import no longer clashes.
    const both = [item, { kind: 'keybinding', name: 'workbench.action.openRecent', value: 'Mod+R', target: 'project.recent' }]
    assert.equal(keybindingNote(item, commands, importedKeymap(both, {})), '⌘O')
  })
})
