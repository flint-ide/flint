import type { Meta, StoryObj } from '@storybook/react-vite'
import { Meter, MeterLabel, MeterValue } from '@wunderui/react'

const meta = {
  title: 'Data Display/Meter',
  component: Meter,
  args: { value: 72, min: 0, max: 100 },
  argTypes: {
    value: { control: { type: 'range', min: 0, max: 100, step: 1 } },
    min: { control: 'number' },
    max: { control: 'number' },
  },
  render: (args) => (
    <div style={{ maxWidth: 420 }}>
      <Meter {...args}>
        <MeterLabel>Disk usage</MeterLabel>
        <MeterValue />
      </Meter>
    </div>
  ),
} satisfies Meta<typeof Meter>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Bare: Story = {
  render: (args) => (
    <div style={{ maxWidth: 420 }}>
      <Meter {...args} aria-label="Disk usage" />
    </div>
  ),
}

export const CustomValue: Story = {
  name: 'Custom value text',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <Meter value={148_000} max={200_000} format={{ notation: 'compact' }}>
        <MeterLabel>Context window (Claude Code)</MeterLabel>
        <MeterValue>{(formatted) => `${formatted} / 200K tokens`}</MeterValue>
      </Meter>
      <Meter value={38.4} max={64}>
        <MeterLabel>Volume /var/lib/postgresql</MeterLabel>
        <MeterValue>{(_, value) => `${value} of 64 GB`}</MeterValue>
      </Meter>
      <Meter value={0.91} max={1} format={{ style: 'percent' }}>
        <MeterLabel>Test coverage — internal/review</MeterLabel>
        <MeterValue />
      </Meter>
    </div>
  ),
}

export const Levels: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      {[
        { pod: 'flint-api-7d9f8c6b5-x2kqz', cpu: 18 },
        { pod: 'flint-worker-5c4b7d9f-mp8rw', cpu: 57 },
        { pod: 'postgres-0', cpu: 93 },
      ].map(({ pod, cpu }) => (
        <Meter key={pod} value={cpu}>
          <MeterLabel style={{ fontFamily: 'var(--font-mono)' }}>{pod}</MeterLabel>
          <MeterValue>{(formatted) => `${formatted} CPU`}</MeterValue>
        </Meter>
      ))}
    </div>
  ),
}
