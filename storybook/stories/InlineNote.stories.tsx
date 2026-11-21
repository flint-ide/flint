import type { Meta, StoryObj } from '@storybook/react-vite'
import { Alert, InlineNote } from '@wunderui/react'
import { Info, KeyRound, Lightbulb } from 'lucide-react'

/**
 * A one-line hint under a field or inside a panel. Deprecated upstream in favour of
 * `<Alert size="inline">`, which renders the same thing; both are shown here.
 */
const meta = {
  title: 'Feedback/Inline Note',
  component: InlineNote,
  args: {
    tone: 'neutral',
    children: 'Agents only see files inside this workspace.',
  },
  argTypes: {
    tone: { control: 'inline-radio', options: ['neutral', 'warning'] },
    children: { control: 'text' },
    icon: { control: false },
  },
  decorators: [(Story) => <div style={{ maxWidth: 460 }}>{Story()}</div>],
} satisfies Meta<typeof InlineNote>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Warning: Story = {
  args: { tone: 'warning', children: 'Force-pushing rewrites history for everyone on feat/ui-kit.' },
}

/** Custom icons replace the default mark. */
export const CustomIcon: Story = {
  name: 'Custom icon',
  render: () => (
    <div className="sb-stack">
      <InlineNote icon={<Lightbulb />}>Press ⌘K to run any command.</InlineNote>
      <InlineNote icon={<Info />}>Indexing pauses while on battery.</InlineNote>
      <InlineNote tone="warning" icon={<KeyRound />}>
        GITHUB_TOKEN expires in 3 days.
      </InlineNote>
    </div>
  ),
}

/** Under a form field, the most common place for a note. */
export const UnderAField: Story = {
  name: 'Under a field',
  render: () => (
    <div className="sb-stack" style={{ gap: 6 }}>
      <label style={{ fontSize: 13, fontWeight: 500 }} htmlFor="remote">
        Remote URL
      </label>
      <input
        id="remote"
        defaultValue="git@github.com:flint-ide/flint.git"
        style={{
          font: '13px var(--font-mono)',
          padding: '6px 8px',
          borderRadius: 6,
          border: '1px solid var(--border)',
          background: 'var(--bg-raised)',
          color: 'inherit',
        }}
      />
      <InlineNote>SSH remotes use the key from ~/.ssh/config.</InlineNote>
    </div>
  ),
}

/** The replacement: `Alert size="inline"` with `variant="plain"` / `"warning"`. */
export const AlertInlineEquivalent: Story = {
  name: 'Alert size="inline" (replacement)',
  render: () => (
    <div className="sb-stack">
      <Alert size="inline" variant="plain">
        Agents only see files inside this workspace.
      </Alert>
      <Alert size="inline" variant="warning">
        Force-pushing rewrites history for everyone on feat/ui-kit.
      </Alert>
    </div>
  ),
}
