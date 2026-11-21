import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { TabBar, TabItem } from '@wunderui/react'

const PR_TABS = [
  { id: 'conversation', label: 'Conversation', count: 12 },
  { id: 'commits', label: 'Commits', count: 7 },
  { id: 'checks', label: 'Checks', count: 4 },
  { id: 'files', label: 'Files changed', count: 23 },
]

const PANELS: Record<string, string> = {
  conversation: '12 comments from 3 reviewers and the review agent.',
  commits: '7 commits on feat/ui-kit since it branched from main.',
  checks: 'lint, test (go), test (frontend) and build: 3 passing, 1 running.',
  files: '23 files changed: +1,204 −212.',
}

type Args = { showCounts: boolean; disableChecks: boolean }

/**
 * A line tab bar with optional counts. Deprecated upstream in favour of `Tabs variant="line"` with
 * `TabsTrigger count`, but still exported. TabBar handles roving focus (←/→, Home/End).
 * Note: TabItem shows its `count` badge only on the active tab; inactive tabs hide it.
 */
const meta = {
  title: 'Navigation/Tab Bar',
  args: { showCounts: true, disableChecks: false },
  render: function Render({ showCounts, disableChecks }) {
    const [active, setActive] = useState('conversation')
    return (
      <div className="sb-stack" style={{ maxWidth: 640 }}>
        <TabBar aria-label="Pull request #76">
          {PR_TABS.map((t) => (
            <TabItem
              key={t.id}
              id={`tab-${t.id}`}
              aria-controls={`panel-${t.id}`}
              active={active === t.id}
              count={showCounts ? t.count : undefined}
              disabled={disableChecks && t.id === 'checks'}
              onClick={() => setActive(t.id)}
            >
              {t.label}
            </TabItem>
          ))}
        </TabBar>
        <div
          id={`panel-${active}`}
          role="tabpanel"
          aria-labelledby={`tab-${active}`}
          style={{ padding: 12, fontSize: 13, color: 'var(--fg-muted)', border: '1px solid var(--border)', background: 'var(--bg-raised)', borderRadius: 6 }}
        >
          {PANELS[active]}
        </div>
      </div>
    )
  },
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {}

export const WithoutCounts: Story = { args: { showCounts: false } }

export const DisabledTab: Story = { args: { disableChecks: true } }

/** Counts can be any node, e.g. "99+" for a busy bottom panel (switch tabs to see each count). */
export const BottomPanel: Story = {
  render: function Render() {
    const tabs = [
      { id: 'problems', label: 'Problems', count: '99+' },
      { id: 'output', label: 'Output' },
      { id: 'terminal', label: 'Terminal', count: 2 },
      { id: 'ports', label: 'Ports', count: 0 },
    ]
    const [active, setActive] = useState('terminal')
    return (
      <TabBar aria-label="Bottom panel">
        {tabs.map((t) => (
          <TabItem key={t.id} active={active === t.id} count={t.count} onClick={() => setActive(t.id)}>
            {t.label}
          </TabItem>
        ))}
      </TabBar>
    )
  },
}
