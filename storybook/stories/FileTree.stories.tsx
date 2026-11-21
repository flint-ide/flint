import type { Meta, StoryObj } from '@storybook/react-vite'
import { FileTree, type FileTreeNode } from '@wunderui/react'
import { useState } from 'react'

const REPO: FileTreeNode[] = [
  {
    id: 'cmd',
    name: 'cmd',
    children: [
      { id: 'cmd/flint', name: 'flint', children: [{ id: 'cmd/flint/main.go', name: 'main.go' }] },
      { id: 'cmd/wuicss', name: 'wuicss', children: [{ id: 'cmd/wuicss/main.go', name: 'main.go' }] },
    ],
  },
  {
    id: 'internal',
    name: 'internal',
    children: [
      {
        id: 'internal/review',
        name: 'review',
        children: [
          { id: 'internal/review/drafts.go', name: 'drafts.go' },
          { id: 'internal/review/drafts_test.go', name: 'drafts_test.go' },
        ],
      },
      { id: 'internal/agent', name: 'agent', children: [{ id: 'internal/agent/session.go', name: 'session.go' }] },
    ],
  },
  {
    id: 'frontend',
    name: 'frontend',
    children: [
      {
        id: 'frontend/src',
        name: 'src',
        children: [
          { id: 'frontend/src/main.tsx', name: 'main.tsx' },
          { id: 'frontend/src/boot.ts', name: 'boot.ts' },
          { id: 'frontend/src/store', name: 'store', children: [{ id: 'frontend/src/store/settings.ts', name: 'settings.ts' }] },
        ],
      },
      { id: 'frontend/package.json', name: 'package.json' },
    ],
  },
  { id: 'go.mod', name: 'go.mod' },
  { id: 'Taskfile.yml', name: 'Taskfile.yml' },
  { id: 'README.md', name: 'README.md' },
]

const meta = {
  title: 'Data Display/File Tree',
  component: FileTree,
  args: { data: REPO, 'aria-label': 'Repository files' },
  argTypes: {
    data: { control: false },
    selectedId: { control: 'text' },
  },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 320 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof FileTree>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

function ControlledTree() {
  const [selected, setSelected] = useState<FileTreeNode | undefined>(undefined)
  return (
    <div className="sb-stack">
      <FileTree
        data={REPO}
        aria-label="Repository files"
        selectedId={selected?.id ?? 'internal/review/drafts.go'}
        onSelect={setSelected}
      />
      <span className="sb-label" style={{ width: 'auto' }}>
        selected: {selected?.id ?? 'internal/review/drafts.go'}
      </span>
    </div>
  )
}

export const Controlled: Story = {
  render: () => <ControlledTree />,
}

export const Flat: Story = {
  args: {
    'aria-label': 'Changed files',
    data: [
      { id: 'a', name: 'frontend/src/style.css' },
      { id: 'b', name: 'frontend/src/store/settings.ts' },
      { id: 'c', name: '.github/workflows/pages.yml' },
      { id: 'd', name: 'docs/DEVELOPMENT.md' },
    ],
  },
}
