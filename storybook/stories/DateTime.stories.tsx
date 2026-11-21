import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Calendar, DatePicker, datePickerPresets, type DatePreset, type DateRange } from '@wunderui/react'

const day = (offset: number) => {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() + offset)
  return d
}

const COMMIT_PRESETS: DatePreset[] = [
  { label: 'Today', range: () => ({ from: day(0), to: day(0) }) },
  { label: 'Since yesterday', range: () => ({ from: day(-1), to: day(0) }) },
  { label: 'Last 7 days', range: () => ({ from: day(-6), to: day(0) }) },
  { label: 'Last sprint (14 days)', range: () => ({ from: day(-13), to: day(0) }) },
  { label: 'Last 30 days', range: () => ({ from: day(-29), to: day(0) }) },
]

type PlaygroundArgs = { placeholder: string }

const meta = {
  title: 'Forms/Date & Time',
  args: { placeholder: 'Pick a date' },
  argTypes: { placeholder: { control: 'text' } },
  decorators: [
    (Story) => (
      <div style={{ minHeight: 420 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<PlaygroundArgs>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => <DatePicker placeholder={args.placeholder} />,
}

export const CalendarSingle: Story = {
  name: 'Calendar — single',
  render: function Render() {
    const [date, setDate] = useState<Date>(day(0))
    return (
      <div className="sb-stack">
        <Calendar selected={date} onSelect={setDate} />
        <span className="sb-label" style={{ width: 'auto' }}>
          release date: {date.toDateString()}
        </span>
      </div>
    )
  },
}

export const CalendarRange: Story = {
  name: 'Calendar — range with limits',
  render: function Render() {
    const [range, setRange] = useState<DateRange>({ from: day(-3), to: day(2) })
    return (
      <div className="sb-stack">
        <Calendar
          mode="range"
          selected={range}
          onSelect={setRange}
          min={day(-30)}
          max={day(30)}
          disabled={(d) => d.getDay() === 0 || d.getDay() === 6}
        />
        <span className="sb-label" style={{ width: 'auto' }}>
          freeze window: {range.from?.toDateString() ?? '—'} → {range.to?.toDateString() ?? '—'} (weekends blocked)
        </span>
      </div>
    )
  },
}

export const PickerSingle: Story = {
  name: 'DatePicker — single',
  render: function Render() {
    const [date, setDate] = useState<Date | undefined>()
    return (
      <div className="sb-stack" style={{ maxWidth: 280 }}>
        <DatePicker value={date} onValueChange={setDate} placeholder="Schedule agent run" min={day(0)} />
        <span className="sb-label" style={{ width: 'auto' }}>
          {date ? date.toDateString() : 'not scheduled'}
        </span>
      </div>
    )
  },
}

export const PickerRange: Story = {
  name: 'DatePicker — range',
  render: () => <DatePicker mode="range" placeholder="Commits between…" defaultValue={{ from: day(-7), to: day(0) }} />,
}

export const PickerPresets: Story = {
  name: 'DatePicker — presets',
  render: function Render() {
    const [range, setRange] = useState<DateRange | undefined>()
    return (
      <div className="sb-stack">
        <div className="sb-row">
          <span className="sb-label">custom presets</span>
          <DatePicker mode="range" presets={COMMIT_PRESETS} value={range} onValueChange={setRange} placeholder="Filter git log" />
        </div>
        <div className="sb-row">
          <span className="sb-label">built-in ({datePickerPresets.length})</span>
          <DatePicker mode="range" presets placeholder="Activity range" />
        </div>
      </div>
    )
  },
}
