import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  Avatar,
  AvatarBadge,
  AvatarFallback,
  AvatarGroup,
  AvatarGroupCount,
  AvatarImage,
  AvatarStack,
  type AvatarColor,
  type Person,
} from '@wunderui/react'
import { Bot, Check } from 'lucide-react'

const SIZES = ['sm', 'xs', 'default', 'lg', 'xl'] as const
const COLORS: AvatarColor[] = ['indigo', 'blue', 'purple', 'pink', 'red', 'orange', 'yellow', 'green', 'alternative', 'grey']

const REVIEWERS: Person[] = [
  { name: 'Ada Lovelace', initials: 'AL', color: 'indigo' },
  { name: 'Grace Hopper', initials: 'GH', color: 'green' },
  { name: 'Linus Torvalds', initials: 'LT', color: 'orange' },
  { name: 'Rob Pike', initials: 'RP', color: 'blue' },
  { name: 'Ken Thompson', initials: 'KT', color: 'purple' },
  { name: 'Dan Abramov', initials: 'DA', color: 'pink' },
]

const meta = {
  title: 'Data Display/Avatar',
  component: Avatar,
  args: { size: 'default', shape: 'circle', variant: 'plain', color: 'indigo' },
  argTypes: {
    size: { control: 'inline-radio', options: SIZES },
    shape: { control: 'inline-radio', options: ['circle', 'square'] },
    variant: { control: 'inline-radio', options: ['plain', 'primary'] },
    color: { control: 'select', options: COLORS },
  },
  render: (args) => (
    <Avatar {...args}>
      <AvatarFallback>AL</AvatarFallback>
    </Avatar>
  ),
} satisfies Meta<typeof Avatar>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Sizes: Story = {
  render: (args) => (
    <div className="sb-stack">
      {(['circle', 'square'] as const).map((shape) => (
        <div key={shape} className="sb-row">
          <span className="sb-label">{shape}</span>
          {SIZES.map((size) => (
            <Avatar key={size} {...args} size={size} shape={shape}>
              <AvatarFallback>GH</AvatarFallback>
            </Avatar>
          ))}
        </div>
      ))}
    </div>
  ),
}

export const Colors: Story = {
  render: (args) => (
    <div className="sb-row">
      {COLORS.map((color, i) => (
        <Avatar key={color} {...args} color={color} title={color}>
          <AvatarFallback>{REVIEWERS[i % REVIEWERS.length].initials}</AvatarFallback>
        </Avatar>
      ))}
    </div>
  ),
}

export const ImageWithFallback: Story = {
  name: 'Image with fallback',
  render: () => (
    <div className="sb-row">
      {/* No network in the story: the image fails and the initials show instead. */}
      <Avatar size="lg" color="blue">
        <AvatarImage src="/missing-avatar.png" alt="Rob Pike" />
        <AvatarFallback>RP</AvatarFallback>
      </Avatar>
      <span className="sb-label" style={{ width: 'auto' }}>
        image failed to load, fallback initials rendered
      </span>
    </div>
  ),
}

export const WithBadge: Story = {
  name: 'With badge',
  render: () => (
    <div className="sb-row">
      <Avatar size="lg" color="green">
        <AvatarFallback>GH</AvatarFallback>
        <AvatarBadge style={{ width: 10, height: 10, backgroundColor: 'var(--success)' }} aria-label="online" />
      </Avatar>
      <Avatar size="lg" color="indigo">
        <AvatarFallback>AL</AvatarFallback>
        <AvatarBadge style={{ width: 14, height: 14 }} aria-label="approved">
          <Check style={{ width: 9, height: 9 }} />
        </AvatarBadge>
      </Avatar>
      <Avatar size="lg" shape="square" variant="primary">
        <AvatarFallback>
          <Bot style={{ width: 18, height: 18 }} />
        </AvatarFallback>
        <AvatarBadge style={{ width: 10, height: 10, backgroundColor: 'var(--destructive)' }} aria-label="agent stopped" />
      </Avatar>
    </div>
  ),
}

export const Group: Story = {
  render: () => (
    <div className="sb-stack">
      <div className="sb-row">
        <span className="sb-label">reviewers</span>
        <AvatarGroup>
          {REVIEWERS.slice(0, 4).map((p) => (
            <Avatar key={p.name} color={p.color} title={p.name}>
              <AvatarFallback>{p.initials}</AvatarFallback>
            </Avatar>
          ))}
          <AvatarGroupCount>+{REVIEWERS.length - 4}</AvatarGroupCount>
        </AvatarGroup>
      </div>
      <div className="sb-row">
        <span className="sb-label">small</span>
        <AvatarGroup>
          {REVIEWERS.slice(0, 3).map((p) => (
            <Avatar key={p.name} size="sm" color={p.color} title={p.name}>
              <AvatarFallback>{p.initials}</AvatarFallback>
            </Avatar>
          ))}
        </AvatarGroup>
      </div>
    </div>
  ),
}

export const StackOfReviewers: Story = {
  name: 'Stack (PR reviewers)',
  render: () => (
    <div className="sb-stack">
      {(['sm', 'xs', 'default'] as const).map((size) => (
        <div key={size} className="sb-row">
          <span className="sb-label">{size}</span>
          <AvatarStack people={REVIEWERS} size={size} max={3} />
          <span style={{ fontSize: 13, color: 'var(--fg-muted)' }}>#76 feat: storybook for WunderUI</span>
        </div>
      ))}
      <div className="sb-row">
        <span className="sb-label">max=5</span>
        <AvatarStack people={REVIEWERS} max={5} />
      </div>
    </div>
  ),
}
