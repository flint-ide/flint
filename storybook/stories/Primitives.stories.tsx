import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Button,
  CellColorPicker,
  CellSlider,
  Density,
  DividerWithLabel,
  Input,
  PressableFeedback,
  PullToRefresh,
  Switch,
  TrendCaret,
} from '@wunderui/react'
import { GitBranch, KeyRound } from 'lucide-react'

const meta = { title: 'Layout/Primitives' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

export const DividerLabel: Story = {
  name: 'Divider with label',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 360 }}>
      <Button variant="plain">
        <GitBranch data-icon="inline-start" /> Sign in with GitHub
      </Button>
      <DividerWithLabel>or</DividerWithLabel>
      <Button variant="plain">
        <KeyRound data-icon="inline-start" /> Use a personal access token
      </Button>
    </div>
  ),
}

/** WunderUI defaults to compact (Flint's scale). `Density` switches one region to comfortable. */
export const DensityScale: Story = {
  name: 'Density',
  render: () => (
    <div className="sb-grid">
      {(['compact', 'comfortable'] as const).map((value) => (
        <Density key={value} value={value}>
          <div className="sb-stack">
            <span className="sb-label">{value}</span>
            <Input placeholder="Branch name" />
            <div className="sb-row">
              <Button>Create branch</Button>
              <Button variant="plain">Cancel</Button>
              <Switch defaultChecked aria-label="Checkout" />
            </div>
          </div>
        </Density>
      ))}
    </div>
  ),
}

export const Pressable: Story = {
  name: 'Pressable feedback',
  render: function Render() {
    const [count, setCount] = useState(0)
    return (
      <div className="sb-row">
        <PressableFeedback onPress={() => setCount((n) => n + 1)}>
          <div style={{ padding: '12px 16px', border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg-raised)' }}>
            Press and hold me
          </div>
        </PressableFeedback>
        <span className="sb-label">pressed {count}×</span>
      </div>
    )
  },
}

export const PullRefresh: Story = {
  name: 'Pull to refresh',
  render: function Render() {
    const [fetchedAt, setFetchedAt] = useState(() => new Date().toLocaleTimeString())
    const refresh = () =>
      new Promise<void>((resolve) =>
        setTimeout(() => {
          setFetchedAt(new Date().toLocaleTimeString())
          resolve()
        }, 800),
      )
    return (
      <div style={{ maxWidth: 360, height: 220, overflow: 'auto', border: '1px solid var(--border)', borderRadius: 8 }}>
        <PullToRefresh onRefresh={refresh}>
          <div className="sb-stack" style={{ padding: 16 }}>
            <strong>Pull requests</strong>
            <span className="sb-label">Drag down to fetch. Last fetch: {fetchedAt}</span>
            {['#76 project feature proposals', '#75 go-minor bumps', '#74 review drafts'].map((pr) => (
              <span key={pr}>{pr}</span>
            ))}
          </div>
        </PullToRefresh>
      </div>
    )
  },
}

export const Cells: Story = {
  name: 'Table cell controls',
  render: function Render() {
    const [color, setColor] = useState('#f59e0b')
    return (
      <div className="sb-stack" style={{ maxWidth: 260 }}>
        <span className="sb-label">CellSlider: coverage</span>
        <CellSlider value={82} />
        <span className="sb-label">CellColorPicker: label color</span>
        <CellColorPicker value={color} onValueChange={setColor} />
      </div>
    )
  },
}

export const Numbers: Story = {
  name: 'Trend caret',
  render: () => (
    <div className="sb-row" style={{ gap: 32 }}>
      <span className="sb-row">
        <TrendCaret /> <TrendCaret down /> <TrendCaret large />
      </span>
    </div>
  ),
}
