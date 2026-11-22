import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, IconButton, Input, ToolbarButton, ToolbarSeparator, ViewToolbar } from '@flint-ide/ui'
import { Play, RefreshCw, Square } from '@flint-ide/ui/icons'

/** ViewToolbar: the flat bar over an editor view, on WunderUI's Toolbar (ui/src/ViewToolbar.tsx). */
const meta = { title: 'Flint/View Toolbar', component: ViewToolbar } satisfies Meta<typeof ViewToolbar>

export default meta
type Story = StoryObj<typeof meta>

const editor = { width: 640, border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden', background: 'var(--bg-editor)' } as const

/** ToolbarButtons rendered as kit buttons get arrow-key focus; other controls stay normal tab stops. */
export const QueryConsole: Story = {
  args: { 'aria-label': 'Query' },
  render: (args) => (
    <div style={editor}>
      <ViewToolbar {...args}>
        <ToolbarButton render={<Button size="sm" variant="primary" />}>
          <Play /> Run
        </ToolbarButton>
        <ToolbarButton render={<IconButton size="sm" variant="ghost" aria-label="Stop" />}>
          <Square />
        </ToolbarButton>
        <ToolbarSeparator />
        <span className="fui-muted">postgres · local</span>
        <span className="fui-grow" />
        <Input size="sm" placeholder="Filter rows" aria-label="Filter rows" style={{ width: 160 }} />
        <ToolbarButton render={<IconButton size="sm" variant="ghost" aria-label="Refresh" />}>
          <RefreshCw />
        </ToolbarButton>
      </ViewToolbar>
      <div style={{ height: 120 }} />
    </div>
  ),
}
