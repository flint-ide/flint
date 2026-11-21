import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { InputGroupCopy } from '@wunderui/react'

const meta = {
  title: 'Forms/Copy Field',
  component: InputGroupCopy,
  args: {
    value: 'https://flint.dev/invite/ws_8f3k2m9q',
    copyLabel: 'Copy',
    copiedLabel: 'Copied',
    'aria-label': 'Workspace invite link',
  },
  argTypes: {
    value: { control: 'text' },
    copyLabel: { control: 'text' },
    copiedLabel: { control: 'text' },
  },
} satisfies Meta<typeof InputGroupCopy>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {
  render: (args) => (
    <div style={{ maxWidth: 420 }}>
      <InputGroupCopy {...args} />
    </div>
  ),
}

export const Examples: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 460, gap: 16 }}>
      <div className="sb-stack" style={{ gap: 6 }}>
        <span className="sb-label" style={{ width: 'auto' }}>
          Invite link
        </span>
        <InputGroupCopy value="https://flint.dev/invite/ws_8f3k2m9q" copyLabel="Copy link" aria-label="Invite link" />
      </div>
      <div className="sb-stack" style={{ gap: 6 }}>
        <span className="sb-label" style={{ width: 'auto' }}>
          Clone URL
        </span>
        <InputGroupCopy value="git@github.com:flint-ide/flint.git" aria-label="SSH clone URL" />
      </div>
      <div className="sb-stack" style={{ gap: 6 }}>
        <span className="sb-label" style={{ width: 'auto' }}>
          Agent webhook secret
        </span>
        <InputGroupCopy value="whsec_3b9e1c7a0f4d42" copyLabel="Copy secret" copiedLabel="Secret copied" aria-label="Webhook secret" />
      </div>
    </div>
  ),
}

export const WithCallback: Story = {
  name: 'onCopy callback',
  render: function Render() {
    const [count, setCount] = useState(0)
    return (
      <div className="sb-stack" style={{ maxWidth: 420 }}>
        <InputGroupCopy value="FLINT-2026-TRIAL-7Q4X" aria-label="License key" onCopy={() => setCount((c) => c + 1)} />
        <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
          Copied {count} time{count === 1 ? '' : 's'}. (Copy only succeeds where the clipboard API is allowed.)
        </span>
      </div>
    )
  },
}
