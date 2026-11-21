import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Segment, type SegmentOption, type SegmentShape, type SegmentSize } from '@wunderui/react'
import { Columns2, FileDiff, Rows3 } from 'lucide-react'

const SIZES: SegmentSize[] = ['xs', 'sm', 'md', 'lg']
const SHAPES: SegmentShape[] = ['pill', 'rounded']

const DIFF_VIEWS: SegmentOption[] = [
  { value: 'unified', label: 'Unified', icon: <Rows3 size={14} /> },
  { value: 'split', label: 'Split', icon: <Columns2 size={14} /> },
  { value: 'raw', label: 'Raw', icon: <FileDiff size={14} /> },
]

type Args = { size: SegmentSize; shape: SegmentShape; slide: boolean; activation: 'auto' | 'manual'; wrap: boolean }

/** A segmented control: one choice out of a few, here the diff view of the review panel. */
const meta = {
  title: 'Navigation/Segment',
  args: { size: 'md', shape: 'pill', slide: true, activation: 'auto', wrap: false },
  argTypes: {
    size: { control: 'inline-radio', options: SIZES },
    shape: { control: 'inline-radio', options: SHAPES },
    activation: { control: 'inline-radio', options: ['auto', 'manual'] },
  },
  render: function Render(args) {
    const [value, setValue] = useState('unified')
    return <Segment {...args} aria-label="Diff view" options={DIFF_VIEWS} value={value} onValueChange={setValue} />
  },
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Sizes: Story = {
  render: function Render({ shape }) {
    const [value, setValue] = useState('split')
    return (
      <div className="sb-stack">
        {SIZES.map((size) => (
          <div key={size} className="sb-row">
            <span className="sb-label">{size}</span>
            <Segment size={size} shape={shape} aria-label={`Diff view ${size}`} options={DIFF_VIEWS} value={value} onValueChange={setValue} />
          </div>
        ))}
      </div>
    )
  },
}

export const Shapes: Story = {
  render: function Render({ size }) {
    const [value, setValue] = useState('unified')
    return (
      <div className="sb-stack">
        {SHAPES.map((shape) => (
          <div key={shape} className="sb-row">
            <span className="sb-label">{shape}</span>
            <Segment size={size} shape={shape} aria-label={`Diff view ${shape}`} options={DIFF_VIEWS} value={value} onValueChange={setValue} />
          </div>
        ))}
      </div>
    )
  },
}

/** Text only, with one disabled option (no remote configured, so Remote is unavailable). */
export const TextWithDisabled: Story = {
  render: function Render({ size, shape }) {
    const [value, setValue] = useState('local')
    return (
      <Segment
        size={size}
        shape={shape}
        aria-label="Branches"
        value={value}
        onValueChange={setValue}
        options={[
          { value: 'local', label: 'Local' },
          { value: 'remote', label: 'Remote', disabled: true },
          { value: 'tags', label: 'Tags' },
        ]}
      />
    )
  },
}

/** slide={false} switches instantly; activation="manual" needs Enter/Space after moving focus. */
export const NoSlideManual: Story = {
  args: { slide: false, activation: 'manual' },
}

/** wrap lets many options flow to a second row in a narrow sidebar. */
export const Wrapping: Story = {
  render: function Render({ size }) {
    const options = ['All', 'Errors', 'Warnings', 'Info', 'Debug', 'Trace'].map((l) => ({ value: l.toLowerCase(), label: l }))
    const [value, setValue] = useState('all')
    return (
      <div style={{ width: 220, padding: 8, border: '1px solid var(--border)', background: 'var(--bg-raised)' }}>
        <Segment wrap size={size} aria-label="Log level" options={options} value={value} onValueChange={setValue} />
      </div>
    )
  },
}
