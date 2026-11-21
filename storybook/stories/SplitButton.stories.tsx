import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { DropdownMenuItem, DropdownMenuSeparator, DropdownMenuShortcut, SplitButton, type SplitButtonSize } from '@wunderui/react'
import { Bug, GitMerge, Play, Save, Upload } from 'lucide-react'

const SIZES: SplitButtonSize[] = ['sm', 'md', 'lg', 'xl', '2xl']

const RUN_MENU = (
  <>
    <DropdownMenuItem icon={<Bug />}>
      Debug <DropdownMenuShortcut>⇧F5</DropdownMenuShortcut>
    </DropdownMenuItem>
    <DropdownMenuItem>Run with coverage</DropdownMenuItem>
    <DropdownMenuItem>Run tests in current file</DropdownMenuItem>
    <DropdownMenuSeparator />
    <DropdownMenuItem>Edit run configurations…</DropdownMenuItem>
  </>
)

type Args = { variant: 'plain' | 'solid'; size: SplitButtonSize; shape: 'rounded' | 'pill'; disabled: boolean; label: string }

/** An action part plus a chevron that opens a menu with the alternatives. */
const meta = {
  title: 'Actions/Split Button',
  args: { variant: 'plain', size: 'md', shape: 'rounded', disabled: false, label: 'Run' },
  argTypes: {
    variant: { control: 'inline-radio', options: ['plain', 'solid'] },
    size: { control: 'inline-radio', options: SIZES },
    shape: { control: 'inline-radio', options: ['rounded', 'pill'] },
  },
  render: ({ label, ...args }) => (
    <SplitButton {...args} icon={<Play />} menuLabel="More run options" menu={RUN_MENU}>
      {label}
    </SplitButton>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {}

export const Solid: Story = { args: { variant: 'solid' } }

export const Variants: Story = {
  render: ({ size, shape }) => (
    <div className="sb-stack">
      {(['plain', 'solid'] as const).map((variant) => (
        <div key={variant} className="sb-row">
          <span className="sb-label">{variant}</span>
          <SplitButton variant={variant} size={size} shape={shape} icon={<Play />} menu={RUN_MENU}>
            Run
          </SplitButton>
          <SplitButton variant={variant} size={size} shape={shape} icon={<Play />} menu={RUN_MENU} disabled>
            Run
          </SplitButton>
        </div>
      ))}
    </div>
  ),
}

export const Sizes: Story = {
  render: ({ variant, shape }) => (
    <div className="sb-row">
      {SIZES.map((size) => (
        <SplitButton key={size} variant={variant} size={size} shape={shape} menu={RUN_MENU}>
          {size}
        </SplitButton>
      ))}
    </div>
  ),
}

/** Merge a pull request: the action runs the last chosen strategy, the menu switches it. */
export const MergeStrategy: Story = {
  render: function Render({ size, shape }) {
    const strategies = ['Create a merge commit', 'Squash and merge', 'Rebase and merge']
    const [strategy, setStrategy] = useState(strategies[1])
    const [merged, setMerged] = useState(0)
    return (
      <div className="sb-stack">
        <SplitButton
          variant="solid"
          size={size}
          shape={shape}
          icon={<GitMerge />}
          onAction={() => setMerged((n) => n + 1)}
          menuLabel="Merge strategy"
          menu={strategies.map((s) => (
            <DropdownMenuItem key={s} onClick={() => setStrategy(s)}>
              {s}
            </DropdownMenuItem>
          ))}
        >
          {strategy}
        </SplitButton>
        <span className="sb-label">Merged {merged} time(s)</span>
      </div>
    )
  },
}

/** menuProps passes props to the popup, e.g. align="start"; defaultOpen shows the menu at once. */
export const OpenMenu: Story = {
  render: ({ size }) => (
    <div style={{ minHeight: 200 }}>
      <SplitButton
        size={size}
        icon={<Save />}
        defaultOpen
        menuProps={{ align: 'start' }}
        menu={
          <>
            <DropdownMenuItem icon={<Save />}>Save all</DropdownMenuItem>
            <DropdownMenuItem icon={<Upload />}>Save and push</DropdownMenuItem>
          </>
        }
      >
        Save
      </SplitButton>
    </div>
  ),
}
