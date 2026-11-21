import type { Meta, StoryObj } from '@storybook/react-vite'
import { Avatar, AvatarFallback, Badge, Timeline, TimelineEvent, type TimelineItem } from '@wunderui/react'
import { Check, GitCommit, GitMerge, GitPullRequest, MessageSquare, Rocket, X } from 'lucide-react'

const PR_ACTIVITY: TimelineItem[] = [
  { id: '1', title: 'Pull request #76 opened', description: 'feat: Storybook for WunderUI components', timestamp: '09:12', icon: <GitPullRequest /> },
  { id: '2', title: 'CI failed on golangci-lint', description: 'internal/review/drafts.go:88 — unused parameter', timestamp: '09:20', icon: <X />, color: 'var(--destructive)' },
  { id: '3', title: 'Pushed 2 commits', description: 'fix: lint, test: cover symlinked folders', timestamp: '09:41', icon: <GitCommit /> },
  { id: '4', title: 'All checks passed', description: 'go test, vitest, lint, pages build', timestamp: '09:49', icon: <Check />, color: 'var(--success)' },
  { id: '5', title: 'Merged into main', timestamp: '10:03', icon: <GitMerge />, color: 'var(--chart-3)' },
]

const meta = {
  title: 'Data Display/Timeline',
  component: Timeline,
  args: { items: PR_ACTIVITY, orientation: 'vertical', compact: false },
  argTypes: {
    items: { control: false },
    orientation: { control: 'inline-radio', options: ['vertical', 'horizontal'] },
    compact: { control: 'boolean' },
  },
} satisfies Meta<typeof Timeline>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Horizontal: Story = {
  args: {
    orientation: 'horizontal',
    items: [
      { id: 'a', title: 'v0.4.0', description: 'Agent panel', timestamp: 'Sep 2', icon: <Rocket /> },
      { id: 'b', title: 'v0.5.0', description: 'Review drafts', timestamp: 'Oct 7', icon: <Rocket /> },
      { id: 'c', title: 'v0.6.0', description: 'WunderUI kit', timestamp: 'Nov 18', icon: <Rocket />, color: 'var(--success)' },
      { id: 'd', title: 'v0.7.0', description: 'Planned', timestamp: 'Dec 15' },
    ],
  },
}

export const Compact: Story = {
  args: { compact: true },
}

export const WithAvatars: Story = {
  name: 'With avatars',
  args: {
    items: [
      {
        id: 'a',
        title: 'Ada Lovelace requested changes',
        description: 'Please split the settings store migration into its own commit.',
        timestamp: '2h ago',
        avatar: (
          <Avatar size="sm" color="indigo">
            <AvatarFallback>AL</AvatarFallback>
          </Avatar>
        ),
      },
      {
        id: 'b',
        title: 'Claude Code pushed a fix',
        description: 'refactor: extract migrateSettings()',
        timestamp: '1h ago',
        avatar: (
          <Avatar size="sm" color="orange">
            <AvatarFallback>CC</AvatarFallback>
          </Avatar>
        ),
      },
      {
        id: 'c',
        title: 'Grace Hopper approved',
        timestamp: '20m ago',
        avatar: (
          <Avatar size="sm" color="green">
            <AvatarFallback>GH</AvatarFallback>
          </Avatar>
        ),
      },
    ],
  },
}

export const Events: Story = {
  name: 'TimelineEvent',
  render: () => (
    <div style={{ maxWidth: 520 }}>
      <TimelineEvent
        avatar={{ initials: 'RP', color: 'blue' }}
        title={
          <span>
            <strong>Rob Pike</strong> opened a pull request
          </span>
        }
        time="09:12"
        object={{ icon: <GitPullRequest />, label: 'flint-ide/flint#74' }}
      />
      <TimelineEvent
        avatar={{ initials: 'AL', color: 'indigo' }}
        title={
          <span>
            <strong>Ada Lovelace</strong> commented
          </span>
        }
        time="09:30"
        object={{ icon: <MessageSquare />, label: 'internal/review/drafts.go' }}
      >
        <div style={{ fontSize: 13, color: 'var(--fg-muted)' }}>
          Should <code>EvalSymlinks</code> run before or after the workspace root check?
        </div>
      </TimelineEvent>
      <TimelineEvent status={{ tone: 'warning', icon: <X /> }} title="golangci-lint" badge="failed" badgeColor="red" time="09:20" meta="52s" />
      <TimelineEvent status={{ tone: 'primary', icon: <GitCommit /> }} title="go test ./..." badge="running" badgeColor="yellow" time="09:41" />
      <TimelineEvent status={{ tone: 'success', icon: <Check /> }} title="frontend: vitest" badge="passed" badgeColor="green" time="09:49" meta="214 tests" />
      <TimelineEvent status={{ tone: 'muted' }} title="Deploy to pages" badge="queued" time="—" last>
        <div className="sb-row">
          <Badge color="grey" badgeStyle="border_grey">
            waiting for approval
          </Badge>
        </div>
      </TimelineEvent>
    </div>
  ),
}
