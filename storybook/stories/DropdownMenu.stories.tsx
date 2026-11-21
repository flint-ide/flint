import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Avatar,
  AvatarFallback,
  Button,
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuEmpty,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuLoading,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
  IconButton,
} from '@wunderui/react'
import { ChevronDown, Copy, FilePlus, FolderOpen, GitBranch, MoreHorizontal, Scissors, Settings, Trash2 } from 'lucide-react'

type Args = { defaultOpen: boolean; align: 'start' | 'center' | 'end'; side: 'top' | 'bottom' | 'left' | 'right' }

/** WunderUI's DropdownMenu (Base UI Menu): a menu of actions that closes after one is picked. */
const meta = {
  title: 'Overlays/Dropdown Menu',
  args: { defaultOpen: true, align: 'start', side: 'bottom' },
  argTypes: {
    align: { control: 'inline-radio', options: ['start', 'center', 'end'] },
    side: { control: 'inline-radio', options: ['top', 'bottom', 'left', 'right'] },
  },
  decorators: [(Story) => <div style={{ minHeight: 380, padding: 8 }}><Story /></div>],
  render: ({ defaultOpen, align, side }) => (
    <DropdownMenu key={String(defaultOpen)} defaultOpen={defaultOpen}>
      <DropdownMenuTrigger render={<Button variant="plain" />}>
        File <ChevronDown data-icon="inline-end" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} side={side}>
        <DropdownMenuItem icon={<FilePlus />} aria-keyshortcuts="Meta+N">
          New file <DropdownMenuShortcut>⌘N</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem icon={<FolderOpen />} aria-keyshortcuts="Meta+O">
          Open folder… <DropdownMenuShortcut>⌘O</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>Open recent</DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuItem>~/projects/flint</DropdownMenuItem>
            <DropdownMenuItem>~/projects/poc-mailos</DropdownMenuItem>
            <DropdownMenuItem disabled>~/notes (missing)</DropdownMenuItem>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<Scissors />}>
          Cut <DropdownMenuShortcut>⌘X</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem icon={<Copy />}>
          Copy <DropdownMenuShortcut>⌘C</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<Settings />}>
          Settings <DropdownMenuShortcut>⌘,</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem variant="destructive" icon={<Trash2 />}>
          Delete file
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

/** Icons, shortcuts, a submenu, separators and a destructive entry. */
export const Default: Story = {}

/** Labelled groups, checkbox items (with a mixed "Show all") and a radio group. */
export const CheckboxAndRadio: Story = {
  render: function Render({ defaultOpen, align, side }) {
    const panels = ['Explorer', 'Source control', 'Terminal']
    const [visible, setVisible] = useState<string[]>(['Explorer', 'Terminal'])
    const [sort, setSort] = useState('name')
    const all = visible.length === panels.length
    const toggle = (p: string, on: boolean) => setVisible((v) => (on ? [...v, p] : v.filter((x) => x !== p)))
    return (
      <DropdownMenu defaultOpen={defaultOpen}>
        <DropdownMenuTrigger render={<Button variant="plain" />}>
          View <ChevronDown data-icon="inline-end" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align={align} side={side}>
          <DropdownMenuGroup>
            <DropdownMenuLabel>Panels</DropdownMenuLabel>
            <DropdownMenuCheckboxItem
              checked={all}
              indeterminate={visible.length > 0 && !all}
              onCheckedChange={(on) => setVisible(on ? panels : [])}
            >
              Show all
            </DropdownMenuCheckboxItem>
            {panels.map((p) => (
              <DropdownMenuCheckboxItem key={p} checked={visible.includes(p)} onCheckedChange={(on) => toggle(p, on)}>
                {p}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>Sort files by</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={sort} onValueChange={(v) => setSort(String(v))}>
              <DropdownMenuRadioItem value="name">Name</DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="modified">Last modified</DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="type">Type</DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    )
  },
}

/** Entries with a description line and avatars: switching the agent that runs a task. */
export const DescriptionsAndAvatars: Story = {
  render: ({ defaultOpen }) => (
    <DropdownMenu defaultOpen={defaultOpen}>
      <DropdownMenuTrigger render={<IconButton variant="plain" aria-label="Branch actions" />}>
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuLabel>Assign to</DropdownMenuLabel>
        <DropdownMenuItem avatar={<Avatar size="sm"><AvatarFallback>CL</AvatarFallback></Avatar>} description="Writes and runs code in a worktree">
          Coding agent
        </DropdownMenuItem>
        <DropdownMenuItem avatar={<Avatar size="sm"><AvatarFallback>RV</AvatarFallback></Avatar>} description="Reviews the diff against main">
          Review agent
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<GitBranch />} description="Creates feat/ui-kit-2 from HEAD" inset={false}>
          New branch from here
        </DropdownMenuItem>
        <DropdownMenuItem inset>Copy branch name</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  ),
}

/** DropdownMenuLoading while entries load (aria-busy on the content) and DropdownMenuEmpty when there are none. */
export const LoadingAndEmpty: Story = {
  render: ({ defaultOpen }) => (
    <div className="sb-row" style={{ alignItems: 'flex-start', gap: 200 }}>
      <DropdownMenu defaultOpen={defaultOpen}>
        <DropdownMenuTrigger render={<Button variant="plain" />}>
          Remote branches <ChevronDown data-icon="inline-end" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" aria-busy>
          <DropdownMenuLoading>Fetching origin…</DropdownMenuLoading>
        </DropdownMenuContent>
      </DropdownMenu>
      <DropdownMenu defaultOpen={defaultOpen}>
        <DropdownMenuTrigger render={<Button variant="plain" />}>
          Stashes <ChevronDown data-icon="inline-end" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuEmpty>No stashes in this repository</DropdownMenuEmpty>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  ),
}
