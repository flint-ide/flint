import type { Meta, StoryObj } from '@storybook/react-vite'
import { TextShimmer } from '@flint-ide/ui'

/** TextShimmer: a label with a soft highlight sweeping across it while an agent works (ui/src/TextShimmer.tsx). Reduced motion shows plain muted text. */
const meta = {
  title: 'Flint/Text Shimmer',
  component: TextShimmer,
  args: { children: 'Thinking…', active: true },
} satisfies Meta<typeof TextShimmer>

export default meta
type Story = StoryObj<typeof meta>

export const Active: Story = {}

export const Inactive: Story = {
  args: { active: false },
}

/** In a row of agent status, the way the chat shows a working turn. */
export const InLine: Story = {
  render: (args) => (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
      <span aria-hidden>●</span>
      <TextShimmer {...args} />
      <span style={{ color: 'var(--fg-muted)' }}>12s</span>
    </div>
  ),
}

export const AsDiv: Story = {
  args: { as: 'div', children: 'Reading 14 files…' },
}
