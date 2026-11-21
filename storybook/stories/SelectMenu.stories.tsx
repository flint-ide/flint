import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Field,
  FieldDescription,
  FieldLabel,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@wunderui/react'

const MODELS = [
  { value: 'claude-opus', label: 'Claude Opus' },
  { value: 'claude-sonnet', label: 'Claude Sonnet' },
  { value: 'claude-haiku', label: 'Claude Haiku' },
  { value: 'gpt', label: 'GPT (via Codex)' },
  { value: 'local', label: 'Local model (Ollama)' },
]

const BRANCHES = [
  { value: 'main', label: 'main' },
  { value: 'develop', label: 'develop' },
  { value: 'feat/ui-kit', label: 'feat/ui-kit' },
  { value: 'release/1.4', label: 'release/1.4' },
]

const INDENT = [
  { value: 'tabs', label: 'tabs' },
  { value: '2', label: '2 spaces' },
  { value: '4', label: '4 spaces' },
]

type Option = { value: string; label: string }

function ModelSelect({ options, ...props }: { options: Option[] } & React.ComponentProps<typeof Select>) {
  return (
    <Select items={options} {...props}>
      <SelectTrigger aria-label="Agent model">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

const meta = {
  title: 'Forms/Select Menu',
  component: Select,
  args: { disabled: false, defaultValue: 'claude-sonnet' },
  argTypes: {
    disabled: { control: 'boolean' },
    defaultValue: { control: 'select', options: MODELS.map((m) => m.value) },
  },
  decorators: [
    (Story) => (
      <div style={{ minHeight: 340 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Select>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {
  render: (args) => (
    <div style={{ maxWidth: 280 }}>
      <ModelSelect key={String(args.defaultValue)} {...args} options={MODELS} />
    </div>
  ),
}

export const WithField: Story = {
  name: 'In a field',
  render: function Render() {
    const [model, setModel] = useState<string | null>('claude-opus')
    return (
      <div className="sb-stack" style={{ maxWidth: 320 }}>
        <Field>
          <FieldLabel>Agent model</FieldLabel>
          <Select items={MODELS} value={model} onValueChange={(v) => setModel(v)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {MODELS.map((m) => (
                <SelectItem key={m.value} value={m.value}>
                  {m.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FieldDescription>Used for new agent sessions in this workspace.</FieldDescription>
        </Field>
        <span className="sb-label" style={{ width: 'auto' }}>
          value: {model ?? 'null'}
        </span>
      </div>
    )
  },
}

export const Inline: Story = {
  name: 'Inline variant',
  render: function Render() {
    const [branch, setBranch] = useState<string | null>('main')
    const [indent, setIndent] = useState<string | null>('2')
    return (
      <div className="sb-stack" style={{ color: 'var(--fg)', fontSize: 13 }}>
        <div className="sb-row">
          Compare
          <code style={{ fontFamily: 'var(--font-mono)' }}>feat/ui-kit</code>
          against
          <Select items={BRANCHES} value={branch} onValueChange={(v) => setBranch(v)}>
            <SelectTrigger variant="inline" aria-label="Base branch">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {BRANCHES.map((b) => (
                <SelectItem key={b.value} value={b.value}>
                  {b.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="sb-row">
          Indent using
          <Select items={INDENT} value={indent} onValueChange={(v) => setIndent(v)}>
            <SelectTrigger variant="inline" aria-label="Indentation">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {INDENT.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
    )
  },
}

export const States: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <div className="sb-row">
        <span className="sb-label">placeholder</span>
        <div style={{ flex: 1 }}>
          <Select items={MODELS}>
            <SelectTrigger aria-label="Agent model">
              <SelectValue placeholder="Choose a model" />
            </SelectTrigger>
            <SelectContent>
              {MODELS.map((m) => (
                <SelectItem key={m.value} value={m.value}>
                  {m.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="sb-row">
        <span className="sb-label">selected</span>
        <div style={{ flex: 1 }}>
          <ModelSelect options={MODELS} defaultValue="claude-haiku" />
        </div>
      </div>
      <div className="sb-row">
        <span className="sb-label">disabled</span>
        <div style={{ flex: 1 }}>
          <ModelSelect options={MODELS} defaultValue="local" disabled />
        </div>
      </div>
    </div>
  ),
}

export const OpenByDefault: Story = {
  name: 'Open',
  render: () => (
    <div style={{ maxWidth: 280 }}>
      <ModelSelect options={MODELS} defaultValue="claude-sonnet" defaultOpen />
    </div>
  ),
}
