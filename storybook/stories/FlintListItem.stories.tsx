import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Badge, Button, ListGroup, ListItem } from '@flint-ide/ui'

/** ListItem and ListGroup: two-line list rows (ui/src/ListItem.tsx): pull requests, query history, items in a card. */
const meta = { title: 'Flint/List Item', component: ListItem } satisfies Meta<typeof ListItem>

export default meta
type Story = StoryObj<typeof meta>

const sidebar = { width: 320, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-sidebar)', fontSize: 13, overflow: 'hidden' } as const

/** Props table playground: one row. */
export const Playground: Story = {
  args: {
    label: 'feat(ui): list item and task card',
    meta: ['#96', 'octocat', 'feat/list-item', '2h'],
    trailing: <Badge size="sm" color="green" badgeStyle="light">open</Badge>,
    onClick: () => undefined,
  },
  render: (args) => (
    <div style={sidebar}>
      <ListItem {...args} />
    </div>
  ),
}

const prs = [
  { n: 96, title: 'feat(ui): toasts on the UI kit’s Toaster', author: 'octocat', branch: 'feat/ui-kit-gaps', ci: '✓', state: 'open', color: 'green' as const },
  { n: 94, title: 'chore(deps): bump the npm-minor group in /frontend with 12 updates', author: 'dependabot', branch: 'dependabot/npm_and_yarn/frontend/npm-minor', ci: '●', state: 'draft', color: 'grey' as const },
  { n: 90, title: 'fix(git): keep the commit message when the hook fails', author: 'hubot', branch: 'fix/commit-hook', ci: '✗', state: 'merged', color: 'purple' as const },
]

/** A pull request list: CI glyph, title, state badge, meta with separators; the open one is active. Tab to a row, Enter opens it. */
export const PullRequests: Story = {
  args: { label: '' },
  render: () => {
    const [active, setActive] = useState(96)
    return (
      <div style={sidebar}>
        {prs.map((pr) => (
          <ListItem
            key={pr.n}
            active={pr.n === active}
            onClick={() => setActive(pr.n)}
            leading={<span>{pr.ci}</span>}
            label={pr.title}
            trailing={<Badge size="sm" color={pr.color} badgeStyle="light">{pr.state}</Badge>}
            meta={[`#${pr.n}`, pr.author, <span className="fui-mono">{pr.branch}</span>, '3d']}
          />
        ))}
      </div>
    )
  },
}

const statements = [
  { sql: 'SELECT id, email FROM users WHERE created_at > now() - interval 1 day', meta: ['just now', '12 ms', '41 rows'] },
  { sql: 'UPDATE orders SET status = $1 WHERE id = $2', meta: ['2 min ago', '3 ms', '1 rows'], kind: 'edit' },
  { sql: 'SELECT * FROM missing_table', meta: ['5 min ago', '1 ms', <span className="fui-danger">relation "missing_table" does not exist</span>], error: true },
]

/** Query history: monospace label, a failed statement dimmed, actions on hover and on the keyboard cursor's row. */
export const HoverActions: Story = {
  args: { label: '' },
  render: () => {
    const [cursor, setCursor] = useState(0)
    return (
      <div style={{ ...sidebar, width: 520 }}>
        {statements.map((s, i) => (
          <ListItem
            key={s.sql}
            selected={i === cursor}
            onMouseMove={() => setCursor(i)}
            mono
            dimmed={s.error}
            leading={s.kind && <Badge size="sm" color="indigo" badgeStyle="border_color">{s.kind}</Badge>}
            label={s.sql}
            meta={s.meta}
            actions={
              <>
                <Button variant="ghost" size="sm">Insert</Button>
                <Button variant="ghost" size="sm">Run</Button>
                <Button variant="ghost" size="sm">Copy</Button>
              </>
            }
          />
        ))}
      </div>
    )
  },
}

/** Compact rows with actions always shown, in a ListGroup (the agent tasks of a work item). */
export const CompactInGroup: Story = {
  args: { label: '' },
  render: () => (
    <div style={sidebar}>
      <ListGroup meta="one agent · 2 repositories" tooltip="One agent works in both worktrees">
        {['flint', 'flint-extensions'].map((repo, i) => (
          <ListItem
            key={repo}
            compact
            leading={<Badge size="sm" color="blue" badgeStyle="light">working</Badge>}
            label={<span className="fui-muted">{repo}</span>}
            badges={i === 0 && <Badge size="sm" color="grey" badgeStyle="light">lead</Badge>}
            showActions="always"
            actions={<Button variant="ghost" size="sm">Open</Button>}
          />
        ))}
      </ListGroup>
    </div>
  ),
}
