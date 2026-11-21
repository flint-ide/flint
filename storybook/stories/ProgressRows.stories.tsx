import type { Meta, StoryObj } from '@storybook/react-vite'
import { HistogramBar, ProgressRow, Stars } from '@wunderui/react'
import { Star } from 'lucide-react'

const RATINGS = [
  { stars: 5, share: 72, count: 1284 },
  { stars: 4, share: 18, count: 321 },
  { stars: 3, share: 6, count: 107 },
  { stars: 2, share: 2, count: 36 },
  { stars: 1, share: 2, count: 29 },
]

const star = <Star fill="currentColor" strokeWidth={0} />

/** One row of a breakdown: label, a thick bar and the share at the end. */
const meta = {
  title: 'Data Display/Progress Rows & Histogram',
  component: ProgressRow,
  args: { label: 'Go', value: 64, tone: 'primary' },
  argTypes: {
    label: { control: 'text' },
    value: { control: { type: 'range', min: 0, max: 100 } },
    valueText: { control: 'text' },
    tone: { control: 'inline-radio', options: ['primary', 'yellow'] },
    icon: { control: false },
  },
  decorators: [(Story) => <div style={{ maxWidth: 420 }}>{Story()}</div>],
} satisfies Meta<typeof ProgressRow>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Languages of the repository, like the GitHub language bar broken into rows. */
export const Languages: Story = {
  render: () => (
    <div className="sb-stack" style={{ gap: 8 }}>
      {(
        [
          ['Go', 64],
          ['TypeScript', 27],
          ['CSS', 6],
          ['Shell', 2],
          ['Other', 1],
        ] as const
      ).map(([label, value]) => (
        <ProgressRow key={label} label={label} value={value} />
      ))}
    </div>
  ),
}

/** Extension rating distribution: yellow tone, a star icon and review counts instead of percentages. */
export const RatingBreakdown: Story = {
  name: 'Rating breakdown',
  render: () => (
    <div className="sb-stack" style={{ gap: 8 }}>
      <div className="sb-row" style={{ gap: 12 }}>
        <span style={{ fontSize: 28, fontWeight: 600 }}>4.6</span>
        <div className="sb-stack" style={{ gap: 2 }}>
          <Stars value={4.6} />
          <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>1,777 reviews · Claude Code extension</span>
        </div>
      </div>
      {RATINGS.map((r) => (
        <ProgressRow key={r.stars} label={r.stars} icon={star} value={r.share} valueText={r.count.toLocaleString('en-US')} tone="yellow" />
      ))}
    </div>
  ),
}

/** Edge values are clamped to 0 – 100. */
export const Bounds: Story = {
  render: () => (
    <div className="sb-stack" style={{ gap: 8 }}>
      {[-10, 0, 50, 100, 140].map((value) => (
        <ProgressRow key={value} label={`value ${value}`} value={value} />
      ))}
    </div>
  ),
}

/** `HistogramBar` (deprecated; use ProgressRow tone="yellow"). */
export const Histogram: Story = {
  name: 'HistogramBar (deprecated)',
  render: () => (
    <div className="sb-stack" style={{ gap: 8 }}>
      {RATINGS.map((r) => (
        <HistogramBar key={r.stars} label={r.stars} value={r.share} count={r.count} />
      ))}
    </div>
  ),
}

/** `Stars` (deprecated; use Rating readOnly size="sm"): fractional values and a custom max. */
export const StarsRow: Story = {
  name: 'Stars (deprecated)',
  render: () => (
    <div className="sb-stack">
      {[5, 4.5, 3.2, 1, 0].map((value) => (
        <div key={value} className="sb-row">
          <span className="sb-label">{value} / 5</span>
          <Stars value={value} />
        </div>
      ))}
      <div className="sb-row">
        <span className="sb-label">7 / 10</span>
        <Stars value={7} max={10} />
      </div>
    </div>
  ),
}
