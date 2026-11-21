import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Button, ChecklistTask, InviteLinkRow, InviteRow } from '@wunderui/react'

const ROLES = ['Owner', 'Maintainer', 'Developer', 'Viewer']

const meta = {
  title: 'Forms/Invite & Onboarding',
  component: InviteRow,
  args: { email: 'ada@flint.dev', role: 'Developer', roles: ROLES, stacked: false },
  argTypes: {
    email: { control: 'text' },
    role: { control: 'select', options: ROLES },
    stacked: { control: 'boolean' },
  },
} satisfies Meta<typeof InviteRow>

export default meta
type Story = StoryObj<typeof meta>

const panel = {
  padding: 20,
  border: '1px solid var(--border)',
  borderRadius: 12,
  background: 'var(--bg-raised)',
} as const

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 480, minHeight: 240 }}>
      <InviteRow {...args} />
    </div>
  ),
}

type Invite = { id: number; email: string; role: string }

export const InviteTeammates: Story = {
  name: 'Composed: invite teammates',
  render: function Render() {
    const [rows, setRows] = useState<Invite[]>([
      { id: 1, email: 'ada@flint.dev', role: 'Maintainer' },
      { id: 2, email: 'linus@flint.dev', role: 'Developer' },
    ])
    const [nextId, setNextId] = useState(3)
    const update = (id: number, patch: Partial<Invite>) =>
      setRows((prev) => prev.map((r) => (r.id === id ? { ...r, ...patch } : r)))
    const add = () => {
      setRows((prev) => [...prev, { id: nextId, email: '', role: 'Developer' }])
      setNextId((n) => n + 1)
    }
    return (
      <div className="sb-stack" style={{ ...panel, maxWidth: 520, minHeight: 360, gap: 14 }}>
        <strong style={{ color: 'var(--fg)' }}>Invite to workspace “flint-ide”</strong>
        {rows.map((r) => (
          <InviteRow
            key={r.id}
            email={r.email}
            role={r.role}
            roles={ROLES}
            onEmailChange={(email) => update(r.id, { email })}
            onRoleChange={(role) => update(r.id, { role })}
            onRemove={() => setRows((prev) => prev.filter((x) => x.id !== r.id))}
          />
        ))}
        <div className="sb-row" style={{ justifyContent: 'space-between' }}>
          <Button variant="ghost" size="sm" onClick={add}>
            Add another
          </Button>
          <Button size="sm" disabled={rows.length === 0}>
            Send {rows.length} invite{rows.length === 1 ? '' : 's'}
          </Button>
        </div>
        <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>Or share a link:</span>
        <InviteLinkRow link="https://flint.dev/invite/ws_8f3k2m9q" />
      </div>
    )
  },
}

export const Stacked: Story = {
  name: 'InviteRow stacked',
  render: () => (
    <div style={{ maxWidth: 300, minHeight: 240 }}>
      <InviteRow email="grace@flint.dev" role="Viewer" roles={ROLES} stacked onRemove={() => undefined} />
    </div>
  ),
}

export const LinkRow: Story = {
  name: 'InviteLinkRow (deprecated)',
  render: () => (
    <div style={{ maxWidth: 460 }}>
      <InviteLinkRow link="https://flint.dev/invite/ws_8f3k2m9q" />
    </div>
  ),
}

export const ChecklistStates: Story = {
  name: 'ChecklistTask states',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 440, gap: 6 }}>
      <ChecklistTask done>Open a folder</ChecklistTask>
      <ChecklistTask
        current
        meta="Flint uses your token to clone, push and open pull requests."
        action={<Button size="sm">Connect</Button>}
      >
        Connect GitHub
      </ChecklistTask>
      <ChecklistTask>Pick an agent model</ChecklistTask>
    </div>
  ),
}

type Step = { id: string; label: string; meta: string; cta: string }

const STEPS: Step[] = [
  { id: 'folder', label: 'Open a project folder', meta: 'Flint indexes it for search and the agent.', cta: 'Open folder' },
  { id: 'github', label: 'Connect GitHub', meta: 'Needed to push branches and open pull requests.', cta: 'Connect' },
  { id: 'agent', label: 'Choose an agent model', meta: 'You can change it per session later.', cta: 'Choose' },
  { id: 'format', label: 'Turn on format on save', meta: 'Uses the formatter configured for each language.', cta: 'Enable' },
  { id: 'invite', label: 'Invite a teammate', meta: 'Share agents, snippets and settings.', cta: 'Invite' },
]

export const OnboardingChecklist: Story = {
  name: 'Composed: getting started',
  render: function Render() {
    const [done, setDone] = useState<Record<string, boolean>>({ folder: true })
    const toggle = (id: string) => setDone((prev) => ({ ...prev, [id]: !prev[id] }))
    const currentId = STEPS.find((s) => !done[s.id])?.id
    const count = STEPS.filter((s) => done[s.id]).length
    return (
      <div className="sb-stack" style={{ ...panel, maxWidth: 460, gap: 8 }}>
        <div className="sb-row" style={{ justifyContent: 'space-between' }}>
          <strong style={{ color: 'var(--fg)' }}>Get started with Flint</strong>
          <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
            {count}/{STEPS.length}
          </span>
        </div>
        {STEPS.map((s) => (
          <ChecklistTask
            key={s.id}
            done={Boolean(done[s.id])}
            current={s.id === currentId}
            meta={s.id === currentId ? s.meta : undefined}
            action={
              s.id === currentId ? (
                <Button size="sm" onClick={() => toggle(s.id)}>
                  {s.cta}
                </Button>
              ) : undefined
            }
            onClick={() => toggle(s.id)}
          >
            {s.label}
          </ChecklistTask>
        ))}
      </div>
    )
  },
}
