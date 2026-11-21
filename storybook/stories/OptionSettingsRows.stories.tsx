import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Bot, Cloud, Cpu, GitBranch, Keyboard, Puzzle } from 'lucide-react'
import { Badge, Button, OptionRow, OptionRowGroup, SettingsRow } from '@wunderui/react'

const meta = {
  title: 'Forms/Option & Settings Rows',
  component: OptionRow,
  args: {
    title: 'Claude Sonnet',
    description: 'Balanced speed and quality for everyday edits.',
    variant: 'radio',
    selected: true,
    disabled: false,
    badge: 'Recommended',
    badgeColor: 'indigo',
  },
  argTypes: {
    variant: { control: 'inline-radio', options: ['radio', 'checkbox', 'link', 'icon'] },
    selected: { control: 'boolean' },
    disabled: { control: 'boolean' },
    badge: { control: 'text' },
    badgeColor: { control: 'select', options: ['blue', 'green', 'red', 'yellow', 'purple', 'indigo', 'pink', 'orange', 'grey'] },
  },
} satisfies Meta<typeof OptionRow>

export default meta
type Story = StoryObj<typeof meta>

const panel = {
  maxWidth: 460,
  padding: 16,
  border: '1px solid var(--border)',
  borderRadius: 10,
  background: 'var(--bg-raised)',
} as const

export const Playground: Story = {
  render: function Render(args) {
    const [selected, setSelected] = useState(Boolean(args.selected))
    const isRadio = args.variant === 'radio' || args.variant === 'icon'
    const row = (
      <OptionRow
        {...args}
        value="sonnet"
        icon={args.variant === 'icon' ? <Bot size={18} /> : undefined}
        selected={selected}
        onSelect={() => setSelected((s) => !s)}
      />
    )
    return (
      <div style={{ maxWidth: 460 }}>
        {isRadio ? (
          <OptionRowGroup aria-label="Model" value={selected ? 'sonnet' : ''} onValueChange={() => setSelected(true)}>
            {row}
          </OptionRowGroup>
        ) : (
          row
        )}
      </div>
    )
  },
}

const MODELS = [
  { value: 'opus', title: 'Claude Opus', description: 'Deepest reasoning, slower and pricier.', icon: <Cpu size={18} /> },
  { value: 'sonnet', title: 'Claude Sonnet', description: 'Balanced speed and quality.', icon: <Bot size={18} />, badge: 'Recommended' },
  { value: 'haiku', title: 'Claude Haiku', description: 'Fast completions and quick fixes.', icon: <Cloud size={18} /> },
  { value: 'local', title: 'Local model', description: 'Requires Ollama on this machine.', icon: <Cpu size={18} />, disabled: true },
]

export const RadioGroup: Story = {
  name: 'OptionRowGroup (radio)',
  render: function Render() {
    const [model, setModel] = useState('sonnet')
    return (
      <div style={{ maxWidth: 460 }}>
        <OptionRowGroup aria-label="Agent model" value={model} onValueChange={setModel}>
          {MODELS.map((m) => (
            <OptionRow key={m.value} value={m.value} title={m.title} description={m.description} badge={m.badge} disabled={m.disabled} selected={model === m.value} />
          ))}
        </OptionRowGroup>
      </div>
    )
  },
}

export const IconVariant: Story = {
  name: 'Icon variant',
  render: function Render() {
    const [model, setModel] = useState('opus')
    return (
      <div style={{ maxWidth: 460 }}>
        <OptionRowGroup aria-label="Agent model" value={model} onValueChange={setModel}>
          {MODELS.map((m) => (
            <OptionRow key={m.value} variant="icon" icon={m.icon} value={m.value} title={m.title} description={m.description} badge={m.badge} badgeColor="green" disabled={m.disabled} selected={model === m.value} />
          ))}
        </OptionRowGroup>
      </div>
    )
  },
}

export const WithError: Story = {
  name: 'Group with error',
  render: () => (
    <div style={{ maxWidth: 460 }}>
      <OptionRowGroup aria-label="Merge strategy" error="Pick how pull requests are merged before saving." required>
        <OptionRow value="merge" title="Merge commit" description="Keeps every commit from the branch." />
        <OptionRow value="squash" title="Squash and merge" description="One commit per pull request." />
        <OptionRow value="rebase" title="Rebase and merge" description="Linear history, no merge commits." />
      </OptionRowGroup>
    </div>
  ),
}

export const CheckboxAndLink: Story = {
  name: 'Checkbox & link variants',
  render: function Render() {
    const [on, setOn] = useState<Record<string, boolean>>({ lint: true, test: false })
    const toggle = (k: string) => () => setOn((prev) => ({ ...prev, [k]: !prev[k] }))
    return (
      <div className="sb-stack" style={{ maxWidth: 460, gap: 8 }}>
        <OptionRow variant="checkbox" value="lint" title="Run linter before commit" selected={on.lint} onSelect={toggle('lint')} />
        <OptionRow variant="checkbox" value="test" title="Run tests before push" description="Blocks the push when tests fail." selected={on.test} onSelect={toggle('test')} />
        <OptionRow variant="link" title="Keyboard shortcuts" description="Customize keybindings" href="#keybindings" />
        <OptionRow variant="link" title="Extensions" description="12 installed, 2 updates" onSelect={() => undefined} />
      </div>
    )
  },
}

export const Settings: Story = {
  name: 'SettingsRow',
  render: function Render() {
    const [formatOnSave, setFormatOnSave] = useState(true)
    const [autoFetch, setAutoFetch] = useState(false)
    const [telemetry, setTelemetry] = useState(false)
    return (
      <div className="sb-stack" style={{ ...panel, gap: 4 }}>
        <SettingsRow title="Format on save" description="Run the configured formatter whenever a file is saved." checked={formatOnSave} onCheckedChange={setFormatOnSave} />
        <SettingsRow title="Auto-fetch" description="Fetch from all remotes every 3 minutes." checked={autoFetch} onCheckedChange={setAutoFetch} />
        <SettingsRow title="Share usage data" description="Anonymous crash reports and feature usage." checked={telemetry} onCheckedChange={setTelemetry} />
        <SettingsRow
          title="GitHub account"
          description="Signed in as octo-dev"
          control={
            <Button variant="outline" size="sm">
              Sign out
            </Button>
          }
        />
        <SettingsRow title="Default branch" description="Used for new repositories." control={<Badge>main</Badge>} />
      </div>
    )
  },
}

export const SettingsWithIcons: Story = {
  name: 'SettingsRow with rich titles',
  render: () => (
    <div className="sb-stack" style={{ ...panel, gap: 4 }}>
      <SettingsRow
        title={
          <span className="sb-row" style={{ gap: 6 }}>
            <GitBranch size={14} /> Branch protection
          </span>
        }
        description="Prevent force-pushes to main."
        checked
      />
      <SettingsRow
        title={
          <span className="sb-row" style={{ gap: 6 }}>
            <Puzzle size={14} /> Extension auto-update
          </span>
        }
        description="Install updates in the background."
        checked={false}
      />
      <SettingsRow
        title={
          <span className="sb-row" style={{ gap: 6 }}>
            <Keyboard size={14} /> Vim mode
          </span>
        }
        control={
          <Button variant="ghost" size="sm">
            Configure
          </Button>
        }
      />
    </div>
  ),
}
