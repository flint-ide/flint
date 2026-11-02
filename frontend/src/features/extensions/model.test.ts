import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { contributionKey, extensionParts, EXTENSION_ORDER, themeContributions, type Deps, type ExtensionInfo } from './model'
import type { Command } from '../../lib/commands'

const info: ExtensionInfo = {
  id: 'acme.jira',
  name: 'Jira',
  version: '1.0.0',
  enabled: true,
  state: 'stopped',
  commands: [
    { id: 'acme.jira.refresh', title: 'Refresh Issues', keys: 'Mod+Alt+J' },
    { id: 'acme.jira.open', title: 'Open Issue', category: 'Issues', keys: 'Mod+P' },
  ],
  panels: [{ id: 'acme.jira.issues', title: 'Issues', icon: 'nope', order: 20, view: 'tree' }],
  statusItems: [{ id: 'acme.jira.count', order: 5 }],
  settings: { title: 'Jira', properties: [{ key: 'site', type: 'string', default: '' }] },
}

function deps(existing: readonly Command[] = []): Deps & { ran: string[]; shown: string[] } {
  const ran: string[] = []
  const shown: string[] = []
  return {
    ran,
    shown,
    existing,
    panel: () => () => null,
    webviewPanel: () => () => null,
    webviewTab: () => () => null,
    statusItem: () => () => null,
    settingsPage: () => () => null,
    run: async (ext, command) => void ran.push(`${ext} ${command}`),
    show: (panel) => void shown.push(panel),
  }
}

describe('extension contributions', () => {
  it('turns the manifest into commands, a panel, a status item and a Settings page', async () => {
    const d = deps()
    const parts = extensionParts(info, d)
    assert.deepEqual(
      parts.commands.map((c) => [c.id, c.title, c.category]),
      [
        ['acme.jira.refresh', 'Refresh Issues', 'Jira'],
        ['acme.jira.open', 'Open Issue', 'Issues'],
        ['view.acme.jira.issues', 'Show Issues', 'Jira'],
      ],
    )
    await parts.commands[0].run()
    parts.commands[2].run()
    assert.deepEqual(d.ran, ['acme.jira acme.jira.refresh'])
    assert.deepEqual(d.shown, ['acme.jira.issues'])
    const [panel] = parts.contributions.panels ?? []
    assert.equal(panel.icon, 'puzzle', 'an icon Flint lacks becomes the extension icon')
    assert.equal(panel.order, EXTENSION_ORDER + 20)
    assert.equal(panel.command, false)
    assert.equal(parts.contributions.statusItems?.[0].order, EXTENSION_ORDER + 5)
    assert.deepEqual(parts.contributions.settingsPages?.map((p) => [p.id, p.label]), [['extension:acme.jira', 'Jira']])
  })

  it('drops a default shortcut that another command already has', () => {
    const palette: Command = { id: 'palette', title: 'Go to File', keys: 'Mod+P', run: () => {} }
    const parts = extensionParts(info, deps([palette]))
    assert.equal(parts.commands[0].keys, 'Mod+Alt+J')
    assert.equal(parts.commands[1].keys, undefined)
    assert.equal(parts.conflicts.length, 1)
    assert.match(parts.conflicts[0], /Mod\+P is already Go to File/)
  })

  it('refuses a shortcut it cannot parse', () => {
    const odd = { ...info, commands: [{ id: 'acme.jira.x', title: 'X', keys: 'Hyper+K' }] }
    const parts = extensionParts(odd, deps())
    assert.equal(parts.commands[0].keys, undefined)
    assert.match(parts.conflicts[0], /not a shortcut/)
  })

  it('has no Settings page without settings', () => {
    assert.deepEqual(extensionParts({ ...info, settings: null }, deps()).contributions.settingsPages, [])
  })

  it('keys contributions by what they contain, not by status or state', () => {
    const key = contributionKey(info)
    assert.equal(contributionKey({ ...info, state: 'running' }), key)
    assert.notEqual(contributionKey({ ...info, version: '1.0.1' }), key)
    assert.equal(contributionKey({ ...info, enabled: false }), '')
    assert.equal(contributionKey({ ...info, state: 'invalid' }), '')
  })
})

describe('extension themes', () => {
  const withThemes: ExtensionInfo = {
    ...info,
    themes: [
      { id: 'acme.jira.dark', label: 'Jira Dark', base: 'dark', colors: { '--bg-editor': '#1d2125', '--evil': '#fff', '--fg': 'url(x)' }, terminal: { red: '#f87168', pink: '#f0f' } },
      { id: 'acme.jira.light', label: 'Jira Light', base: 'light', colors: { '--bg-editor': '#ffffff' }, terminal: null },
    ],
  }

  it('become theme contributions under the extension name, filtered like a custom theme', () => {
    const themes = themeContributions(withThemes)
    assert.deepEqual(themes.map((t) => [t.id, t.group, t.base]), [['acme.jira.dark', 'Jira', 'dark'], ['acme.jira.light', 'Jira', 'light']])
    assert.deepEqual(themes[0].colors, { '--bg-editor': '#1d2125' })
    assert.deepEqual(themes[0].terminal, { red: '#f87168' })
    assert.deepEqual(themes[1].terminal, {})
    assert.deepEqual(extensionParts(withThemes, deps()).contributions.themes, themes)
  })

  it('change the contribution key', () => {
    assert.notEqual(contributionKey(withThemes), contributionKey(info))
  })
})

describe('webview contributions', () => {
  it('draws webview panels with the frame and turns tab views into restorable tab kinds', () => {
    const marks: string[] = []
    const d = {
      ...deps(),
      webviewPanel: (_e: unknown, p: { id: string }) => (marks.push(`panel ${p.id}`), () => null),
      webviewTab: (_e: unknown, v: string) => (marks.push(`tab ${v}`), () => null),
    }
    const parts = extensionParts(
      {
        ...info,
        panels: [{ id: 'acme.jira.board', title: 'Board', icon: 'database', order: 1, view: 'webview' }],
        tabViews: [
          { id: 'acme.jira.issue', title: 'Issue', icon: 'database', view: 'webview', restore: true },
          { id: 'acme.jira.future', title: 'Later', view: 'canvas' },
        ],
      },
      d,
    )
    assert.deepEqual(marks, ['panel acme.jira.board', 'tab acme.jira.issue'])
    assert.deepEqual(
      parts.contributions.tabKinds?.map((k) => [k.id, k.icon, k.restore]),
      [['acme.jira.issue', 'database', true]],
    )
  })
})

