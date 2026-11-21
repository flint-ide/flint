import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  CalendarDay,
  CalendarHeader,
  CalendarPeriodCell,
  CalendarPeriodPicker,
  DatePickerField,
  DatePickerFooter,
  DatePickerPanel,
  type DateRange,
} from '@wunderui/react'

// Fixed "today" so the stories render the same every day.
const TODAY = new Date(2026, 10, 21)
const RELEASE_RANGE: DateRange = { from: new Date(2026, 10, 9), to: new Date(2026, 10, 20) }
const fmt = (d?: Date) => (d ? d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) : '—')

const meta = {
  title: 'Forms/Date Picker Parts',
  component: DatePickerField,
  args: {
    label: 'Release date',
    placeholder: 'Pick a date',
    helperText: 'The tag is created at 00:00 UTC.',
    error: '',
    fieldState: 'default',
    open: false,
  },
  argTypes: {
    label: { control: 'text' },
    placeholder: { control: 'text' },
    helperText: { control: 'text' },
    error: { control: 'text' },
    fieldState: { control: 'inline-radio', options: ['default', 'disabled', 'readonly'] },
    open: { control: 'boolean' },
  },
} satisfies Meta<typeof DatePickerField>

export default meta
type Story = StoryObj<typeof meta>

export const FieldPlayground: Story = {
  name: 'DatePickerField',
  render: (args) => (
    <div style={{ maxWidth: 320 }}>
      <DatePickerField {...args} error={args.error || undefined} />
    </div>
  ),
}

export const FieldStates: Story = {
  name: 'DatePickerField states',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 320, gap: 20 }}>
      <DatePickerField label="Release date" value={TODAY} />
      <DatePickerField mode="range" label="Sprint" value={RELEASE_RANGE} open />
      <DatePickerField label="Token expires" value={TODAY} error="Expiry must be in the future." />
      <DatePickerField label="Created" value={new Date(2025, 2, 4)} fieldState="readonly" />
      <DatePickerField label="Archived on" fieldState="disabled" helperText={false} />
    </div>
  ),
}

export const PanelSingle: Story = {
  name: 'DatePickerPanel (single)',
  render: function Render() {
    const [picked, setPicked] = useState<Date | undefined>(TODAY)
    return (
      <div className="sb-stack">
        <DatePickerPanel
          today={TODAY}
          value={picked}
          onApply={(v) => setPicked(v instanceof Date ? v : undefined)}
          min={new Date(2026, 9, 1)}
          surface
        />
        <span className="sb-label" style={{ width: 'auto' }}>
          applied: {fmt(picked)}
        </span>
      </div>
    )
  },
}

export const PanelRange: Story = {
  name: 'DatePickerPanel (range, presets)',
  render: () => (
    <DatePickerPanel mode="range" today={TODAY} value={RELEASE_RANGE} defaultMonth={new Date(2026, 9, 1)} presets numberOfMonths={2} surface />
  ),
}

export const PanelStacked: Story = {
  name: 'DatePickerPanel (stacked, instant)',
  render: () => (
    <DatePickerPanel mode="range" today={TODAY} confirmation="instant" stacked header="month-year" unit="days" surface />
  ),
}

export const Footer: Story = {
  name: 'DatePickerFooter',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420, gap: 16 }}>
      <DatePickerFooter summary="Nov 9 – Nov 20, 2026 · 12 days" onCancel={() => undefined} onApply={() => undefined} />
      <DatePickerFooter summary="Pick an end date" applyDisabled />
      <DatePickerFooter summary="Range is longer than 90 days" invalid applyDisabled />
    </div>
  ),
}

export const Header: Story = {
  name: 'CalendarHeader',
  render: function Render() {
    const [month, setMonth] = useState(new Date(2026, 10, 1))
    const shift = (n: number) => setMonth((m) => new Date(m.getFullYear(), m.getMonth() + n, 1))
    return (
      <div className="sb-stack" style={{ maxWidth: 320, gap: 16 }}>
        <CalendarHeader month={month} onPrevious={() => shift(-1)} onNext={() => shift(1)} />
        <CalendarHeader month={month} type="month-year" size="sm" onPrevious={() => shift(-1)} onNext={() => shift(1)} />
        <CalendarHeader month={month} navigation="next" nextDisabled />
        <CalendarHeader month={month} navigation="none" />
      </div>
    )
  },
}

const SELECTIONS = ['none', 'single', 'start', 'middle', 'end', 'preview'] as const
const INTERACTIONS = ['rest', 'hover', 'pressed', 'focus'] as const

export const Days: Story = {
  name: 'CalendarDay',
  render: () => (
    <div className="sb-stack" style={{ gap: 14 }}>
      <div className="sb-row">
        <span className="sb-label">selection</span>
        {SELECTIONS.map((s, i) => (
          <CalendarDay key={s} selection={s} aria-label={s}>
            {10 + i}
          </CalendarDay>
        ))}
      </div>
      <div className="sb-row">
        <span className="sb-label">interaction</span>
        {INTERACTIONS.map((s, i) => (
          <CalendarDay key={s} interaction={s} aria-label={s}>
            {3 + i}
          </CalendarDay>
        ))}
      </div>
      <div className="sb-row">
        <span className="sb-label">flags</span>
        <CalendarDay today>21</CalendarDay>
        <CalendarDay outside>30</CalendarDay>
        <CalendarDay unavailable>22</CalendarDay>
        <CalendarDay marked>24</CalendarDay>
      </div>
      <div className="sb-row">
        <span className="sb-label">size sm</span>
        <CalendarDay size="sm">1</CalendarDay>
        <CalendarDay size="sm" selection="single">2</CalendarDay>
        <CalendarDay size="sm" today>3</CalendarDay>
      </div>
    </div>
  ),
}

export const PeriodCells: Story = {
  name: 'CalendarPeriodCell',
  render: () => (
    <div className="sb-row">
      <CalendarPeriodCell>Jan</CalendarPeriodCell>
      <CalendarPeriodCell current>Nov</CalendarPeriodCell>
      <CalendarPeriodCell selected>Dec</CalendarPeriodCell>
      <CalendarPeriodCell unavailable>2027</CalendarPeriodCell>
      <CalendarPeriodCell interaction="hover">Mar</CalendarPeriodCell>
      <CalendarPeriodCell interaction="focus">Apr</CalendarPeriodCell>
    </div>
  ),
}

export const PeriodPicker: Story = {
  name: 'CalendarPeriodPicker',
  render: function Render() {
    const [month, setMonth] = useState<Date>(new Date(2026, 10, 1))
    const [year, setYear] = useState<Date>(new Date(2026, 0, 1))
    return (
      <div className="sb-row" style={{ alignItems: 'flex-start', gap: 32 }}>
        <div style={{ width: 280 }}>
          <CalendarPeriodPicker type="month" month={month} selected={month} today={TODAY} onSelect={setMonth} onNavigate={setMonth} />
        </div>
        <div style={{ width: 280 }}>
          <CalendarPeriodPicker
            type="year"
            month={year}
            selected={year}
            today={TODAY}
            max={new Date(2027, 11, 31)}
            onSelect={setYear}
            onNavigate={setYear}
          />
        </div>
      </div>
    )
  },
}
