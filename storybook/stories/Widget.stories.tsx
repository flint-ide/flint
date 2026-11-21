import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, IconButton, Meter, MeterLabel, MeterValue, TrendChip, Widget } from '@wunderui/react'
import { MoreHorizontal, RefreshCw } from 'lucide-react'

const bigNumber = { fontSize: 28, fontWeight: 700, lineHeight: 1.1 } as const
const muted = { fontSize: 12, color: 'var(--fg-muted)' } as const

const meta = {
  title: 'Data Display/Widget',
  component: Widget,
  args: {
    title: 'Tests passed',
    description: 'main · last 24 hours',
    children: (
      <div className="sb-row">
        <span style={bigNumber}>1,284</span>
        <TrendChip value="3%" direction="up" />
      </div>
    ),
  },
  argTypes: {
    title: { control: 'text' },
    description: { control: 'text' },
    children: { control: false },
    action: { control: false },
    footer: { control: false },
  },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 340 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Widget>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Dashboard: Story = {
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 'none' }}>
        <Story />
      </div>
    ),
  ],
  render: () => (
    <div className="sb-grid">
      <Widget
        title="Tests passed"
        description="main · last 24 hours"
        action={
          <IconButton variant="ghost" aria-label="Refresh">
            <RefreshCw />
          </IconButton>
        }
        footer={<span style={muted}>3 flaky tests quarantined</span>}
      >
        <div className="sb-row">
          <span style={bigNumber}>1,284</span>
          <TrendChip value="3%" direction="up" />
        </div>
      </Widget>
      <Widget
        title="Build time"
        description="p50 of go build + vite build"
        footer={<span style={muted}>Compared with last week</span>}
      >
        <div className="sb-row">
          <span style={bigNumber}>2m 41s</span>
          <TrendChip value="12%" direction="down" sentiment="positive" />
        </div>
      </Widget>
      <Widget
        title="Failed runs"
        description="All workflows · 7 days"
        action={
          <IconButton variant="ghost" aria-label="More">
            <MoreHorizontal />
          </IconButton>
        }
      >
        <div className="sb-row">
          <span style={bigNumber}>9</span>
          <TrendChip value="4" direction="up" sentiment="negative" />
        </div>
      </Widget>
      <Widget
        title="Agent tokens"
        description="Claude Code + Codex · this month"
        footer={
          <Button size="sm" variant="plain">
            View usage
          </Button>
        }
      >
        <Meter value={6.2} max={10}>
          <MeterLabel>6.2M of 10M</MeterLabel>
          <MeterValue>{(_, value) => `${Math.round((value / 10) * 100)}%`}</MeterValue>
        </Meter>
      </Widget>
    </div>
  ),
}
