import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, IconButton, Input, Popover, PopoverClose, PopoverContent, PopoverTrigger } from '@wunderui/react'
import { GitBranch, X } from 'lucide-react'

const SIDES = ['top', 'right', 'bottom', 'left'] as const
const ALIGNS = ['start', 'center', 'end'] as const

type Args = { side: (typeof SIDES)[number]; align: (typeof ALIGNS)[number]; sideOffset: number; defaultOpen: boolean }

/** Floating content anchored to a trigger; closes on outside click or Escape. */
const meta = {
  title: 'Overlays/Popover',
  args: { side: 'bottom', align: 'start', sideOffset: 8, defaultOpen: false },
  argTypes: {
    side: { control: 'inline-radio', options: SIDES },
    align: { control: 'inline-radio', options: ALIGNS },
    sideOffset: { control: { type: 'range', min: 0, max: 32, step: 2 } },
  },
  render: ({ side, align, sideOffset, defaultOpen }) => (
    <div style={{ minHeight: 360, display: 'grid', placeItems: 'center' }}>
      <Popover defaultOpen={defaultOpen}>
        <PopoverTrigger render={<Button variant="plain" />}>
          <GitBranch data-icon="inline-start" /> feat/ui-kit
        </PopoverTrigger>
        <PopoverContent side={side} align={align} sideOffset={sideOffset} style={{ width: 280 }}>
          <div className="sb-stack">
            <div style={{ fontWeight: 600 }}>feat/ui-kit</div>
            <div style={{ color: 'var(--fg-muted)', fontSize: 12 }}>4 ahead, 0 behind origin/main · last commit 2 hours ago</div>
            <div className="sb-row">
              <PopoverClose render={<Button size="sm" variant="plain" />}>Pull</PopoverClose>
              <PopoverClose render={<Button size="sm" />}>Push</PopoverClose>
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Rendered open so it shows up in screenshots. */
export const Open: Story = { args: { defaultOpen: true } }

/** A small form with an × close button: rename a terminal tab. */
export const WithForm: Story = {
  render: () => (
    <div style={{ minHeight: 360 }}>
      <Popover defaultOpen>
        <PopoverTrigger render={<Button variant="plain" />}>zsh — flint</PopoverTrigger>
        <PopoverContent align="start" style={{ width: 300 }}>
          <div className="sb-stack">
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <span style={{ fontWeight: 600 }}>Rename terminal</span>
              <PopoverClose render={<IconButton size="sm" variant="ghost" aria-label="Close" />}>
                <X />
              </PopoverClose>
            </div>
            <Input aria-label="Terminal name" defaultValue="zsh — flint" />
            <div className="sb-row" style={{ justifyContent: 'flex-end' }}>
              <PopoverClose render={<Button size="sm" variant="plain" />}>Cancel</PopoverClose>
              <PopoverClose render={<Button size="sm" />}>Rename</PopoverClose>
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  ),
}
