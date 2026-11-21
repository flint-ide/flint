import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@wunderui/react'

type Args = { label: string; defaultOpen: boolean; disabled: boolean }

const mono: React.CSSProperties = { font: '12px var(--font-mono)', color: 'var(--fg-muted)' }

const STAGED = ['internal/git/diff.go', 'internal/git/status.go', 'internal/git/diff_test.go']

/** One section that expands and collapses, e.g. a group in the Changes view. */
const meta = {
  title: 'Layout/Collapsible',
  args: { label: 'Staged Changes (3)', defaultOpen: true, disabled: false },
  render: ({ label, defaultOpen, disabled }) => (
    <div style={{ maxWidth: 420 }}>
      <Collapsible defaultOpen={defaultOpen} disabled={disabled}>
        <CollapsibleTrigger>{label}</CollapsibleTrigger>
        <CollapsibleContent>
          <div className="sb-stack" style={{ gap: 4, paddingTop: 8 }}>
            {STAGED.map((file) => (
              <div key={file} style={mono}>
                M {file}
              </div>
            ))}
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Closed: Story = { args: { defaultOpen: false } }

/** Controlled: the open state lives in the story, shown under the panel. */
export const Controlled: Story = {
  render: function Render() {
    const [open, setOpen] = useState(false)
    return (
      <div className="sb-stack" style={{ maxWidth: 420 }}>
        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger>Run configuration: go test ./...</CollapsibleTrigger>
          <CollapsibleContent>
            <div className="sb-stack" style={{ gap: 4, paddingTop: 8 }}>
              <div style={mono}>cwd: ~/projects/flint</div>
              <div style={mono}>env: GOFLAGS=-count=1</div>
              <div style={mono}>before: task generate</div>
            </div>
          </CollapsibleContent>
        </Collapsible>
        <span className="sb-label">open: {String(open)}</span>
      </div>
    )
  },
}

/** Several sections stacked, like the Source Control side bar. */
export const Stacked: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      {[
        { label: 'Staged Changes (3)', files: STAGED, open: true },
        { label: 'Changes (2)', files: ['frontend/src/main.tsx', 'Taskfile.yml'], open: true },
        { label: 'Untracked (1)', files: ['storybook/'], open: false },
      ].map((group) => (
        <Collapsible key={group.label} defaultOpen={group.open}>
          <CollapsibleTrigger>{group.label}</CollapsibleTrigger>
          <CollapsibleContent>
            <div className="sb-stack" style={{ gap: 4, paddingTop: 8 }}>
              {group.files.map((file) => (
                <div key={file} style={mono}>
                  {file}
                </div>
              ))}
            </div>
          </CollapsibleContent>
        </Collapsible>
      ))}
    </div>
  ),
}
