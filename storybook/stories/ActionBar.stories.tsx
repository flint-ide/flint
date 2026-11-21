import type { Meta, StoryObj } from '@storybook/react-vite'
import { ActionBar, Button, IconButton } from '@wunderui/react'
import { GitCommitHorizontal, Plus, Trash2, Undo2, X } from 'lucide-react'

type Args = { label: string }

/** A floating pill of bulk actions for a selection, e.g. files picked in the Changes view. */
const meta = {
  title: 'Navigation/Action Bar',
  args: { label: '3 files selected' },
  argTypes: {
    label: { control: 'text' },
  },
  render: ({ label }) => (
    <ActionBar label={label}>
      <Button size="sm" variant="ghost">
        <Plus data-icon="inline-start" /> Stage
      </Button>
      <Button size="sm" variant="ghost">
        <Undo2 data-icon="inline-start" /> Discard
      </Button>
      <Button size="sm">
        <GitCommitHorizontal data-icon="inline-start" /> Commit
      </Button>
    </ActionBar>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Without a label: icon buttons only. */
export const IconsOnly: Story = {
  render: () => (
    <ActionBar>
      <IconButton size="sm" variant="ghost" aria-label="Stage">
        <Plus />
      </IconButton>
      <IconButton size="sm" variant="ghost" aria-label="Discard">
        <Undo2 />
      </IconButton>
      <IconButton size="sm" variant="ghost" aria-label="Delete">
        <Trash2 />
      </IconButton>
      <IconButton size="sm" variant="ghost" aria-label="Clear selection">
        <X />
      </IconButton>
    </ActionBar>
  ),
}

/** Pinned to the bottom of a list, the way it floats over the Changes view. */
export const OverList: Story = {
  render: () => (
    <div
      style={{
        position: 'relative',
        height: 260,
        maxWidth: 480,
        border: '1px solid var(--border, transparent)',
        borderRadius: 8,
        padding: 12,
        font: '13px var(--font-mono)',
      }}
    >
      {['M internal/git/diff.go', 'M internal/git/status.go', 'A internal/git/diff_test.go', 'M frontend/src/main.tsx'].map(
        (line) => (
          <div key={line} style={{ padding: '4px 0' }}>
            {line}
          </div>
        ),
      )}
      <div style={{ position: 'absolute', left: 0, right: 0, bottom: 16, display: 'flex', justifyContent: 'center' }}>
        <ActionBar label="3 files selected">
          <Button size="sm" variant="ghost">
            Stage
          </Button>
          <Button size="sm" variant="destructive">
            Discard
          </Button>
        </ActionBar>
      </div>
    </div>
  ),
}
