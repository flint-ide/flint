import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Checkbox, IconButton, TreeRow, TreeSection } from '@flint-ide/ui'
import { ExternalLink, File, Folder, FolderOpen, Undo2 } from '@flint-ide/ui/icons'

/**
 * TreeRow and TreeSection: the 22 px rows of every sidebar list and tree (ui/src/TreeRow.tsx). A numeric `badge` or
 * `count` is the kit's quiet Count (a small muted number, ui/src/Count.tsx), not a pill.
 */
const meta = { title: 'Flint/Tree Row', component: TreeRow } satisfies Meta<typeof TreeRow>

export default meta
type Story = StoryObj<typeof meta>

const sidebar = { width: 300, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-sidebar)', padding: '4px 0', fontSize: 13 } as const
const icon = { width: 14, height: 14 } as const

/** Props table playground: one row. */
export const Playground: Story = {
  args: { label: 'style.css', hint: 'frontend/src', badge: 3, depth: 1, twisty: 'none', file: true },
  render: (args) => (
    <div style={sidebar}>
      <TreeRow {...args} icon={<File style={icon} />} />
    </div>
  ),
}

const files = [
  { name: 'frontend', depth: 0, dir: true, open: true },
  { name: 'src', depth: 1, dir: true, open: true },
  { name: 'App.tsx', depth: 2 },
  { name: 'style.css', depth: 2 },
  { name: 'ui', depth: 0, dir: true, open: false },
  { name: 'go.mod', depth: 0 },
]

/** A tree in normal flow: depth, twisty, icon, the active editor's row and a selection. Click to select. */
export const Tree: Story = {
  args: { label: '' },
  render: () => {
    const [selected, setSelected] = useState('src')
    return (
      <div style={sidebar} tabIndex={0}>
        {files.map((f) => (
          <TreeRow
            key={f.name}
            depth={f.depth}
            twisty={f.dir ? (f.open ? 'open' : 'closed') : 'none'}
            icon={f.dir ? (f.open ? <FolderOpen style={icon} /> : <Folder style={icon} />) : <File style={icon} />}
            label={f.name}
            file={!f.dir}
            active={f.name === 'App.tsx'}
            selected={f.name === selected}
            onClick={() => setSelected(f.name)}
          />
        ))}
      </div>
    )
  },
}

/** Virtualised: rows are absolutely positioned in a sized box and moved with transform (lib/virtualList). */
export const Virtualised: Story = {
  args: { label: '' },
  render: () => (
    <div style={{ ...sidebar, height: 160, overflow: 'auto' }}>
      <div style={{ position: 'relative', height: 200 * 22 }}>
        {Array.from({ length: 12 }, (_, i) => (
          <TreeRow
            key={i}
            virtual
            style={{ transform: `translateY(${i * 22}px)` }}
            twisty="none"
            icon={<File style={icon} />}
            label={`file-${i}.ts`}
            file
            hint="src/generated"
          />
        ))}
      </div>
    </div>
  ),
}

const changes = [
  { name: 'TreeRow.tsx', dir: 'ui/src' },
  { name: 'components.css', dir: 'ui/css' },
  { name: 'README.md', dir: '' },
]

/** Collapsible groups with a tick-all checkbox, counts and hover actions (the Commit panel). */
export const Sections: Story = {
  args: { label: '' },
  render: () => {
    const [ticked, setTicked] = useState<string[]>(['TreeRow.tsx'])
    const tick = (name: string) => setTicked((t) => (t.includes(name) ? t.filter((n) => n !== name) : [...t, name]))
    return (
      <div style={sidebar}>
        <TreeSection
          title="Changes"
          count={changes.length}
          leading={
            <Checkbox
              aria-label="Include all changes"
              checked={ticked.length === changes.length}
              indeterminate={ticked.length > 0 && ticked.length < changes.length}
              onCheckedChange={(on) => setTicked(on ? changes.map((c) => c.name) : [])}
            />
          }
        >
          {changes.map((c) => (
            <TreeRow
              key={c.name}
              depth={1}
              leading={<Checkbox aria-label={`Include ${c.name}`} checked={ticked.includes(c.name)} onCheckedChange={() => tick(c.name)} />}
              label={c.name}
              file
              hint={c.dir}
              actions={
                <>
                  <IconButton size="sm" variant="ghost" aria-label="Open file"><ExternalLink /></IconButton>
                  <IconButton size="sm" variant="ghost" aria-label="Rollback"><Undo2 /></IconButton>
                </>
              }
            />
          ))}
        </TreeSection>
        <TreeSection
          title="Review"
          count="2/3"
          actionsVisible="always"
          actions={<IconButton size="sm" variant="ghost" aria-label="Clear the review"><Undo2 /></IconButton>}
        >
          <TreeRow depth={1} status={<span className="fui-mono">new</span>} label="Agent finding" hint="status slot" />
          <TreeRow depth={1} current label="/review" hint="current: bright without focus" />
        </TreeSection>
        <TreeSection title="Stashes" count={2} defaultOpen={false}>
          <TreeRow depth={1} label="wip (main)" hint="2 hours ago" />
          <TreeRow depth={1} label="spike (ui)" hint="yesterday" />
        </TreeSection>
      </div>
    )
  },
}

/** A workspace root (small caps) and search-style hints with the match in <b>. */
export const RootAndHints: Story = {
  args: { label: '' },
  render: () => (
    <div style={sidebar}>
      <TreeRow emphasis="root" twisty="open" icon={<FolderOpen style={icon} />} label="flint" />
      <TreeRow depth={1} twisty="open" icon={<File style={icon} />} label="TreeRow.tsx" file hint="ui/src" badge={2} />
      <TreeRow depth={2} label={<span className="fui-mono">12</span>} hint={<>export function <b>TreeRow</b>(…)</>} />
    </div>
  ),
}
