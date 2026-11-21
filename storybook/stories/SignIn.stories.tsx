import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { BackupCode, SocialSignInButtons, TextLinkRow, TextField, type SocialProvider } from '@wunderui/react'

const ALL: SocialProvider[] = ['github', 'google', 'microsoft', 'apple']

const meta = {
  title: 'Forms/Sign In',
  component: SocialSignInButtons,
  args: { providers: ['github', 'google'], layout: 'vertical', prefix: 'Continue with' },
  argTypes: {
    providers: { control: 'check', options: ALL },
    layout: { control: 'inline-radio', options: ['horizontal', 'vertical'] },
    prefix: { control: 'text' },
  },
} satisfies Meta<typeof SocialSignInButtons>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 380 }}>
      <SocialSignInButtons {...args} />
    </div>
  ),
}

export const Layouts: Story = {
  name: 'Social buttons layouts',
  render: function Render() {
    const [last, setLast] = useState<SocialProvider | null>(null)
    return (
      <div className="sb-stack" style={{ maxWidth: 420, gap: 20 }}>
        <SocialSignInButtons providers={ALL} layout="vertical" onSelect={setLast} />
        <SocialSignInButtons providers={['github', 'google', 'microsoft']} layout="horizontal" prefix="" onSelect={setLast} />
        <SocialSignInButtons providers={['github']} layout="vertical" prefix="Sign in with" onSelect={setLast} />
        <span className="sb-label" style={{ width: 'auto' }}>
          last selected: {last ?? 'none'}
        </span>
      </div>
    )
  },
}

const VARIANTS = ['inline', 'small', 'stacked', 'icon', 'back'] as const

export const TextLinkRows: Story = {
  name: 'TextLinkRow variants',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 380, gap: 18 }}>
      {VARIANTS.map((v) => (
        <div key={v} className="sb-row" style={{ flexWrap: 'nowrap' }}>
          <span className="sb-label">{v}</span>
          <TextLinkRow
            variant={v}
            text={v === 'back' ? 'Back to sign in' : v === 'icon' ? 'Trouble with your token?' : "Don't have a Flint account?"}
            link={v === 'back' ? undefined : v === 'icon' ? 'Read the docs' : 'Sign up'}
            href="#"
          />
        </div>
      ))}
    </div>
  ),
}

const CODES = ['8F2K-Q7LM', 'R4XD-91VN', 'C6TP-2HBW', 'M3JZ-8YEA', 'K9SU-5QFG', 'W2NB-7ARX']

export const BackupCodes: Story = {
  name: 'BackupCode',
  render: function Render() {
    const [copied, setCopied] = useState<string | null>(null)
    return (
      <div className="sb-stack" style={{ maxWidth: 360 }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
          {CODES.map((c, i) => (
            <BackupCode key={c} code={c} used={i < 2} onCopy={setCopied} />
          ))}
        </div>
        <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
          {copied ? `Copied ${copied}` : 'Two codes are already used. Each code works once.'}
        </span>
      </div>
    )
  },
}

export const SignInCard: Story = {
  name: 'Composed: sign in to Flint Cloud',
  render: () => (
    <div
      className="sb-stack"
      style={{
        maxWidth: 380,
        padding: 24,
        gap: 16,
        border: '1px solid var(--border)',
        borderRadius: 12,
        background: 'var(--bg-raised)',
      }}
    >
      <div className="sb-stack" style={{ gap: 4 }}>
        <strong style={{ fontSize: 16, color: 'var(--fg)' }}>Sign in to Flint Cloud</strong>
        <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>Sync settings, agents and extensions across machines.</span>
      </div>
      <SocialSignInButtons providers={['github', 'google']} layout="vertical" />
      <div style={{ height: 1, background: 'var(--border)' }} />
      <TextField label="Email" type="email" placeholder="you@company.dev" />
      <TextLinkRow variant="inline" text="Don't have an account?" link="Sign up" href="#" />
    </div>
  ),
}
