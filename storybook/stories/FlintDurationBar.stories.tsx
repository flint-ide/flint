import type { Meta, StoryObj } from '@storybook/react-vite'
import { DurationBar } from '@flint-ide/ui'

/** DurationBar: one step's duration as a thin bar, with a tick at its estimate; the fill turns red when over (ui/src/DurationBar.tsx). */
const meta = {
  title: 'Flint/Duration Bar',
  component: DurationBar,
  args: { value: 4000, estimate: 10000, label: 'Run tests' },
  render: (args) => (
    <div style={{ width: 260 }}>
      <DurationBar {...args} />
    </div>
  ),
} satisfies Meta<typeof DurationBar>

export default meta
type Story = StoryObj<typeof meta>

export const UnderEstimate: Story = {}

export const OverEstimate: Story = {
  args: { value: 12400, estimate: 10000, label: 'Install dependencies' },
}

export const NoEstimate: Story = {
  args: { value: 850, estimate: undefined, label: 'Read file' },
}

/** A shared scale: several steps of one run, each as a share of the longest. */
export const SharedScale: Story = {
  render: () => (
    <div style={{ display: 'grid', gridTemplateColumns: '120px 260px', rowGap: 10, alignItems: 'center', fontSize: 12 }}>
      <span>Plan</span>
      <DurationBar value={850} total={185000} label="Plan" />
      <span>Edit files</span>
      <DurationBar value={95000} estimate={60000} total={185000} label="Edit files" />
      <span>Run tests</span>
      <DurationBar value={185000} estimate={120000} total={185000} label="Run tests" />
    </div>
  ),
}
