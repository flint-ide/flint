import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  Avatar,
  AvatarFallback,
  BadgeCounter,
  BadgeIndicator,
  BadgeProcessing,
  type BadgeCounterTone,
  type BadgeIndicatorKind,
  type BadgeProcessingState,
} from '@wunderui/react'
import { Bell, GitPullRequest, MessageSquare } from 'lucide-react'

const TONES: BadgeCounterTone[] = ['neutral', 'brand', 'danger']
const KINDS: BadgeIndicatorKind[] = ['unread', 'online', 'away', 'busy', 'offline', 'check', 'warning', 'lock']
const STATES: BadgeProcessingState[] = ['pending', 'success', 'error']

type Args = { count: number; max: number; tone: BadgeCounterTone; hideZero: boolean }

/** A box that hosts an overlay badge in its top-right corner. */
const anchor: React.CSSProperties = {
  position: 'relative',
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  width: 36,
  height: 36,
  borderRadius: 8,
  border: '1px solid var(--border)',
  background: 'var(--bg-raised)',
  color: 'var(--fg-muted)',
}

/** Small counters, status dots and processing badges for tabs, avatars and activity rows. */
const meta = {
  title: 'Data Display/Badge Counters & Indicators',
  args: { count: 12, max: 99, tone: 'brand', hideZero: false },
  argTypes: {
    count: { control: { type: 'number', min: 0 } },
    max: { control: { type: 'number', min: 1 } },
    tone: { control: 'inline-radio', options: TONES },
  },
  render: ({ count, max, tone, hideZero }) => (
    <div className="sb-row">
      <span className="sb-label">Pull requests</span>
      <BadgeCounter count={count} max={max} tone={tone} hideZero={hideZero} />
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Every tone, with a small count, a two-digit count and one past `max`. */
export const CounterTones: Story = {
  name: 'Counter tones',
  render: () => (
    <div className="sb-stack">
      {TONES.map((tone) => (
        <div key={tone} className="sb-row">
          <span className="sb-label">{tone}</span>
          <BadgeCounter count={3} tone={tone} />
          <BadgeCounter count={42} tone={tone} />
          <BadgeCounter count={250} max={99} tone={tone} />
        </div>
      ))}
      <div className="sb-row">
        <span className="sb-label">hideZero</span>
        <BadgeCounter count={0} tone="neutral" />
        <BadgeCounter count={0} tone="neutral" hideZero />
        <span className="sb-label">(second one renders nothing)</span>
      </div>
    </div>
  ),
}

/** `overlay` pins the counter to the corner of a positioned parent, e.g. an activity bar icon. */
export const CounterOverlay: Story = {
  name: 'Counter overlay',
  render: () => (
    <div className="sb-row" style={{ gap: 24 }}>
      <span style={anchor} title="Notifications">
        <Bell size={18} />
        <BadgeCounter count={4} tone="danger" overlay />
      </span>
      <span style={anchor} title="Pull requests">
        <GitPullRequest size={18} />
        <BadgeCounter count={128} tone="brand" overlay />
      </span>
      <span style={anchor} title="Comments">
        <MessageSquare size={18} />
        <BadgeCounter count={7} tone="neutral" overlay />
      </span>
    </div>
  ),
}

/** Every indicator kind in both sizes. */
export const Indicators: Story = {
  render: () => (
    <div className="sb-stack">
      {(['sm', 'md'] as const).map((size) => (
        <div key={size} className="sb-row" style={{ gap: 16 }}>
          <span className="sb-label">{size}</span>
          {KINDS.map((kind) => (
            <span key={kind} className="sb-row" style={{ gap: 6 }}>
              <BadgeIndicator kind={kind} size={size} label={kind} />
              <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>{kind}</span>
            </span>
          ))}
        </div>
      ))}
    </div>
  ),
}

/** Indicators as overlays: presence on reviewer avatars, a lock on a protected branch. */
export const IndicatorOverlay: Story = {
  name: 'Indicator overlay',
  render: () => (
    <div className="sb-row" style={{ gap: 20 }}>
      {(
        [
          ['AL', 'online'],
          ['GH', 'away'],
          ['LT', 'busy'],
          ['RP', 'offline'],
        ] as const
      ).map(([initials, kind]) => (
        <span key={initials} style={{ position: 'relative', display: 'inline-flex' }}>
          <Avatar>
            <AvatarFallback>{initials}</AvatarFallback>
          </Avatar>
          <BadgeIndicator kind={kind} label={kind} overlay />
        </span>
      ))}
      <span style={anchor} title="Unread messages">
        <MessageSquare size={18} />
        <BadgeIndicator kind="unread" label="Unread" overlay />
      </span>
    </div>
  ),
}

/** A Badge that follows a background job: pending spins, then success or error. */
export const Processing: Story = {
  render: () => (
    <div className="sb-stack">
      {STATES.map((state) => (
        <div key={state} className="sb-row">
          <span className="sb-label">{state}</span>
          <BadgeProcessing state={state}>
            {state === 'pending' ? 'Indexing workspace' : state === 'success' ? 'Indexed' : 'Index failed'}
          </BadgeProcessing>
          <BadgeProcessing state={state} size="sm">
            {state === 'pending' ? 'CI running' : state === 'success' ? 'CI passed' : 'CI failed'}
          </BadgeProcessing>
        </div>
      ))}
    </div>
  ),
}
