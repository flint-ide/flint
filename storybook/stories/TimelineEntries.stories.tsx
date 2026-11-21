import type { Meta, StoryObj } from '@storybook/react-vite'
import { AgendaEventChip, Badge, TimelineEntry, type EventColor } from '@wunderui/react'
import { Check, CircleDashed, GitCommit, GitPullRequest, Loader, Rocket, TriangleAlert } from 'lucide-react'

const EVENT_COLORS: EventColor[] = ['indigo', 'blue', 'purple', 'pink', 'red', 'orange', 'yellow', 'green']

const quote: React.CSSProperties = {
  marginTop: 6,
  padding: '8px 10px',
  borderRadius: 6,
  border: '1px solid var(--border)',
  background: 'var(--bg-raised)',
  fontSize: 13,
}

/** One entry of an activity feed (avatar) or a step list (status dot), stacked in a column. */
const meta = {
  title: 'Data Display/Timeline Entries',
  component: TimelineEntry,
  args: {
    title: 'Ada Lovelace opened a pull request',
    time: '2h ago',
    avatar: { initials: 'AL', color: 'indigo' },
    object: { icon: <GitPullRequest />, label: 'flint-ide/flint#76' },
    meta: 'feat/ui-kit → main',
    last: true,
  },
  argTypes: {
    title: { control: 'text' },
    time: { control: 'text' },
    meta: { control: 'text' },
    object: { control: false },
    status: { control: false },
    children: { control: false },
  },
  decorators: [(Story) => <div style={{ maxWidth: 520 }}>{Story()}</div>],
} satisfies Meta<typeof TimelineEntry>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Type=event: actor avatars, object chips and content below the title. */
export const ActivityFeed: Story = {
  name: 'Activity feed',
  render: () => (
    <div>
      <TimelineEntry
        avatar={{ initials: 'AL', color: 'indigo' }}
        title="Ada Lovelace opened a pull request"
        time="2h ago"
        object={{ icon: <GitPullRequest />, label: 'flint-ide/flint#76' }}
      />
      <TimelineEntry
        avatar={{ initials: 'GH', color: 'green' }}
        title="Grace Hopper commented"
        time="1h ago"
        object={{ icon: <GitCommit />, label: 'a5fvh9' }}
      >
        <div style={quote}>Could we keep review drafts next to the worktree instead of in ~/.flint?</div>
      </TimelineEntry>
      <TimelineEntry avatar={{ initials: 'CL', color: 'purple' }} title="Claude pushed 3 commits" time="40m ago" meta="fix: symlinked folders" />
      <TimelineEntry avatar={{ initials: 'LT', color: 'orange' }} title="Linus Torvalds approved" time="12m ago" last />
    </div>
  ),
}

/** Type=step: status dots in four tones with a trailing badge, like a CI pipeline. */
export const PipelineSteps: Story = {
  name: 'Pipeline steps',
  render: () => (
    <div>
      <TimelineEntry status={{ tone: 'success', icon: <Check /> }} title="lint" time="0:42" badge="passed" badgeColor="green" />
      <TimelineEntry status={{ tone: 'success', icon: <Check /> }} title="test" time="3:18" badge="passed" badgeColor="green" meta="412 tests · 81% coverage" />
      <TimelineEntry status={{ tone: 'warning', icon: <TriangleAlert /> }} title="e2e" time="5:02" badge="flaky" badgeColor="yellow" meta="1 retry: review-draft.spec.ts" />
      <TimelineEntry status={{ tone: 'primary', icon: <Loader /> }} title="build" time="running" badge="in progress" badgeColor="indigo" />
      <TimelineEntry status={{ tone: 'muted', icon: <CircleDashed /> }} title="deploy to Pages" badge="queued" badgeColor="grey" last />
    </div>
  ),
}

/** A release entry with richer children: badges and an attachment line. */
export const WithChildren: Story = {
  name: 'With children',
  args: {
    avatar: undefined,
    status: { tone: 'success', icon: <Rocket /> },
    title: 'Released v0.9.0',
    time: 'Nov 20',
    object: undefined,
    meta: undefined,
    children: (
      <div className="sb-row" style={{ marginTop: 6 }}>
        <Badge size="sm" badgeStyle="light" color="blue">
          darwin-arm64
        </Badge>
        <Badge size="sm" badgeStyle="light" color="blue">
          linux-amd64
        </Badge>
        <Badge size="sm" badgeStyle="light" color="blue">
          windows-amd64
        </Badge>
      </div>
    ),
  },
}

/** `AgendaEventChip`: a compact event button for calendar cells, in every colour. */
export const AgendaEventChips: Story = {
  name: 'Agenda event chips',
  render: () => (
    <div className="sb-stack" style={{ gap: 6 }}>
      {EVENT_COLORS.map((color, i) => (
        <div key={color} className="sb-row">
          <span className="sb-label">{color}</span>
          <AgendaEventChip color={color}>
            {['Sprint review', 'Release v0.9', 'Pairing: LSP', 'Design sync', 'Incident retro', 'Planning', 'Docs day', '1:1'][i]}
          </AgendaEventChip>
        </div>
      ))}
    </div>
  ),
}

/** Chips stacked inside a calendar day cell. */
export const InADayCell: Story = {
  name: 'In a day cell',
  render: () => (
    <div
      className="sb-stack"
      style={{ width: 160, gap: 4, padding: 8, borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-raised)' }}
    >
      <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>Thu 21</span>
      <AgendaEventChip color="indigo">10:00 Standup</AgendaEventChip>
      <AgendaEventChip color="green">13:00 Release v0.9</AgendaEventChip>
      <AgendaEventChip color="red">16:30 Incident retro</AgendaEventChip>
    </div>
  ),
}
