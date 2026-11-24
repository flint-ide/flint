import type { Meta, StoryObj } from '@storybook/react-vite'
import { Sparkline, StatTile, StatTiles } from '@flint-ide/ui'

/** Sparkline: a tiny inline trend line for a row or a tile (ui/src/Sparkline.tsx, geometry in ui/src/sparklineMath.ts). */
const meta = {
  title: 'Flint/Sparkline',
  component: Sparkline,
  args: { label: 'Tokens per day', values: [300, 800, 1900, 700, 1000, 900, 1200] },
} satisfies Meta<typeof Sparkline>

export default meta
type Story = StoryObj<typeof meta>

const panel = { width: 360, padding: 12, background: 'var(--bg-sidebar)', border: '1px solid var(--border)', borderRadius: 8 } as const

export const Default: Story = {}

export const WithArea: Story = { args: { area: true, width: 120, height: 32 } }

export const Tones: Story = {
  render: (args) => (
    <div style={{ display: 'flex', gap: 16 }}>
      {(['accent', 'ok', 'warn', 'danger', 'muted'] as const).map((tone) => (
        <Sparkline key={tone} {...args} tone={tone} area />
      ))}
    </div>
  ),
}

/** `null` breaks the line; one value is a dot; an empty series is a flat baseline; a constant series is a centred line. */
export const Edges: Story = {
  render: (args) => (
    <div style={{ display: 'flex', gap: 16 }}>
      <Sparkline {...args} values={[3, 5, null, 8, 4, null, null, 6]} />
      <Sparkline {...args} values={[4]} />
      <Sparkline {...args} values={[]} />
      <Sparkline {...args} values={[2, 2, 2, 2]} />
    </div>
  ),
}

/** Pinned min and max put different rows on one scale; values outside are clipped. */
export const SharedScale: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 16 }}>
      <Sparkline label="Small spender" values={[10, 20, 15, 25]} min={0} max={100} />
      <Sparkline label="Big spender" values={[60, 80, 95, 130]} min={0} max={100} />
    </div>
  ),
}

const rows = [
  { name: 'claude-code', values: [120, 340, 280, 510, 460, 620, 700], tone: 'accent' },
  { name: 'codex', values: [900, 700, 650, 400, 380, 200, 150], tone: 'ok' },
  { name: 'gemini', values: [50, null, null, 300, 1200, 2400, 4100], tone: 'danger' },
] as const

export const InRows: Story = {
  render: () => (
    <div style={{ ...panel, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 12 }}>
      {rows.map((r) => (
        <div key={r.name} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
          <span>{r.name}</span>
          <Sparkline label={`${r.name} tokens per day`} values={r.values} tone={r.tone} min={0} max={4500} width={96} />
        </div>
      ))}
    </div>
  ),
}

export const InStatTile: Story = {
  render: () => (
    <div style={panel}>
      <StatTiles>
        <StatTile label="Tokens" value="1.2M" sub={<Sparkline label="Tokens per day" values={[300, 800, 1900, 700, 1000, 900, 1200]} area width={80} />} />
        <StatTile label="Test time" value="42s" sub={<Sparkline label="Duration, last runs" values={[50, 48, 47, 45, 44, 42]} tone="ok" width={80} />} />
      </StatTiles>
    </div>
  ),
}
