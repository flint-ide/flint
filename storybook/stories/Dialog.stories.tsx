import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  Button,
  Checkbox,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogIcon,
  DialogTitle,
  DialogTrigger,
  Field,
  FieldControl,
  FieldDescription,
  FieldLabel,
  Input,
} from '@wunderui/react'
import { GitBranch, ShieldAlert } from 'lucide-react'

type Args = { title: string; description: string; showCloseButton: boolean; defaultOpen: boolean }

/** A modal window: Dialog with a header (title + description), a body and a footer of actions. */
const meta = {
  title: 'Overlays/Dialog',
  args: {
    title: 'Rename run configuration',
    description: 'The name shows in the Run menu and in the title of its terminal.',
    showCloseButton: true,
    defaultOpen: false,
  },
  render: ({ title, description, showCloseButton, defaultOpen }) => (
    <div style={{ minHeight: 360 }}>
      <Dialog defaultOpen={defaultOpen}>
        <DialogTrigger render={<Button variant="plain" />}>Rename…</DialogTrigger>
        <DialogContent showCloseButton={showCloseButton}>
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          <Input aria-label="Name" defaultValue="go test ./internal/..." />
          <DialogFooter>
            <DialogClose render={<Button variant="plain" />}>Cancel</DialogClose>
            <DialogClose render={<Button />}>Rename</DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Rendered open so it shows up in screenshots. */
export const Open: Story = { args: { defaultOpen: true } }

/** A form inside the dialog: create a branch from the current HEAD. */
export const WithForm: Story = {
  render: () => (
    <div style={{ minHeight: 420 }}>
      <Dialog defaultOpen>
        <DialogTrigger render={<Button variant="plain" />}>
          <GitBranch data-icon="inline-start" /> New Branch…
        </DialogTrigger>
        <DialogContent>
          <form
            className="sb-stack"
            onSubmit={(event) => {
              event.preventDefault()
            }}
          >
            <DialogHeader>
              <DialogTitle>Create branch</DialogTitle>
              <DialogDescription>Branches off main at 5541e62.</DialogDescription>
            </DialogHeader>
            <Field>
              <FieldLabel>Branch name</FieldLabel>
              <FieldControl render={<Input />} required defaultValue="feat/ui-kit" />
              <FieldDescription>Lowercase, words separated by dashes.</FieldDescription>
            </Field>
            <label className="sb-row" style={{ fontSize: 13 }}>
              <Checkbox defaultChecked /> Check out after creating
            </label>
            <DialogFooter>
              <DialogClose render={<Button variant="plain" />}>Cancel</DialogClose>
              <Button type="submit">Create Branch</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  ),
}

/** DialogIcon in the header, here warning about an untrusted folder. */
export const WithIcon: Story = {
  render: () => (
    <div style={{ minHeight: 360 }}>
      <Dialog defaultOpen>
        <DialogTrigger render={<Button variant="plain" />}>Open Folder…</DialogTrigger>
        <DialogContent showCloseButton={false}>
          <DialogHeader>
            <DialogIcon variant="warning">
              <ShieldAlert />
            </DialogIcon>
            <DialogTitle>Do you trust the authors of this folder?</DialogTitle>
            <DialogDescription>
              Trusting ~/projects/flint lets Flint start its language servers, run its tasks and load the agents' project
              hooks.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose render={<Button variant="plain" />}>Restricted Mode</DialogClose>
            <DialogClose render={<Button />}>Trust Folder</DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  ),
}
