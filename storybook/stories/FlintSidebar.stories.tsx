import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Badge,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  type ContextMenuItems,
  ContextMenuProvider,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
  IconButton,
  Button,
  useContextMenu,
} from '@flint-ide/ui'
import { Copy, MoreHorizontal, Trash2 } from '@flint-ide/ui/icons'

/** Sidebar building blocks from @flint-ide/ui: the shared context menu, dense mode, Badge on Flint's palette. */
const meta = { title: 'Flint/Sidebar' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const surface = { width: 280, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-sidebar)', padding: '4px 0' } as const
const row = { position: 'relative', display: 'flex', alignItems: 'center', gap: 6, height: 22, padding: '0 8px', fontSize: 13, cursor: 'default' } as const

const pods = ['api-7d9f', 'worker-5c2a', 'cron-11b0']

function PodRow({ name, onLog }: { name: string; onLog: (s: string) => void }) {
  const menu = useContextMenu()
  const items: ContextMenuItems = [
    { label: 'Copy name', icon: <Copy />, hint: '⌘C', onSelect: () => onLog(`copied ${name}`) },
    { label: 'Logs', onSelect: () => onLog(`logs ${name}`) },
    name.startsWith('cron') && { label: 'Run now', onSelect: () => onLog(`ran ${name}`) },
    'separator',
    { label: 'Delete pod', icon: <Trash2 />, danger: true, onSelect: () => onLog(`deleted ${name}`) },
  ]
  return (
    <div style={row} tabIndex={0} onContextMenu={menu.onContextMenu(items)}>
      <span style={{ flex: 1 }}>{name}</span>
      <IconButton variant="ghost" size="sm" aria-label={`Actions for ${name}`} onClick={menu.onOpenUnder(items)}>
        <MoreHorizontal />
      </IconButton>
    </div>
  )
}

/** Right-click a row (or focus it and press Shift+F10), or use its "more" button. One menu serves every row. */
export const ContextMenu: Story = {
  render: () => {
    const [log, setLog] = useState('—')
    return (
      <div>
        <ContextMenuProvider className="fui-dense">
          <div style={surface}>
            {pods.map((p) => (
              <PodRow key={p} name={p} onLog={setLog} />
            ))}
          </div>
        </ContextMenuProvider>
        <p style={{ fontSize: 12, color: 'var(--fg-muted)' }}>Last action: {log}</p>
      </div>
    )
  },
}

function Menu({ label, className }: { label: string; className?: string }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>{label}</DropdownMenuTrigger>
      <DropdownMenuContent className={className}>
        <DropdownMenuItem>
          Restart <DropdownMenuShortcut>⌘R</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem>Scale…</DropdownMenuItem>
        <DropdownMenuItem variant="destructive">Delete deployment</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** .fui-dense: 24 px menu and list rows, 12 px text, a tighter Collapsible. On <body> it reaches portals too. */
export const Dense: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 24, alignItems: 'flex-start' }}>
      <Menu label="Default menu" />
      <Menu label="Dense menu" className="fui-dense" />
      <div className="fui-dense" style={{ ...surface, padding: 8 }}>
        <Collapsible defaultOpen>
          <CollapsibleTrigger>Deployments</CollapsibleTrigger>
          <CollapsibleContent>api, worker, cron</CollapsibleContent>
        </Collapsible>
      </div>
    </div>
  ),
}

const colors = ['blue', 'green', 'red', 'yellow', 'orange'] as const
const styles = ['plain', 'light', 'light_border', 'border_color'] as const

/** Badge's blue, green, red, yellow and orange follow Flint's --info, --ok, --danger, --warn and --accent, in both themes. */
export const BadgePalette: Story = {
  render: () => (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, max-content)', gap: 8 }}>
      {styles.flatMap((s) =>
        colors.map((c) => (
          <Badge key={`${s}-${c}`} color={c} badgeStyle={s}>
            {c}
          </Badge>
        )),
      )}
    </div>
  ),
}
