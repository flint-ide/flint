import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, ContextMeter } from '@flint-ide/ui'

/** ContextMeter: how full the model's context window is. A ring for the prompt toolbar, a stacked bar with a legend elsewhere (ui/src/chat/ContextMeter.tsx). */
const meta = {
  title: 'Flint/Context Meter',
  component: ContextMeter,
  args: { used: 124_000, limit: 200_000 },
} satisfies Meta<typeof ContextMeter>

export default meta
type Story = StoryObj<typeof meta>

const parts = [
  { id: 'system', label: 'System prompt', tokens: 6_200 },
  { id: 'tools', label: 'Tools', tokens: 18_400 },
  { id: 'files', label: 'Files', tokens: 54_000 },
  { id: 'chat', label: 'Conversation', tokens: 45_400 },
]

const row = { display: 'flex', alignItems: 'center', gap: 24 } as const
const panel = { width: 280, padding: 12, background: 'var(--bg-raised)', border: '1px solid var(--border)', borderRadius: 8 } as const

/** Green below 70 %, amber from 70 %, red from 90 %. The number is in the tooltip. */
export const RingLevels: Story = {
  render: () => (
    <div style={row}>
      <ContextMeter used={40_000} limit={200_000} />
      <ContextMeter used={150_000} limit={200_000} />
      <ContextMeter used={188_000} limit={200_000} />
      <ContextMeter used={124_000} limit={200_000} size={24} />
    </div>
  ),
}

/** With `parts` the ring is a button: click it for the breakdown. */
export const RingWithBreakdown: Story = {
  render: () => <ContextMeter used={124_000} limit={200_000} parts={parts} />,
}

export const Bar: Story = {
  render: () => (
    <div style={panel}>
      <ContextMeter variant="bar" used={124_000} limit={200_000} parts={parts} />
    </div>
  ),
}

/** Parts that add up to more than the window are scaled to fit. */
export const BarOverLimit: Story = {
  render: () => (
    <div style={panel}>
      <ContextMeter variant="bar" used={210_000} limit={200_000} parts={parts.map((p) => ({ ...p, tokens: p.tokens * 2 }))} />
    </div>
  ),
}

/** Without parts the bar is one fill, coloured by level. */
export const BarWithoutParts: Story = {
  render: () => (
    <div style={panel}>
      <ContextMeter variant="bar" used={150_000} limit={200_000} />
    </div>
  ),
}

/** The agent doesn't report a window: nothing is rendered. */
export const UnknownLimit: Story = {
  render: () => (
    <div style={row}>
      <span className="fui-muted">Before:</span>
      <ContextMeter used={124_000} limit={null} />
      <span className="fui-muted">:After (the meter is absent)</span>
    </div>
  ),
}

/** The ring where it belongs: at the end of a prompt box's toolbar. */
export const InPromptToolbar: Story = {
  render: () => (
    <div style={{ ...panel, width: 420, display: 'flex', flexDirection: 'column', gap: 8 }}>
      <span className="fui-muted">Ask the agent anything…</span>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <Button size="sm" variant="ghost">
          Claude Sonnet
        </Button>
        <span className="fui-grow" style={{ flex: 1 }} />
        <ContextMeter used={150_000} limit={200_000} parts={parts} />
        <Button size="sm">Send</Button>
      </div>
    </div>
  ),
}
