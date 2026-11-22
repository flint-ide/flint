import type { Meta, StoryObj } from '@storybook/react-vite'
import { StatTile, StatTiles } from '@flint-ide/ui'

/** StatTile: a Card with a label, one figure, sub-text and optionally a trend or a meter (ui/src/StatTile.tsx). */
const meta = {
  title: 'Flint/Stat Tile',
  component: StatTile,
  args: { label: 'Today', value: '$4.21', sub: '1.2M tokens' },
} satisfies Meta<typeof StatTile>

export default meta
type Story = StoryObj<typeof meta>

const panel = { width: 360, padding: 12, background: 'var(--bg-sidebar)', border: '1px solid var(--border)', borderRadius: 8 } as const

export const Default: Story = {
  render: (args) => (
    <div style={{ width: 120 }}>
      <StatTile {...args} />
    </div>
  ),
}

/** The Usage panel's row of tiles. */
export const Row: Story = {
  render: () => (
    <div style={panel}>
      <StatTiles>
        <StatTile label="Today" value="$4.21" sub="1.2M tokens" />
        <StatTile label="7 days" value="~$28.90" sub="9.8M tokens · est." />
        <StatTile label="30 days" value="$112.40" sub="41M tokens" />
      </StatTiles>
    </div>
  ),
}

export const WithTrend: Story = {
  render: () => (
    <div style={panel}>
      <StatTiles>
        <StatTile label="Today" value="$4.21" sub="vs yesterday" trend={{ value: '12%', direction: 'up', sentiment: 'negative' }} />
        <StatTile label="7 days" value="$28.90" sub="vs last week" trend={{ value: '8%', direction: 'down', sentiment: 'positive' }} />
      </StatTiles>
    </div>
  ),
}

export const WithMeter: Story = {
  render: () => (
    <div style={panel}>
      <StatTiles>
        <StatTile label="Daily budget" value="$4.21" sub="of $10" meter={{ value: 42, label: 'Daily budget used' }} />
        <StatTile label="Monthly budget" value="$112" sub="of $150" meter={{ value: 112, max: 150, label: 'Monthly budget used' }} />
      </StatTiles>
    </div>
  ),
}

export const NarrowTruncates: Story = {
  render: () => (
    <div style={{ ...panel, width: 160 }}>
      <StatTile label="Spent this billing period" value="$1,234,567.89" sub="across every folder and agent" />
    </div>
  ),
}
