import type { Meta, StoryObj } from '@storybook/react-vite'
import { StatTile, StatTiles, StatusTracker } from '@flint-ide/ui'

/** StatusTracker: a strip of blocks, one per run, oldest left (ui/src/StatusTracker.tsx, logic in ui/src/statusTrackerMath.ts). */
const meta = {
  title: 'Flint/Status Tracker',
  component: StatusTracker,
  args: { label: 'Nightly build', max: 30, items: [] },
} satisfies Meta<typeof StatusTracker>

export default meta
type Story = StoryObj<typeof meta>

const panel = { width: 360, padding: 12, background: 'var(--bg-sidebar)', border: '1px solid var(--border)', borderRadius: 8 } as const

type State = 'ok' | 'failed' | 'warn' | 'running' | 'skipped'
const run = (n: number, state: State) => ({ id: `run-${n}`, state, title: `Run #${n}` })
const history = (states: readonly State[]) => states.map((s, i) => run(i + 1, s))

const mixed = history([...Array<State>(12).fill('ok'), 'failed', 'ok', 'ok', 'warn', ...Array<State>(8).fill('ok'), 'skipped', 'ok', 'failed', 'ok', 'running'])

export const Default: Story = { args: { items: mixed } }

export const Medium: Story = { args: { items: mixed, size: 'md' } }

/** Fewer runs than `max`: the left is padded so strips line up in a list. */
export const FewRuns: Story = { args: { items: history(['ok', 'failed', 'running']) } }

export const Empty: Story = {}

/** With `onSelect` every block is a button; one tab stop, arrow keys, Home and End move between them. */
export const Selectable: Story = {
  args: { items: mixed, size: 'md' },
  render: (args) => <StatusTracker {...args} onSelect={(item) => window.alert(`Open ${item.title ?? item.id}`)} />,
}

export const InRows: Story = {
  render: () => (
    <div style={{ ...panel, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 12 }}>
      {[
        { name: 'CI', items: mixed },
        { name: 'Release', items: history(['ok', 'ok', 'failed']) },
        { name: 'Docs', items: history(Array<State>(30).fill('ok')) },
      ].map((r) => (
        <div key={r.name} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
          <span>{r.name}</span>
          <StatusTracker label={r.name} items={r.items} max={20} />
        </div>
      ))}
    </div>
  ),
}

export const InStatTile: Story = {
  render: () => (
    <div style={panel}>
      <StatTiles>
        <StatTile label="CI" value="27/30" sub={<StatusTracker label="CI" items={mixed} max={12} />} />
        <StatTile label="Release" value="2/3" sub={<StatusTracker label="Release" items={history(['ok', 'failed', 'ok'])} max={12} />} />
      </StatTiles>
    </div>
  ),
}
