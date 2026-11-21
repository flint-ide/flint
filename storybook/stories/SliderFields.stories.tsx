import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Slider,
  SliderBubble,
  SliderField,
  SliderInput,
  SliderMarks,
  SliderRangeInputs,
  type SliderHandle,
  type SliderMarkInput,
} from '@wunderui/react'

const meta = {
  title: 'Forms/Slider Fields',
  component: SliderField,
  args: {
    label: 'Font size',
    unit: 'px',
    min: 8,
    max: 32,
    step: 1,
    defaultValue: 13,
    showValue: true,
    showLimits: false,
    showInput: false,
    helper: 'Applies to the editor and the terminal.',
    error: '',
    disabled: false,
  },
  argTypes: {
    label: { control: 'text' },
    unit: { control: 'text' },
    min: { control: 'number' },
    max: { control: 'number' },
    step: { control: 'number' },
    defaultValue: { control: 'number' },
    showValue: { control: 'boolean' },
    showLimits: { control: 'boolean' },
    showInput: { control: 'boolean' },
    helper: { control: 'text' },
    error: { control: 'text' },
    disabled: { control: 'boolean' },
  },
} satisfies Meta<typeof SliderField>

export default meta
type Story = StoryObj<typeof meta>

const box = { maxWidth: 380 } as const

export const Playground: Story = {
  render: (args) => (
    <div style={box}>
      <SliderField key={String(args.defaultValue)} {...args} error={args.error || undefined} />
    </div>
  ),
}

export const FieldVariants: Story = {
  name: 'SliderField variants',
  render: function Render() {
    const [opacity, setOpacity] = useState(85)
    return (
      <div className="sb-stack" style={{ ...box, gap: 28 }}>
        <SliderField label="Minimap opacity" unit="%" min={0} max={100} value={opacity} onValueChange={(v) => setOpacity(v as number)} showLimits />
        <SliderField label="Agent max turns" min={1} max={50} defaultValue={20} showInput helper="The agent stops and asks after this many steps." />
        <SliderField label="Autosave delay" unit="ms" min={0} max={5000} step={250} defaultValue={1000} showInput inputSuffix="ms" />
        <SliderField label="Line length warning" unit="cols" min={60} max={160} defaultValue={[80, 120]} showInput showLimits />
        <SliderField label="Temperature" min={0} max={2} step={0.1} defaultValue={1.6} error="Values above 1.5 make code edits unreliable." />
        <SliderField label="Indexing threads" min={1} max={16} defaultValue={4} disabled helper="Managed by your organization." />
      </div>
    )
  },
}

const HANDLES: SliderHandle[] = ['circle', 'circle-neutral', 'grip-bars', 'grip-dots']

export const Handles: Story = {
  name: 'Handles (SliderHandle)',
  render: () => (
    <div className="sb-stack" style={{ ...box, gap: 20 }}>
      {HANDLES.map((h) => (
        <div key={h} className="sb-row" style={{ flexWrap: 'nowrap' }}>
          <span className="sb-label">{h}</span>
          <div style={{ flex: 1 }}>
            <Slider handle={h} defaultValue={40} aria-label={`Handle ${h}`} />
          </div>
        </div>
      ))}
    </div>
  ),
}

const ZOOM_MARKS: SliderMarkInput[] = [
  { value: 50, label: '50%' },
  75,
  { value: 100, label: '100%' },
  125,
  150,
  175,
  { value: 200, label: '200%' },
]

export const MarksAndBubble: Story = {
  name: 'Marks & bubble',
  render: () => (
    <div className="sb-stack" style={{ ...box, gap: 36, paddingTop: 32 }}>
      <Slider
        aria-label="UI zoom"
        min={50}
        max={200}
        step={25}
        defaultValue={100}
        marks={ZOOM_MARKS}
        bubble="always"
        formatValue={(n) => `${n}%`}
      />
      <Slider aria-label="Diff context lines" min={-10} max={10} defaultValue={3} centered={0} bubble marks={[-10, -5, 0, 5, 10]} />
      <div className="sb-stack" style={{ gap: 4 }}>
        <span className="sb-label" style={{ width: 'auto' }}>
          SliderMarks standalone
        </span>
        <SliderMarks marks={[0, { value: 4, label: '4' }, 8, { value: 12, label: '12' }, 16]} min={0} max={16} />
      </div>
    </div>
  ),
}

export const Bubbles: Story = {
  name: 'SliderBubble',
  render: () => (
    <div className="sb-row" style={{ gap: 48, padding: '40px 16px 16px' }}>
      <span style={{ position: 'relative', display: 'inline-block', width: 1, height: 1 }}>
        <SliderBubble visible>13 px</SliderBubble>
      </span>
      <span style={{ position: 'relative', display: 'inline-block', width: 1, height: 1 }}>
        <SliderBubble visible joined>
          80 – 120
        </SliderBubble>
      </span>
    </div>
  ),
}

export const NumberInputs: Story = {
  name: 'SliderInput & SliderRangeInputs',
  render: function Render() {
    const [tab, setTab] = useState(4)
    const [range, setRange] = useState<[number, number]>([2, 40])
    return (
      <div className="sb-stack" style={{ ...box, gap: 24 }}>
        <div className="sb-row" style={{ flexWrap: 'nowrap', gap: 16 }}>
          <div style={{ flex: 1 }}>
            <Slider aria-label="Tab size" min={1} max={8} value={tab} onValueChange={(v) => setTab(v as number)} />
          </div>
          <SliderInput label="Tab size" value={tab} onValueChange={setTab} min={1} max={8} suffix="sp" />
        </div>
        <SliderInput
          label="Token budget"
          showLabel
          value={8000}
          onValueChange={() => undefined}
          min={1000}
          max={200000}
          step={1000}
          validate={(v) => (v % 2000 !== 0 ? 'Use a multiple of 2,000.' : null)}
        />
        <div className="sb-stack" style={{ gap: 12 }}>
          <Slider aria-label="File size filter" min={0} max={100} value={range} onValueChange={(v) => setRange(v as [number, number])} />
          <SliderRangeInputs
            value={range}
            onValueChange={setRange}
            min={0}
            max={100}
            suffix="MB"
            labels={['Smallest file', 'Largest file']}
          />
        </div>
        <SliderInput label="Disabled" showLabel value={12} onValueChange={() => undefined} disabled />
      </div>
    )
  },
}
