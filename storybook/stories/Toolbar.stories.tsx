import type { Meta, StoryObj } from '@storybook/react-vite'
import { Toolbar, ToolbarButton, ToolbarGroup, ToolbarLink, ToolbarSeparator } from '@wunderui/react'
import {
  Bug,
  Columns2,
  GitCommitHorizontal,
  GitPullRequest,
  Play,
  RefreshCw,
  Rows2,
  Square,
  StepForward,
  Upload,
} from 'lucide-react'

type Args = { orientation: 'horizontal' | 'vertical'; loop: boolean; disabled: boolean }

/** A row of icon buttons with roving focus: Tab enters it once, the arrow keys move between buttons. */
const meta = {
  title: 'Navigation/Toolbar',
  args: { orientation: 'horizontal', loop: true, disabled: false },
  argTypes: {
    orientation: { control: 'inline-radio', options: ['horizontal', 'vertical'] },
  },
  render: ({ orientation, loop, disabled }) => (
    <div style={{ display: 'inline-flex' }}>
      <Toolbar
        aria-label="Run"
        orientation={orientation}
        loopFocus={loop}
        disabled={disabled}
        style={orientation === 'vertical' ? { flexDirection: 'column' } : undefined}
      >
        <ToolbarGroup aria-label="Run configuration" style={orientation === 'vertical' ? { flexDirection: 'column' } : undefined}>
          <ToolbarButton aria-label="Run">
            <Play />
          </ToolbarButton>
          <ToolbarButton aria-label="Debug">
            <Bug />
          </ToolbarButton>
          <ToolbarButton aria-label="Step over">
            <StepForward />
          </ToolbarButton>
          <ToolbarButton aria-label="Stop" disabled>
            <Square />
          </ToolbarButton>
        </ToolbarGroup>
        <ToolbarSeparator orientation={orientation === 'horizontal' ? 'vertical' : 'horizontal'} />
        <ToolbarGroup aria-label="Layout" style={orientation === 'vertical' ? { flexDirection: 'column' } : undefined}>
          <ToolbarButton aria-label="Split right">
            <Columns2 />
          </ToolbarButton>
          <ToolbarButton aria-label="Split down">
            <Rows2 />
          </ToolbarButton>
        </ToolbarGroup>
      </Toolbar>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Vertical: Story = { args: { orientation: 'vertical' } }

/** Source control toolbar with a trailing link to the pull request. */
export const WithLink: Story = {
  render: () => (
    <div style={{ display: 'inline-flex' }}>
      <Toolbar aria-label="Source control">
        <ToolbarGroup aria-label="Git">
          <ToolbarButton aria-label="Commit">
            <GitCommitHorizontal />
          </ToolbarButton>
          <ToolbarButton aria-label="Push">
            <Upload />
          </ToolbarButton>
          <ToolbarButton aria-label="Refresh">
            <RefreshCw />
          </ToolbarButton>
        </ToolbarGroup>
        <ToolbarSeparator />
        <ToolbarLink href="#" style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '0 8px' }}>
          <GitPullRequest size={14} /> #77 feat/ui-kit
        </ToolbarLink>
      </Toolbar>
    </div>
  ),
}
