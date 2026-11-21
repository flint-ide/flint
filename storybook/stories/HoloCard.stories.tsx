import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge, HoloCard } from '@wunderui/react'
import { Sparkles } from 'lucide-react'

const meta = {
  title: 'Data Display/Holo Card',
  component: HoloCard,
  args: {
    children: (
      <div className="sb-stack" style={{ gap: 8 }}>
        <div className="sb-row">
          <Sparkles style={{ width: 18, height: 18 }} />
          <strong>Flint Pro</strong>
          <Badge color="purple" badgeStyle="light" shape="pill">
            beta
          </Badge>
        </div>
        <div style={{ fontSize: 13, color: 'var(--fg-muted)' }}>
          Unlimited agent sessions, shared review drafts and remote Kubernetes contexts.
        </div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--fg-muted)' }}>license · FLNT-7Q2K-93XD</div>
      </div>
    ),
  },
  argTypes: { children: { control: false } },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 360 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof HoloCard>

export default meta
type Story = StoryObj<typeof meta>

/** Move the pointer over the card: the holographic sheen follows it. */
export const Playground: Story = {}

export const Achievements: Story = {
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 'none' }}>
        <Story />
      </div>
    ),
  ],
  render: () => (
    <div className="sb-grid">
      {[
        { title: '1,000 green builds', body: 'main stayed green for 1,000 CI runs in a row.' },
        { title: 'First merged agent PR', body: 'Claude Code opened #61 and it landed without changes.' },
        { title: 'Zero flaky tests', body: 'No quarantined tests in internal/ for 30 days.' },
      ].map(({ title, body }) => (
        <HoloCard key={title}>
          <div style={{ fontWeight: 600, marginBottom: 6 }}>{title}</div>
          <div style={{ fontSize: 13, color: 'var(--fg-muted)' }}>{body}</div>
        </HoloCard>
      ))}
    </div>
  ),
}
