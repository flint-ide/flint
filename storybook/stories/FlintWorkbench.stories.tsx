import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Badge, CommandPalette, EditorTabs, type EditorTab, Keys, type PaletteCommand, Spinner, StatusBar, StatusItem, StatusSpacer } from '@flint-ide/ui'
import { AlertTriangle, Bell, FileCode2, GitBranch, Search, Sparkles, XCircle } from '@flint-ide/ui/icons'

/** Flint's workbench components from @flint-ide/ui (ui/src): the app's chrome, reusable in extension webviews. */
const meta = { title: 'Flint/Workbench' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const COMMANDS: readonly PaletteCommand[] = [
  { id: 'file.open', label: 'Go to File…', detail: 'File', keys: ['⌘', 'P'] },
  { id: 'git.commit', label: 'Git: Commit', detail: 'Git', keys: ['⌘', '↵'] },
  { id: 'git.push', label: 'Git: Push', detail: 'Git' },
  { id: 'agent.ask', label: 'Agent: Ask about selection', detail: 'Agent', keys: ['⌘', 'L'] },
  { id: 'view.terminal', label: 'View: Toggle Terminal', detail: 'View', keys: ['⌃', '`'] },
  { id: 'settings.open', label: 'Preferences: Open Settings', detail: 'Preferences', keys: ['⌘', ','] },
]

export const CommandPaletteStory: Story = {
  name: 'Command palette',
  render: function Render() {
    const [ran, setRan] = useState('')
    return (
      <div className="sb-stack">
        <CommandPalette commands={COMMANDS} onRun={(c) => setRan(c.label)} />
        <span className="sb-label">{ran ? `Ran: ${ran}` : 'Type, use ↑ ↓ and ↵'}</span>
      </div>
    )
  },
}

export const EditorTabsStory: Story = {
  name: 'Editor tabs',
  render: function Render() {
    const [tabs, setTabs] = useState<readonly EditorTab[]>(
      ['main.go', 'App.tsx', 'style.css', 'DEVELOPMENT.md'].map((title, i) => ({ id: title, title, icon: <FileCode2 />, dirty: i % 2 === 1 })),
    )
    const [active, setActive] = useState('App.tsx')
    return <EditorTabs tabs={tabs} active={active} onSelect={setActive} onClose={(id) => setTabs((all) => all.filter((t) => t.id !== id))} />
  },
}

export const StatusBarStory: Story = {
  name: 'Status bar',
  parameters: { layout: 'fullscreen' },
  render: () => (
    <div style={{ paddingTop: 80 }}>
      <StatusBar>
        <StatusItem tip="Switch branch">
          <GitBranch /> feat/ui-kit
        </StatusItem>
        <StatusItem tip="2 errors, 1 warning">
          <XCircle color="var(--danger)" /> 2 <AlertTriangle color="var(--warn)" /> 1
        </StatusItem>
        <StatusItem tip="gopls is indexing">
          <Spinner /> gopls
        </StatusItem>
        <StatusSpacer />
        <StatusItem tip="Agent is idle">
          <Sparkles color="var(--accent)" /> Agent
        </StatusItem>
        <StatusItem tip="Go to line">Ln 42, Col 7</StatusItem>
        <StatusItem tip="Notifications">
          <Bell />
          <Badge color="orange" shape="pill">
            3
          </Badge>
        </StatusItem>
      </StatusBar>
    </div>
  ),
}

export const KeyboardShortcuts: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 360, padding: 12, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-raised)' }}>
      {COMMANDS.filter((c) => c.keys).map((c) => (
        <div key={c.id} className="sb-row" style={{ justifyContent: 'space-between' }}>
          <span>{c.label}</span>
          <Keys keys={c.keys ?? []} />
        </div>
      ))}
      <div className="sb-row" style={{ color: 'var(--fg-muted)' }}>
        <Search size={14} /> All shortcuts: <Keys keys={['⌘', 'K', '⌘', 'S']} />
      </div>
    </div>
  ),
}
