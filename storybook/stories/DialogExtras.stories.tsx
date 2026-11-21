import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFeedback,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
  Button,
  Checkbox,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogProgress,
  DialogSurface,
  DialogTitle,
  Input,
} from '@wunderui/react'
import { Trash2 } from 'lucide-react'

type Args = { size: 'sm' | 'md' | 'lg'; appearance: 'surface' | 'plain'; showCloseButton: boolean }

const STEPS = ['Repository', 'Agent', 'Permissions', 'Review']

const FILES = [
  'cmd/flint/main.go',
  'internal/git/diff.go',
  'internal/git/status.go',
  'internal/review/draft.go',
  'frontend/src/main.tsx',
  'frontend/src/boot.ts',
  'frontend/src/store/settings.ts',
  'frontend/src/style.css',
  'storybook/.storybook/preview.tsx',
  'Taskfile.yml',
  'docs/DEVELOPMENT.md',
  '.github/workflows/pages.yml',
]

/**
 * Dialog parts beyond header and footer: `DialogSurface` (a static, in-page dialog panel),
 * `DialogBody` (with a scrolling variant), `DialogProgress` (wizard steps) and `AlertDialogFeedback`.
 */
const meta = {
  title: 'Overlays/Dialog Extras',
  args: { size: 'md', appearance: 'surface', showCloseButton: true },
  argTypes: {
    size: { control: 'inline-radio', options: ['sm', 'md', 'lg'] },
    appearance: { control: 'inline-radio', options: ['surface', 'plain'] },
  },
  render: ({ size, appearance, showCloseButton }) => (
    <DialogSurface size={size} appearance={appearance} showCloseButton={showCloseButton} onClose={() => {}}>
      <DialogHeader>
        <DialogTitle>Clone repository</DialogTitle>
        <DialogDescription>Flint opens the folder in a new window when the clone finishes.</DialogDescription>
      </DialogHeader>
      <DialogBody>
        <Input aria-label="Repository URL" defaultValue="https://github.com/flint-ide/flint.git" />
        <Checkbox label="Open in a new window" defaultChecked />
      </DialogBody>
      <DialogFooter>
        <Button variant="plain">Cancel</Button>
        <Button>Clone</Button>
      </DialogFooter>
    </DialogSurface>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** The three surface widths: 384, 512 and 720 px. */
export const SurfaceSizes: Story = {
  name: 'Surface sizes',
  render: () => (
    <div className="sb-stack" style={{ gap: 24 }}>
      {(['sm', 'md', 'lg'] as const).map((size) => (
        <DialogSurface key={size} size={size} showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>size="{size}"</DialogTitle>
            <DialogDescription>Static dialog panel, e.g. for onboarding or docs.</DialogDescription>
          </DialogHeader>
        </DialogSurface>
      ))}
    </div>
  ),
}

/** `DialogBody scrollable`: the header and footer stay put while the file list scrolls. */
export const ScrollableBody: Story = {
  name: 'Scrollable body',
  render: () => (
    <DialogSurface size="md" onClose={() => {}}>
      <DialogHeader>
        <DialogTitle>Commit 12 files</DialogTitle>
        <DialogDescription>feat: Storybook for WunderUI</DialogDescription>
      </DialogHeader>
      <DialogBody scrollable aria-label="Files in this commit">
        {FILES.map((file) => (
          <Checkbox key={file} label={<code style={{ fontSize: 12 }}>{file}</code>} defaultChecked />
        ))}
      </DialogBody>
      <DialogFooter>
        <Button variant="plain">Cancel</Button>
        <Button>Commit</Button>
      </DialogFooter>
    </DialogSurface>
  ),
}

/** `DialogProgress` in a step-by-step wizard. */
export const Wizard: Story = {
  render: function Render() {
    const [step, setStep] = useState(2)
    return (
      <DialogSurface size="md" onClose={() => {}}>
        <DialogProgress step={step} steps={STEPS.length} label={STEPS[step - 1]} />
        <DialogHeader>
          <DialogTitle>Set up an agent</DialogTitle>
          <DialogDescription>Choose which agent runs in this workspace and what it may touch.</DialogDescription>
        </DialogHeader>
        <DialogFooter
          start={
            <Button variant="plain" disabled={step === 1} onClick={() => setStep((s) => s - 1)}>
              Back
            </Button>
          }
        >
          <Button onClick={() => setStep((s) => Math.min(STEPS.length, s + 1))}>
            {step === STEPS.length ? 'Finish' : 'Next'}
          </Button>
        </DialogFooter>
      </DialogSurface>
    )
  },
}

/** `AlertDialogFeedback` explains why a destructive action failed or what it will cost. */
export const AlertFeedback: Story = {
  name: 'AlertDialog feedback',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 440 }}>
      <AlertDialogFeedback tone="error">Could not delete the branch: it is protected on GitHub.</AlertDialogFeedback>
      <AlertDialogFeedback tone="warning">3 unpushed commits on feat/ui-kit will be lost.</AlertDialogFeedback>
      <AlertDialogFeedback tone="warning" icon={<Trash2 />}>
        The worktree folder is removed from disk.
      </AlertDialogFeedback>
      <AlertDialogFeedback tone="error" icon={null}>
        Without an icon.
      </AlertDialogFeedback>
    </div>
  ),
}

/** The feedback inside a real alert dialog, open on load. */
export const InAlertDialog: Story = {
  name: 'In an AlertDialog',
  render: () => (
    <div style={{ minHeight: 320 }}>
      <AlertDialog defaultOpen>
        <AlertDialogTrigger render={<Button variant="destructive" />}>Delete branch…</AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete feat/ui-kit?</AlertDialogTitle>
            <AlertDialogDescription>The local branch and its worktree are removed.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFeedback tone="warning">3 commits have not been pushed to origin.</AlertDialogFeedback>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive">Delete anyway</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  ),
}
