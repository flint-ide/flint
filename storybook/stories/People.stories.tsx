import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  Avatar,
  AvatarBadge,
  AvatarFallback,
  AvatarLabel,
  Badge,
  IconButton,
  PersonCell,
  UserInfo,
  type AvatarBadgeStatus,
  type AvatarColor,
} from '@wunderui/react'
import { MoreHorizontal } from 'lucide-react'

const STATUSES: AvatarBadgeStatus[] = ['available', 'busy', 'away', 'dnd', 'offline', 'unknown']
const COLORS: AvatarColor[] = ['indigo', 'blue', 'purple', 'pink', 'red', 'orange', 'yellow', 'green', 'alternative', 'grey']

const TEAM = [
  { name: 'Ada Lovelace', subtext: 'Maintainer · flint-ide', color: 'indigo', load: 42, hours: '17h' },
  { name: 'Grace Hopper', subtext: 'Reviewer · compiler team', color: 'green', load: 78, hours: '31h' },
  { name: 'Linus Torvalds', subtext: 'Release manager', color: 'orange', load: 96, hours: '38h' },
  { name: 'Claude', subtext: 'Agent · 3 tasks running', color: 'purple', load: 120, hours: '48h' },
] as const

const menu = (
  <IconButton variant="ghost" size="sm" aria-label="More">
    <MoreHorizontal />
  </IconButton>
)

/** A person row: avatar, name and a second line, optionally with a load bar or a trailing control. */
const meta = {
  title: 'Data Display/People',
  component: AvatarLabel,
  args: {
    name: 'Ada Lovelace',
    subtext: 'ada@flint.dev',
    color: 'indigo',
    size: 'default',
    compact: false,
    tight: true,
    nameWeight: 'medium',
  },
  argTypes: {
    color: { control: 'select', options: COLORS },
    size: { control: 'inline-radio', options: ['default', 'lg', 'xl'] },
    nameWeight: { control: 'inline-radio', options: ['medium', 'semibold'] },
    subtext: { control: 'text' },
    trailing: { control: false },
    capacity: { control: 'object' },
  },
  decorators: [(Story) => <div style={{ maxWidth: 320 }}>{Story()}</div>],
} satisfies Meta<typeof AvatarLabel>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** The three sizes, regular and compact. */
export const Sizes: Story = {
  render: () => (
    <div className="sb-stack">
      {(['default', 'lg', 'xl'] as const).map((size) => (
        <div key={size} className="sb-row" style={{ gap: 24, flexWrap: 'nowrap' }}>
          <span className="sb-label">{size}</span>
          <AvatarLabel name="Grace Hopper" subtext="Reviewer" color="green" size={size} />
          <AvatarLabel name="Grace Hopper" subtext="Reviewer" color="green" size={size} compact />
        </div>
      ))}
    </div>
  ),
  decorators: [(Story) => <div>{Story()}</div>],
}

/** `capacity` adds a load bar: green up to 80 %, yellow up to 100 %, red above. */
export const Capacity: Story = {
  render: () => (
    <div className="sb-stack">
      {TEAM.map((p) => (
        <AvatarLabel key={p.name} name={p.name} subtext={p.subtext} color={p.color} capacity={{ value: p.load, label: p.hours }} />
      ))}
    </div>
  ),
}

/** `trailing` holds a control at the end of the row; `tight={false}` adds 2 px between the lines. */
export const Trailing: Story = {
  render: () => (
    <div className="sb-stack">
      <AvatarLabel name="Rob Pike" subtext="rob@flint.dev" color="blue" trailing={menu} />
      <AvatarLabel
        name="Ken Thompson"
        subtext="Requested review"
        color="purple"
        tight={false}
        trailing={
          <Badge size="sm" color="yellow" badgeStyle="light">
            pending
          </Badge>
        }
      />
    </div>
  ),
}

/** `AvatarBadge status`: every presence state. */
export const Presence: Story = {
  render: () => (
    <div className="sb-row" style={{ gap: 20 }}>
      {STATUSES.map((status, i) => (
        <div key={status} className="sb-stack" style={{ alignItems: 'center', gap: 6 }}>
          <Avatar size="lg" color={COLORS[i]}>
            <AvatarFallback>{TEAM[i % TEAM.length].name.slice(0, 2).toUpperCase()}</AvatarFallback>
            <AvatarBadge status={status} />
          </Avatar>
          <span className="sb-label" style={{ width: 'auto' }}>
            {status}
          </span>
        </div>
      ))}
    </div>
  ),
  decorators: [(Story) => <div>{Story()}</div>],
}

/** `PersonCell` (deprecated; use AvatarLabel): a table cell with a role and an optional capacity bar. */
export const PersonCellDeprecated: Story = {
  name: 'PersonCell (deprecated)',
  render: () => (
    <div className="sb-stack">
      <PersonCell person={{ name: 'Ada Lovelace', color: 'indigo' }} role="Maintainer" />
      <PersonCell person={{ name: 'Grace Hopper', color: 'green' }} role="Reviewer" capacity={{ value: 78, label: '31h' }} />
    </div>
  ),
}

/** `UserInfo` (deprecated; use AvatarLabel) in its three variants. */
export const UserInfoDeprecated: Story = {
  name: 'UserInfo (deprecated)',
  render: () => (
    <div className="sb-stack">
      {(['person', 'sidebar', 'menu'] as const).map((variant) => (
        <div key={variant} className="sb-row" style={{ flexWrap: 'nowrap' }}>
          <span className="sb-label">{variant}</span>
          <UserInfo name="Linus Torvalds" subtext="linus@flint.dev" color="orange" variant={variant} onMenu={() => {}} />
        </div>
      ))}
    </div>
  ),
}
