import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { KeyRound, Search, Globe } from 'lucide-react'
import {
  PasswordField,
  PasswordRequirement,
  PasswordStrength,
  TextField,
  type PasswordStrengthLevel,
} from '@wunderui/react'

const meta = {
  title: 'Forms/Text & Password Fields',
  component: TextField,
  args: {
    label: 'Workspace name',
    placeholder: 'flint-ide',
    helper: 'Shown in the title bar and in invite emails.',
    error: '',
    optional: false,
    disabled: false,
    size: 'default',
  },
  argTypes: {
    label: { control: 'text' },
    placeholder: { control: 'text' },
    helper: { control: 'text' },
    error: { control: 'text' },
    optional: { control: 'boolean' },
    disabled: { control: 'boolean' },
    size: { control: 'inline-radio', options: ['sm', 'default', 'lg'] },
  },
} satisfies Meta<typeof TextField>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 360 }}>
      <TextField {...args} error={args.error || undefined} />
    </div>
  ),
}

export const TextFieldStates: Story = {
  name: 'Text field states',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 360, gap: 20 }}>
      <TextField label="Repository URL" placeholder="https://github.com/org/repo.git" leading={<Globe size={14} />} />
      <TextField label="Search settings" placeholder="format on save" leading={<Search size={14} />} size="sm" />
      <TextField label="Commit author" defaultValue="Flint Bot" optional helper="Overrides git config user.name." />
      <TextField
        label="Branch name"
        defaultValue="feat/ ui kit"
        error="Branch names cannot contain spaces."
      />
      <TextField label="Remote" defaultValue="origin" disabled />
    </div>
  ),
}

export const Password: Story = {
  name: 'Password field',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 360, gap: 20 }}>
      <PasswordField
        type="password"
        revealable
        label="GitHub token"
        placeholder="ghp_..."
        leading={<KeyRound size={14} />}
        helper="Stored in the system keychain, never in settings.json."
        labelTrailing={<a href="#create-token">Create a token</a>}
      />
      <PasswordField
        type="password"
        revealable
        label="Anthropic API key"
        defaultValue="sk-ant-expired"
        error="This key was revoked. Paste a new one."
      />
    </div>
  ),
}

const LEVELS: PasswordStrengthLevel[] = ['weak', 'fair', 'good', 'strong']

export const StrengthLevels: Story = {
  name: 'Password strength levels',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 320, gap: 16 }}>
      {LEVELS.map((level) => (
        <PasswordStrength key={level} strength={level} />
      ))}
      <PasswordStrength strength="good" label="Good enough for a vault passphrase" />
    </div>
  ),
}

export const Requirements: Story = {
  name: 'Password requirements',
  render: () => (
    <div className="sb-stack" style={{ gap: 6 }}>
      <PasswordRequirement met>At least 12 characters</PasswordRequirement>
      <PasswordRequirement met>One uppercase letter</PasswordRequirement>
      <PasswordRequirement met={false}>One number</PasswordRequirement>
      <PasswordRequirement met={false}>One symbol</PasswordRequirement>
    </div>
  ),
}

const RULES = [
  { label: 'At least 12 characters', test: (v: string) => v.length >= 12 },
  { label: 'Upper and lower case letters', test: (v: string) => /[a-z]/.test(v) && /[A-Z]/.test(v) },
  { label: 'At least one number', test: (v: string) => /\d/.test(v) },
  { label: 'At least one symbol', test: (v: string) => /[^A-Za-z0-9]/.test(v) },
]

function strengthOf(met: number): PasswordStrengthLevel {
  return LEVELS[Math.max(0, met - 1)]
}

export const NewVaultPassphrase: Story = {
  name: 'Composed: vault passphrase',
  render: function Render() {
    const [value, setValue] = useState('Flint')
    const met = RULES.filter((r) => r.test(value)).length
    return (
      <div
        className="sb-stack"
        style={{ maxWidth: 360, padding: 16, border: '1px solid var(--border)', borderRadius: 10, background: 'var(--bg-raised)' }}
      >
        <PasswordField
          type="password"
          revealable
          label="Secrets vault passphrase"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <PasswordStrength strength={strengthOf(met)} />
        <div className="sb-stack" style={{ gap: 6 }}>
          {RULES.map((r) => (
            <PasswordRequirement key={r.label} met={r.test(value)}>
              {r.label}
            </PasswordRequirement>
          ))}
        </div>
      </div>
    )
  },
}
