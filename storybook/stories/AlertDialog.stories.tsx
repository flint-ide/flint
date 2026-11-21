import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogIcon,
  AlertDialogTitle,
  AlertDialogTrigger,
  Button,
} from '@wunderui/react'
import { Trash2, Undo2 } from 'lucide-react'

type Args = {
  title: string
  description: string
  action: string
  variant: 'primary' | 'destructive'
  defaultOpen: boolean
}

/** A confirmation that blocks the window until answered: it has no close button and ignores outside clicks. */
const meta = {
  title: 'Overlays/Alert Dialog',
  args: {
    title: 'Discard 3 changes?',
    description: 'The changes to diff.go, status.go and diff_test.go are lost. This cannot be undone.',
    action: 'Discard Changes',
    variant: 'destructive',
    defaultOpen: false,
  },
  argTypes: {
    variant: { control: 'inline-radio', options: ['primary', 'destructive'] },
  },
  render: ({ title, description, action, variant, defaultOpen }) => (
    <div style={{ minHeight: 360 }}>
      <AlertDialog defaultOpen={defaultOpen}>
        <AlertDialogTrigger render={<Button variant="plain" />}>
          <Undo2 data-icon="inline-start" /> Discard All
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{title}</AlertDialogTitle>
            <AlertDialogDescription>{description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant={variant}>{action}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Rendered open so it shows up in screenshots. */
export const Open: Story = { args: { defaultOpen: true } }

/** With an icon: deleting a branch that has unmerged commits. */
export const WithIcon: Story = {
  render: () => (
    <div style={{ minHeight: 360 }}>
      <AlertDialog defaultOpen>
        <AlertDialogTrigger render={<Button variant="destructive" />}>
          <Trash2 data-icon="inline-start" /> Delete Branch
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogIcon variant="destructive">
              <Trash2 />
            </AlertDialogIcon>
            <AlertDialogTitle>Delete feat/ui-kit?</AlertDialogTitle>
            <AlertDialogDescription>
              It has 4 commits that are not on main. Deleting it leaves them reachable only through the reflog.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep Branch</AlertDialogCancel>
            <AlertDialogAction variant="destructive">Delete Anyway</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  ),
}

/** A non-destructive confirmation uses the primary action. */
export const Confirm: Story = {
  render: () => (
    <div style={{ minHeight: 360 }}>
      <AlertDialog defaultOpen>
        <AlertDialogTrigger render={<Button variant="plain" />}>Stop Agent</AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Stop Claude Code?</AlertDialogTitle>
            <AlertDialogDescription>
              The agent is editing 2 files. Its changes so far stay in the working tree.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep Running</AlertDialogCancel>
            <AlertDialogAction variant="primary">Stop</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  ),
}
