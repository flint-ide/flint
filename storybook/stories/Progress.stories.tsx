import type { Meta, StoryObj } from '@storybook/react-vite'
import { Progress, ProgressCell, ProgressLabel, ProgressValue } from '@wunderui/react'
import { useEffect, useState } from 'react'

const meta = {
  title: 'Feedback/Progress',
  component: Progress,
  args: { value: 64, min: 0, max: 100 },
  argTypes: {
    value: { control: { type: 'range', min: 0, max: 100, step: 1 } },
    min: { control: 'number' },
    max: { control: 'number' },
  },
  render: (args) => (
    <div style={{ maxWidth: 420 }}>
      <Progress {...args}>
        <ProgressLabel>Indexing workspace</ProgressLabel>
        <ProgressValue />
      </Progress>
    </div>
  ),
} satisfies Meta<typeof Progress>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Progress always renders its own ProgressTrack + ProgressIndicator; children add the label and value above it. */
export const Parts: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <div className="sb-row">
        <span className="sb-label">bar only</span>
      </div>
      <Progress value={40} aria-label="Uploading artifacts" />
      <div className="sb-row">
        <span className="sb-label">label</span>
      </div>
      <Progress value={40}>
        <ProgressLabel>Uploading artifacts</ProgressLabel>
      </Progress>
      <div className="sb-row">
        <span className="sb-label">label + value</span>
      </div>
      <Progress value={40}>
        <ProgressLabel>Uploading artifacts</ProgressLabel>
        <ProgressValue />
      </Progress>
    </div>
  ),
}

export const Indeterminate: Story = {
  render: () => (
    <div style={{ maxWidth: 420 }}>
      <Progress value={null}>
        <ProgressLabel>Resolving Go modules…</ProgressLabel>
      </Progress>
    </div>
  ),
}

export const CustomValueText: Story = {
  name: 'Custom value text',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <Progress value={142} max={214}>
        <ProgressLabel>frontend: vitest</ProgressLabel>
        <ProgressValue>{(_, value) => `${value ?? 0} / 214 tests`}</ProgressValue>
      </Progress>
      <Progress value={3} max={5}>
        <ProgressLabel>Claude Code — refactor settings store</ProgressLabel>
        <ProgressValue>{(_, value) => `step ${value ?? 0} of 5`}</ProgressValue>
      </Progress>
      <Progress value={0.82} max={1} format={{ style: 'percent' }}>
        <ProgressLabel>docker pull postgres:17</ProgressLabel>
        <ProgressValue />
      </Progress>
    </div>
  ),
}

function LiveProgress() {
  const [value, setValue] = useState(0)
  useEffect(() => {
    const id = window.setInterval(() => setValue((v) => (v >= 100 ? 0 : v + 4)), 300)
    return () => window.clearInterval(id)
  }, [])
  return (
    <div style={{ maxWidth: 420 }}>
      <Progress value={value}>
        <ProgressLabel>{value >= 100 ? 'Build finished' : 'go build ./cmd/flint'}</ProgressLabel>
        <ProgressValue />
      </Progress>
    </div>
  )
}

export const Live: Story = {
  render: () => <LiveProgress />,
}

export const Cells: Story = {
  name: 'ProgressCell',
  render: () => (
    <div className="sb-stack">
      {[
        { label: 'queued', value: 0 },
        { label: 'checkout', value: 12 },
        { label: 'go test', value: 48 },
        { label: 'vitest', value: 75 },
        { label: 'deploy', value: 100 },
      ].map(({ label, value }) => (
        <div key={label} className="sb-row">
          <span className="sb-label">{label}</span>
          <ProgressCell value={value} />
        </div>
      ))}
    </div>
  ),
}
