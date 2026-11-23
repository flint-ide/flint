import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Badge, Button, ListGroup, ListItem, TaskCard, TreeRow } from '@flint-ide/ui'

/**
 * TaskCard: an agent task or a work item: state, title, meta, CI, notes, actions and sub-rows (ui/src/TaskCard.tsx).
 * The actions bar (the card's last line, always shown) is the one place an agent panel puts an item's actions: quiet
 * words; `data-primary` on the one that waits for a person (Approve, Check again) gives it the accent's text colour.
 * A card that opens on click or carries actions hovers and shows its selection as one unit, bar and sub-rows included.
 */
const meta = { title: 'Flint/Task Card', component: TaskCard } satisfies Meta<typeof TaskCard>

export default meta
type Story = StoryObj<typeof meta>

const sidebar = { width: 340, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-sidebar)', fontSize: 13, overflow: 'hidden' } as const

const state = (label: string, color: 'blue' | 'green' | 'red' | 'yellow') => (
  <Badge size="sm" color={color} badgeStyle="light">
    {label}
  </Badge>
)

/** Props table playground. */
export const Playground: Story = {
  args: {
    label: 'Add a list item to the UI kit',
    status: state('working', 'blue'),
    meta: ['flint/list-item', '+120 −40 · 6 files', '↑3', '41k tokens · $0.62'],
    ci: { state: 'pending', title: '3 checks running' },
    actions: (
      <>
        <Button variant="ghost" size="sm" data-primary>Approve</Button>
        <Button variant="ghost" size="sm">Open</Button>
        <Button variant="ghost" size="sm">Show diff</Button>
      </>
    ),
  },
  render: (args) => (
    <div style={sidebar}>
      <TaskCard {...args} />
    </div>
  ),
}

const files = [
  { name: 'ListItem.tsx', dir: 'ui/src', diff: '+128 −0' },
  { name: 'components.css', dir: 'ui/css', diff: '+72 −0' },
  { name: 'style.css', dir: 'frontend/src', diff: '+3 −320' },
]

/** The Agent Tasks panel: a group of attempts, a twisty that shows the changed files, CI states and an error note. */
export const AgentTasks: Story = {
  args: { label: '' },
  render: () => {
    const [open, setOpen] = useState(true)
    const actions = (extra?: string) => (
      <>
        <Button variant="ghost" size="sm">Open</Button>
        {extra && <Button variant="ghost" size="sm">{extra}</Button>}
        <span className="fui-grow" />
        <Button variant="ghost" size="sm" title="Remove the task">Remove</Button>
      </>
    )
    return (
      <div style={sidebar}>
        <ListGroup label="Move the views onto the kit" meta="2 finished · 1 failed" actions={<Button variant="ghost" size="sm">Compare</Button>}>
          <TaskCard
            twisty={open ? 'open' : 'closed'}
            onOpen={() => setOpen((o) => !o)}
            status={state('finished', 'green')}
            label="claude"
            meta={[<span className="fui-mono">flint/kit-1</span>, '+203 −320 · 3 files', '↑2']}
            ci={{ state: 'success', title: 'All 8 checks passed' }}
            actions={actions('Keep: Merge')}
          >
            {open &&
              files.map((f) => (
                <TreeRow key={f.name} depth={2} label={f.name} file hint={f.dir} badge={<span className="fui-mono fui-muted">{f.diff}</span>} />
              ))}
          </TaskCard>
          <TaskCard
            twisty="closed"
            onOpen={() => undefined}
            status={state('finished', 'green')}
            label="codex"
            meta={[<span className="fui-mono">flint/kit-2</span>, '+180 −290 · 4 files']}
            ci={{ state: 'failure', title: '✗ frontend / build' }}
            actions={actions('✦ Fix CI')}
          />
          <TaskCard
            twisty="none"
            status={state('failed', 'red')}
            label="gemini"
            meta={[<span className="fui-mono">flint/kit-3</span>]}
            notes={[{ text: 'The agent stopped: the worktree has conflicts in style.css', tone: 'danger' }]}
            actions={actions('✦ Ask agent')}
          />
        </ListGroup>
      </div>
    )
  },
}

/** A work item on the project board: a WunderUI Card with priority, dependency notes and its agent tasks. Drag it. */
export const BoardCard: Story = {
  args: { label: '' },
  render: () => {
    const [dragging, setDragging] = useState(false)
    return (
      <div style={{ width: 280, padding: 8, background: 'var(--bg-sidebar)', borderRadius: 8 }}>
        <TaskCard
          frame="card"
          draggable
          dragging={dragging}
          onDragStart={() => setDragging(true)}
          onDragEnd={() => setDragging(false)}
          label="Split the settings page"
          badges={<Badge size="sm" color="yellow" badgeStyle="light">not ready</Badge>}
          trailing={<Badge size="sm" color="yellow" badgeStyle="light">high</Badge>}
          meta={['✦ Claude Code', 'flint, flint-docs']}
          notes={[{ text: 'waits for Move the views onto the kit', tone: 'warn' }]}
          actions={
            <>
              <Button variant="ghost" size="sm">Delegate</Button>
              <Button variant="ghost" size="sm">Open</Button>
              <Button variant="ghost" size="sm">Status ⌄</Button>
            </>
          }
        >
          <ListItem
            compact
            leading={state('working', 'blue')}
            label={<span className="fui-muted">flint</span>}
            showActions="always"
            actions={<Button variant="ghost" size="sm">Open</Button>}
          />
        </TaskCard>
      </div>
    )
  },
}
