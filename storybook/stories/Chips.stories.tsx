import type { Meta, StoryObj } from '@storybook/react-vite'
import { BADGE_COLORS, Badge, Kbd, KbdGroup, TrendChip, type BadgeColor, type BadgeStyle } from '@wunderui/react'

const COLORS = Object.keys(BADGE_COLORS) as BadgeColor[]
const STYLES: BadgeStyle[] = ['plain', 'light', 'light_border', 'border_color', 'border_grey', 'ghost']

const meta = {
  title: 'Data Display/Badges & Chips',
  component: Badge,
  args: { children: 'passing', color: 'green', badgeStyle: 'light', shape: 'rounded' },
  argTypes: {
    color: { control: 'select', options: COLORS },
    badgeStyle: { control: 'select', options: STYLES },
    shape: { control: 'inline-radio', options: ['rounded', 'pill'] },
  },
} satisfies Meta<typeof Badge>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const AllColorsAndStyles: Story = {
  render: (args) => (
    <div className="sb-stack">
      {STYLES.map((style) => (
        <div key={style} className="sb-row">
          <span className="sb-label">{style}</span>
          {COLORS.map((color) => (
            <Badge key={color} {...args} color={color} badgeStyle={style}>
              {color}
            </Badge>
          ))}
        </div>
      ))}
    </div>
  ),
}

export const StatusChips: Story = {
  render: () => (
    <div className="sb-row">
      <Badge shape="pill" color="green" badgeStyle="light">
        passing
      </Badge>
      <Badge shape="pill" color="red" badgeStyle="light">
        3 failed
      </Badge>
      <Badge shape="pill" color="yellow" badgeStyle="light">
        running
      </Badge>
      <Badge shape="pill" color="indigo" badgeStyle="light_border">
        draft
      </Badge>
      <Badge shape="pill" color="grey" badgeStyle="border_grey">
        merged
      </Badge>
    </div>
  ),
}

export const Trend: Story = {
  render: () => (
    <div className="sb-row">
      <TrendChip value="12%" direction="up" />
      <TrendChip value="4%" direction="down" />
      <TrendChip value="0%" direction="neutral" />
      <TrendChip value="8%" direction="down" sentiment="positive" />
    </div>
  ),
}

export const Keys: Story = {
  render: () => (
    <div className="sb-row">
      <KbdGroup>
        <Kbd>⇧</Kbd>
        <Kbd>⌘</Kbd>
        <Kbd>P</Kbd>
      </KbdGroup>
      <Kbd>Esc</Kbd>
    </div>
  ),
}
