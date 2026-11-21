import type { Meta, StoryObj } from '@storybook/react-vite'
import { ICON_TILE_SIZES, ICON_TILE_TONES, IconTile, type IconTileProps } from '@wunderui/react'
import { Bot, Bug, GitBranch, GitPullRequest, Package, Puzzle, ShieldCheck, SquareTerminal, Workflow } from 'lucide-react'

const ICONS = [GitBranch, GitPullRequest, Bot, Bug, Package, Puzzle, ShieldCheck, SquareTerminal, Workflow]

/** One icon on a tint surface without a rim: extension lists, integration rows, empty states. */
const meta = {
  title: 'Data Display/Icon Tile',
  component: IconTile,
  args: { icon: GitPullRequest as IconTileProps['icon'], tone: 'indigo', size: 32, shape: 'square', label: '' },
  argTypes: {
    icon: { control: false },
    tone: { control: 'select', options: ICON_TILE_TONES },
    size: { control: 'inline-radio', options: ICON_TILE_SIZES },
    shape: { control: 'inline-radio', options: ['square', 'circle'] },
    label: { control: 'text' },
  },
} satisfies Meta<typeof IconTile>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** All nine tones, square and round. */
export const Tones: Story = {
  render: (args) => (
    <div className="sb-stack">
      {(['square', 'circle'] as const).map((shape) => (
        <div key={shape} className="sb-row" style={{ gap: 12 }}>
          <span className="sb-label">{shape}</span>
          {ICON_TILE_TONES.map((tone, i) => (
            <IconTile key={tone} {...args} icon={ICONS[i % ICONS.length]} tone={tone} shape={shape} label={tone} />
          ))}
        </div>
      ))}
    </div>
  ),
}

/** Every size (24 – 48 px); the icon grows with it (14 – 24 px). */
export const Sizes: Story = {
  render: (args) => (
    <div className="sb-row" style={{ gap: 16, alignItems: 'flex-end' }}>
      {ICON_TILE_SIZES.map((size) => (
        <div key={size} className="sb-stack" style={{ alignItems: 'center', gap: 6 }}>
          <IconTile {...args} size={size} />
          <span className="sb-label" style={{ width: 'auto' }}>
            {size}px
          </span>
        </div>
      ))}
    </div>
  ),
}

/** The icon may be a component (`Bot`) or an element (`<Bot />`); both are sized by the tile. */
export const IconAsElement: Story = {
  name: 'Icon as element',
  args: { icon: <Bot />, tone: 'purple', size: 40 },
}

/** In context: an extensions list where the tile is the visual anchor of each row. */
export const InAList: Story = {
  name: 'In a list',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420, gap: 0 }}>
      {(
        [
          [SquareTerminal, 'green', 'Go Tools', 'gopls, delve and staticcheck'],
          [Bot, 'purple', 'Claude Code', 'Agent terminal with workspace context'],
          [ShieldCheck, 'blue', 'Secret Scanner', 'Blocks commits with credentials'],
          [Bug, 'red', 'Sentry Issues', 'Errors linked to the lines that threw'],
        ] as const
      ).map(([icon, tone, name, description]) => (
        <div
          key={name}
          className="sb-row"
          style={{ gap: 12, padding: '10px 0', borderBottom: '1px solid var(--border)', flexWrap: 'nowrap' }}
        >
          <IconTile icon={icon} tone={tone} size={36} />
          <div>
            <div style={{ fontSize: 13, fontWeight: 600 }}>{name}</div>
            <div style={{ fontSize: 12, color: 'var(--fg-muted)' }}>{description}</div>
          </div>
        </div>
      ))}
    </div>
  ),
}
