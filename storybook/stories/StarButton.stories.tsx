import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { StarButton } from '@wunderui/react'
import { FileCode, GitBranch } from 'lucide-react'

type Args = { defaultStarred: boolean; size: 'sm' | 'md' | 'lg'; label: string; tooltipSide: 'top' | 'bottom' | 'left' | 'right'; disabled: boolean }

/** A favourite toggle (ghost IconButton): outline star, filled yellow when starred, with a Star / Unstar tooltip. */
const meta = {
  title: 'Actions/Star Button',
  args: { defaultStarred: false, size: 'md', label: 'Star branch', tooltipSide: 'top', disabled: false },
  argTypes: {
    size: { control: 'inline-radio', options: ['sm', 'md', 'lg'] },
    tooltipSide: { control: 'inline-radio', options: ['top', 'bottom', 'left', 'right'] },
  },
  render: (args) => (
    <div style={{ padding: 32 }}>
      <StarButton key={String(args.defaultStarred)} {...args} />
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Starred: Story = { args: { defaultStarred: true } }

export const SizesAndStates: Story = {
  render: () => (
    <div className="sb-stack">
      {(['sm', 'md', 'lg'] as const).map((size) => (
        <div key={size} className="sb-row">
          <span className="sb-label">{size}</span>
          <StarButton size={size} label="Star file" />
          <StarButton size={size} label="Star file" defaultStarred />
          <StarButton size={size} label="Star file" disabled />
        </div>
      ))}
    </div>
  ),
}

/** Controlled: favourite branches and files in a list, with a count of starred items. */
export const InAList: Story = {
  render: function Render() {
    const items = [
      { name: 'main', icon: GitBranch },
      { name: 'feat/ui-kit', icon: GitBranch },
      { name: 'frontend/src/boot.ts', icon: FileCode },
      { name: 'internal/git/diff.go', icon: FileCode },
    ]
    const [starred, setStarred] = useState<string[]>(['main'])
    const toggle = (name: string, on: boolean) => setStarred((s) => (on ? [...s, name] : s.filter((x) => x !== name)))
    return (
      <div style={{ width: 320, border: '1px solid var(--border)', background: 'var(--bg-raised)', borderRadius: 8 }}>
        {items.map(({ name, icon: Icon }) => (
          <div key={name} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 8px 4px 12px', fontSize: 13 }}>
            <Icon size={14} style={{ color: 'var(--fg-muted)' }} />
            <span style={{ flex: 1 }}>{name}</span>
            <StarButton size="sm" label={`Star ${name}`} starred={starred.includes(name)} onStarredChange={(on) => toggle(name, on)} />
          </div>
        ))}
        <div style={{ padding: '6px 12px', fontSize: 12, color: 'var(--fg-muted)', borderTop: '1px solid var(--border)' }}>
          {starred.length} starred
        </div>
      </div>
    )
  },
}
