import type { Meta, StoryObj } from '@storybook/react-vite'
import { useEffect, useRef, useState } from 'react'
import { Button, IconButton, type ButtonPhase, type IconButtonPhase } from '@wunderui/react'
import { Copy, GitCommitHorizontal, RefreshCw, Upload } from 'lucide-react'

const PHASES: ButtonPhase[] = ['idle', 'pending', 'success', 'error']
const PUSH_LABEL: Record<ButtonPhase, string> = { idle: 'Push', pending: 'Pushing…', success: 'Pushed', error: 'Retry push' }
const COPY_LABEL: Record<IconButtonPhase, string> = { idle: 'Copy commit SHA', pending: 'Copying', success: 'Copied', error: 'Retry copying' }

type Args = { phase: ButtonPhase; pending: boolean; variant: 'primary' | 'plain' | 'outline' | 'tint' }

/**
 * Async feedback on Button and IconButton: `phase` swaps the leading icon for a spinner, a check or a retry
 * icon; `pending` alone shows the spinner. The label is yours, so keep the width fixed.
 */
const meta = {
  title: 'Actions/Button Phases',
  args: { phase: 'idle', pending: false, variant: 'primary' },
  argTypes: {
    phase: { control: 'inline-radio', options: PHASES },
    variant: { control: 'inline-radio', options: ['primary', 'plain', 'outline', 'tint'] },
  },
  render: ({ phase, pending, variant }) => (
    <div className="sb-row">
      <Button variant={variant} phase={phase} pending={pending} style={{ width: 132 }}>
        <Upload data-icon="inline-start" /> {PUSH_LABEL[phase]}
      </Button>
      <IconButton variant={variant === 'tint' ? 'plain' : variant} phase={phase} pending={pending} aria-label={COPY_LABEL[phase]}>
        <Copy />
      </IconButton>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** All four phases side by side. */
export const AllPhases: Story = {
  render: ({ variant }) => (
    <div className="sb-stack">
      {PHASES.map((phase) => (
        <div key={phase} className="sb-row">
          <span className="sb-label">{phase}</span>
          <Button variant={variant} phase={phase} style={{ width: 132 }}>
            <Upload data-icon="inline-start" /> {PUSH_LABEL[phase]}
          </Button>
          <IconButton variant="plain" phase={phase} aria-label={COPY_LABEL[phase]}>
            <Copy />
          </IconButton>
          <IconButton variant="ghost" shape="circle" phase={phase} aria-label={COPY_LABEL[phase]}>
            <Copy />
          </IconButton>
        </div>
      ))}
    </div>
  ),
}

/** `pending` only: spinner, still focusable, colour kept (not disabled). */
export const Pending: Story = {
  render: () => (
    <div className="sb-row">
      <Button pending>
        <GitCommitHorizontal data-icon="inline-start" /> Committing…
      </Button>
      <Button variant="plain" pending>
        <RefreshCw data-icon="inline-start" /> Fetching
      </Button>
      <IconButton variant="plain" pending aria-label="Refreshing branches">
        <RefreshCw />
      </IconButton>
    </div>
  ),
}

function usePhaseCycle(fail: boolean) {
  const [phase, setPhase] = useState<ButtonPhase>('idle')
  const timers = useRef<number[]>([])
  useEffect(() => () => timers.current.forEach(clearTimeout), [])
  const run = () => {
    if (phase === 'pending') return
    setPhase('pending')
    timers.current.push(
      window.setTimeout(() => setPhase(fail ? 'error' : 'success'), 1200),
      window.setTimeout(() => setPhase('idle'), fail ? 4000 : 2600),
    )
  }
  return [phase, run] as const
}

/** Click to run a fake async action: pending, then success (or error), then back to idle. */
export const Interactive: Story = {
  render: function Render({ variant }) {
    const [pushPhase, push] = usePhaseCycle(false)
    const [failPhase, failPush] = usePhaseCycle(true)
    const [copyPhase, copy] = usePhaseCycle(false)
    return (
      <div className="sb-row">
        <Button variant={variant} phase={pushPhase} onClick={push} style={{ width: 132 }}>
          <Upload data-icon="inline-start" /> {PUSH_LABEL[pushPhase]}
        </Button>
        <Button variant="plain" phase={failPhase} onClick={failPush} style={{ width: 132 }}>
          <Upload data-icon="inline-start" /> {failPhase === 'idle' ? 'Push (fails)' : PUSH_LABEL[failPhase]}
        </Button>
        <IconButton variant="plain" phase={copyPhase} onClick={copy} aria-label={COPY_LABEL[copyPhase]}>
          <Copy />
        </IconButton>
      </div>
    )
  },
}
