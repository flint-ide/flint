import type { Meta, StoryObj } from '@storybook/react-vite'
import { Kbd, KbdGroup, KbdHint, KeyboardHint, type KbdTone } from '@wunderui/react'

const TONES: KbdTone[] = ['default', 'inverse']

type Args = { keys: string; label: string }

/** KbdHint: key caps plus a short label, for the foot of a command palette or a search field. */
const meta = {
  title: 'Actions/Keyboard Hints',
  args: { keys: '⌘ K', label: 'to open the command palette' },
  argTypes: { keys: { control: 'text', description: 'Space-separated keys' } },
  render: ({ keys, label }) => <KbdHint keys={keys.split(' ').filter(Boolean)} label={label} />,
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** A command palette footer with several hints. */
export const PaletteFooter: Story = {
  render: () => (
    <div style={{ width: 560, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-raised)' }}>
      <div style={{ padding: '10px 12px', fontSize: 13, color: 'var(--fg-muted)' }}>&gt; git: checkout branch…</div>
      <div style={{ display: 'flex', gap: 16, padding: '8px 12px', borderTop: '1px solid var(--border)' }}>
        <KbdHint keys={['↑', '↓']} label="to navigate" />
        <KbdHint keys={['↵']} label="to run" />
        <KbdHint keys={['⌘', '↵']} label="to run in new terminal" />
        <KbdHint keys={['esc']} label="to close" />
      </div>
    </div>
  ),
}

/** Kbd and KbdGroup tones: `inverse` for caps on a dark surface such as a tooltip. */
export const Tones: Story = {
  render: () => (
    <div className="sb-stack">
      {TONES.map((tone) => (
        <div
          key={tone}
          className="sb-row"
          style={tone === 'inverse' ? { padding: 8, borderRadius: 6, background: 'var(--fg, #111)', color: 'var(--bg, #fff)' } : { padding: 8 }}
        >
          <span className="sb-label" style={{ width: 80, color: 'inherit' }}>{tone}</span>
          <Kbd tone={tone}>⌘</Kbd>
          <Kbd tone={tone}>Shift</Kbd>
          <KbdGroup tone={tone}>
            <Kbd tone={tone}>⌘</Kbd>
            <Kbd tone={tone}>⇧</Kbd>
            <Kbd tone={tone}>P</Kbd>
          </KbdGroup>
          <KbdGroup tone={tone} separator="then">
            <Kbd tone={tone}>⌘K</Kbd>
            <Kbd tone={tone}>⌘S</Kbd>
          </KbdGroup>
        </div>
      ))}
    </div>
  ),
}

/** On a muted surface (data-kbd-surface="muted") the caps switch to the card fill so the edge stays visible. */
export const OnMutedSurface: Story = {
  render: () => (
    <div className="sb-row">
      <div
        data-kbd-surface="muted"
        style={{ display: 'flex', alignItems: 'center', gap: 8, width: 280, padding: '6px 10px', borderRadius: 999, background: 'var(--bg-raised)', border: '1px solid var(--border)', fontSize: 13, color: 'var(--fg-muted)' }}
      >
        <span style={{ flex: 1 }}>Search files…</span>
        <KbdGroup>
          <Kbd>⌘</Kbd>
          <Kbd>P</Kbd>
        </KbdGroup>
      </div>
    </div>
  ),
}

/** KeyboardHint is the deprecated alias of KbdHint and renders the same thing. */
export const DeprecatedAlias: Story = {
  render: () => (
    <div className="sb-row">
      <KbdHint keys={['⌘', 'B']} label="toggle sidebar" />
      <KeyboardHint keys={['⌘', 'B']} label="toggle sidebar" />
    </div>
  ),
}
