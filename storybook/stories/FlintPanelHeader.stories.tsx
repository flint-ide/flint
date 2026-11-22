import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Badge, IconButton, PanelHeader, Segment } from '@flint-ide/ui'
import { ArrowDownToLine, GitBranch, Plus, RefreshCw, Upload } from '@flint-ide/ui/icons'

/** PanelHeader: the 32 px title bar of every sidebar panel (ui/src/PanelHeader.tsx). */
const meta = { title: 'Flint/Panel Header', component: PanelHeader } satisfies Meta<typeof PanelHeader>

export default meta
type Story = StoryObj<typeof meta>

const sidebar = { width: 300, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-sidebar)', fontSize: 13 } as const

export const Default: Story = {
  args: { title: 'Databases' },
  render: (args) => (
    <div style={sidebar}>
      <PanelHeader
        {...args}
        actions={<IconButton size="sm" variant="ghost" aria-label="New connection" title="New connection"><Plus /></IconButton>}
      />
    </div>
  ),
}

/** Several actions, and a muted line under the header (the Commit panel's branch). */
export const WithSubtitle: Story = {
  args: { title: 'Commit' },
  render: (args) => (
    <div style={sidebar}>
      <PanelHeader
        {...args}
        actions={
          <>
            <IconButton size="sm" variant="ghost" aria-label="Refresh"><RefreshCw /></IconButton>
            <IconButton size="sm" variant="ghost" aria-label="Fetch"><ArrowDownToLine /></IconButton>
            <IconButton size="sm" variant="ghost" aria-label="Push"><Upload /></IconButton>
          </>
        }
      >
        <GitBranch style={{ width: 12, height: 12, verticalAlign: -2 }} /> main → origin/main
      </PanelHeader>
    </div>
  ),
}

/** A badge or a small Segment as the trailing content. */
export const OtherActions: Story = {
  args: { title: 'Usage' },
  render: function Render(args) {
    const [range, setRange] = useState('7d')
    return (
      <div className="sb-stack" style={{ gap: 12 }}>
        <div style={sidebar}>
          <PanelHeader title="Agents" actions={<Badge color="orange" badgeStyle="light" size="sm">2</Badge>} />
        </div>
        <div style={sidebar}>
          <PanelHeader
            {...args}
            actions={
              <Segment
                size="sm"
                shape="rounded"
                aria-label="Range"
                value={range}
                onValueChange={setRange}
                options={[
                  { value: '1d', label: '1d' },
                  { value: '7d', label: '7d' },
                  { value: '30d', label: '30d' },
                ]}
              />
            }
          />
        </div>
      </div>
    )
  },
}
