import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Button, OtpCodeInput, OtpFieldCode } from '@wunderui/react'

const meta = {
  title: 'Forms/OTP Code',
  component: OtpFieldCode,
  args: { length: 6, invalid: false, disabled: false, 'aria-label': 'Verification code' },
  argTypes: {
    length: { control: 'inline-radio', options: [4, 6] },
    invalid: { control: 'boolean' },
    disabled: { control: 'boolean' },
  },
} satisfies Meta<typeof OtpFieldCode>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => <OtpFieldCode key={args.length} {...args} />,
}

export const Lengths: Story = {
  render: () => (
    <div className="sb-stack" style={{ gap: 20 }}>
      <div className="sb-row" style={{ gap: 16 }}>
        <span className="sb-label">length 4</span>
        <OtpFieldCode length={4} aria-label="Device pairing code" />
      </div>
      <div className="sb-row" style={{ gap: 16 }}>
        <span className="sb-label">length 6</span>
        <OtpFieldCode length={6} aria-label="Two-factor code" />
      </div>
    </div>
  ),
}

export const States: Story = {
  render: () => (
    <div className="sb-stack" style={{ gap: 20 }}>
      <div className="sb-row" style={{ gap: 16 }}>
        <span className="sb-label">filled</span>
        <OtpFieldCode value="482913" aria-label="Filled code" />
      </div>
      <div className="sb-row" style={{ gap: 16 }}>
        <span className="sb-label">invalid</span>
        <OtpFieldCode value="111111" invalid aria-label="Invalid code" />
      </div>
      <div className="sb-row" style={{ gap: 16 }}>
        <span className="sb-label">disabled</span>
        <OtpFieldCode disabled aria-label="Disabled code" />
      </div>
    </div>
  ),
}

const EXPECTED = '246810'

export const TwoFactorSignIn: Story = {
  name: 'Composed: verify GitHub sign-in',
  render: function Render() {
    const [code, setCode] = useState('')
    const [status, setStatus] = useState<'idle' | 'ok' | 'bad'>('idle')
    return (
      <div
        className="sb-stack"
        style={{
          maxWidth: 420,
          padding: 20,
          gap: 16,
          border: '1px solid var(--border)',
          borderRadius: 12,
          background: 'var(--bg-raised)',
        }}
      >
        <div className="sb-stack" style={{ gap: 4 }}>
          <strong style={{ color: 'var(--fg)' }}>Enter the code from your authenticator</strong>
          <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>Hint: the code for this story is {EXPECTED}.</span>
        </div>
        <OtpFieldCode
          value={code}
          onValueChange={(v) => {
            setCode(v)
            setStatus('idle')
          }}
          onComplete={(v) => setStatus(v === EXPECTED ? 'ok' : 'bad')}
          invalid={status === 'bad'}
          aria-label="Authenticator code"
        />
        <span style={{ fontSize: 12, color: status === 'bad' ? 'var(--danger, #e5484d)' : 'var(--fg-muted)' }}>
          {status === 'ok' ? 'Verified. Flint is now connected to GitHub.' : status === 'bad' ? 'That code is not valid.' : 'Waiting for 6 digits…'}
        </span>
        <div>
          <Button variant="ghost" size="sm" onClick={() => setCode('')}>
            Clear
          </Button>
        </div>
      </div>
    )
  },
}

export const DeprecatedAlias: Story = {
  name: 'OtpCodeInput (deprecated alias)',
  render: () => <OtpCodeInput length={4} aria-label="Pairing code" />,
}
