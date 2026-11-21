import type { Meta, StoryObj } from '@storybook/react-vite'
import { useRef, useState } from 'react'
import { FloatingToc, type TocItem } from '@wunderui/react'

const ITEMS: TocItem[] = [
  { id: 'toc-editor', label: 'Editor' },
  { id: 'toc-font', label: 'Font', depth: 1 },
  { id: 'toc-formatting', label: 'Format on save', depth: 1 },
  { id: 'toc-terminal', label: 'Terminal' },
  { id: 'toc-git', label: 'Git' },
  { id: 'toc-agents', label: 'Agents' },
  { id: 'toc-lsp', label: 'Language servers' },
]

const BODY: Record<string, string> = {
  'toc-editor': 'Tab size, word wrap, minimap and how the editor restores tabs after a restart.',
  'toc-font': 'Font family and size for the editor; the terminal can use its own.',
  'toc-formatting': 'Run the formatter of the language server (gofmt, prettier, ruff) when a file is saved.',
  'toc-terminal': 'Default shell, scrollback length and whether a terminal opens with the project.',
  'toc-git': 'Auto fetch interval, default branch for new worktrees and how review drafts are stored.',
  'toc-agents': 'Claude Code and Codex: models, permission modes and whether project hooks are loaded.',
  'toc-lsp': 'gopls, rust-analyzer, pyright and typescript-language-server: paths and init options.',
}

type Args = { controlled: boolean }

/** A table of contents for a long page; its marker follows the section being read in the scroll container. */
const meta = {
  title: 'Navigation/Floating TOC',
  args: { controlled: false },
  argTypes: {
    controlled: { control: 'boolean', description: 'Drive activeId from onSelect instead of the built-in scroll spy' },
  },
  render: function Render({ controlled }) {
    const scroller = useRef<HTMLDivElement>(null)
    const [active, setActive] = useState(ITEMS[0].id)
    return (
      <div style={{ display: 'flex', gap: 24, height: 360 }}>
        <FloatingToc
          items={ITEMS}
          scrollContainer={scroller}
          activeId={controlled ? active : undefined}
          onSelect={setActive}
        />
        <div ref={scroller} style={{ flex: 1, overflowY: 'auto', paddingRight: 8 }}>
          {ITEMS.map((item) => (
            <section key={item.id} id={item.id} style={{ minHeight: 200 }}>
              <h3 style={{ margin: '0 0 8px', fontSize: item.depth ? 14 : 16 }}>{item.label}</h3>
              <p style={{ margin: 0, fontSize: 13, color: 'var(--fg-muted)' }}>{BODY[item.id]}</p>
            </section>
          ))}
        </div>
      </div>
    )
  },
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Controlled: Story = { args: { controlled: true } }
