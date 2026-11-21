import type { Meta, StoryObj } from '@storybook/react-vite'
import { Agenda, AgendaEvent, type AgendaItem, type Person } from '@wunderui/react'

const RELEASE_SCHEDULE: AgendaItem[] = [
  { id: '1', time: '09:00', title: 'Code freeze for v0.6.0', description: 'Only fixes land on release/0.6 from now on', color: 'var(--destructive)' },
  { id: '2', time: '11:00', title: 'Release candidate build', description: 'task release:rc — macOS, Linux, Windows' },
  { id: '3', time: '14:00', title: 'Smoke tests', description: 'Agent sessions, review drafts, git panel', color: 'var(--chart-3)' },
  { id: '4', time: '17:00', title: 'Publish v0.6.0', description: 'GitHub release and Storybook on pages', color: 'var(--success)' },
]

const TEAM: Person[] = [
  { name: 'Ada Lovelace', initials: 'AL', color: 'indigo' },
  { name: 'Grace Hopper', initials: 'GH', color: 'green' },
  { name: 'Rob Pike', initials: 'RP', color: 'blue' },
  { name: 'Ken Thompson', initials: 'KT', color: 'purple' },
]

const meta = {
  title: 'Data Display/Agenda',
  component: Agenda,
  args: { items: RELEASE_SCHEDULE },
  argTypes: { items: { control: 'object' } },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 520 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Agenda>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Events: Story = {
  name: 'AgendaEvent',
  render: () => (
    <div className="sb-stack">
      <AgendaEvent start="09:00" end="09:15" title="Stand-up" meta="#flint-dev" color="indigo" guests={TEAM} onMenu={() => undefined} />
      <AgendaEvent start="11:00" end="12:00" title="Release candidate build" meta="CI · release/0.6" color="blue" />
      <AgendaEvent
        start="14:00"
        end="15:30"
        title="Review: agent task queue"
        meta="PR #77 · 3 reviewers"
        color="purple"
        guests={TEAM.slice(0, 3)}
        onClick={() => undefined}
        onMenu={() => undefined}
      />
      <AgendaEvent start="16:00" title="Postgres migration window" meta="agent_sessions: add index" color="orange" />
      <AgendaEvent start="17:00" title="Publish v0.6.0" color="green" guests={TEAM.slice(0, 1)} />
    </div>
  ),
}

export const Colors: Story = {
  render: () => (
    <div className="sb-stack">
      {(['indigo', 'blue', 'purple', 'pink', 'red', 'orange', 'yellow', 'green'] as const).map((color) => (
        <AgendaEvent key={color} start="10:00" end="10:30" title={color} color={color} />
      ))}
    </div>
  ),
}
