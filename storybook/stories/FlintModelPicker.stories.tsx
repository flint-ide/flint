import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, ChatComposer, ModelPicker, type ModelOption } from '@flint-ide/ui'
import { useState } from 'react'

/** ModelPicker: the model chip of a prompt box; its popover switches model and reasoning effort (ui/src/chat/ModelPicker.tsx). */
const meta = {
  title: 'Flint/Model Picker',
  component: ModelPicker,
  args: { models: [], value: '', onChange: () => undefined },
} satisfies Meta<typeof ModelPicker>

export default meta
type Story = StoryObj<typeof meta>

const EFFORTS = ['low', 'medium', 'high'] as const

const CLAUDE: ModelOption[] = [
  { id: 'opus-5.5', label: 'Opus 5.5', description: 'Deepest reasoning', group: 'Claude Code', contextWindow: 200_000, price: '$5 / $25 per MTok', efforts: EFFORTS },
  { id: 'sonnet-5.5', label: 'Sonnet 5.5', description: 'Balanced', group: 'Claude Code', contextWindow: 1_000_000, price: '$3 / $15 per MTok', efforts: EFFORTS },
  { id: 'haiku-5', label: 'Haiku 5', description: 'Fast and cheap', group: 'Claude Code', contextWindow: 200_000, price: '$1 / $5 per MTok' },
]
const CODEX: ModelOption[] = [
  { id: 'gpt-5-codex', label: 'GPT-5 Codex', description: 'Agentic coding', group: 'Codex', contextWindow: 400_000, efforts: ['minimal', ...EFFORTS] },
  { id: 'gpt-5-mini', label: 'GPT-5 mini', description: 'Quick edits', group: 'Codex', contextWindow: 128_000 },
]
const LONG: ModelOption[] = Array.from({ length: 14 }, (_, i) => ({
  id: `model-${i + 1}`,
  label: `Model ${i + 1}`,
  description: i % 2 ? 'Fast' : 'Thorough',
  group: i < 7 ? 'Provider A' : 'Provider B',
  contextWindow: 32_000 * (i + 1),
}))
const WITH_DISABLED: ModelOption[] = [
  CLAUDE[0],
  { ...CLAUDE[1], disabled: true, disabledReason: 'Not on your plan' },
  CLAUDE[2],
  { ...CODEX[0], disabled: true, disabledReason: 'Codex is not signed in' },
]

type DemoProps = Partial<React.ComponentProps<typeof ModelPicker>> & { models: readonly ModelOption[]; initial: string; initialEffort?: string }

/** Holds the model and effort so selecting works. */
function Demo({ initial, initialEffort, ...props }: DemoProps) {
  const [value, setValue] = useState(initial)
  const [effort, setEffort] = useState(initialEffort)
  return <ModelPicker value={value} onChange={setValue} effort={effort} onEffortChange={setEffort} {...props} />
}

const frame = { padding: 12, paddingTop: 340, width: 420 } as const

export const Default: Story = {
  render: () => (
    <div style={frame}>
      <Demo models={CLAUDE.slice(0, 3).map(({ group: _g, ...m }) => m)} initial="sonnet-5.5" />
    </div>
  ),
}

export const GroupedTwoAgents: Story = {
  render: () => (
    <div style={frame}>
      <Demo models={[...CLAUDE, ...CODEX]} initial="opus-5.5" />
    </div>
  ),
}

export const WithEfforts: Story = {
  render: () => (
    <div style={frame}>
      <Demo models={[...CLAUDE, ...CODEX]} initial="opus-5.5" initialEffort="high" />
    </div>
  ),
}

/** More than 8 models: a filter field appears. */
export const SearchableLongList: Story = {
  render: () => (
    <div style={frame}>
      <Demo models={LONG} initial="model-3" />
    </div>
  ),
}

export const DisabledRows: Story = {
  render: () => (
    <div style={frame}>
      <Demo models={WITH_DISABLED} initial="opus-5.5" initialEffort="medium" />
    </div>
  ),
}

/** Closed and dimmed while a turn runs. */
export const DisabledPicker: Story = {
  render: () => (
    <div style={frame}>
      <Demo models={CLAUDE} initial="opus-5.5" initialEffort="high" disabled />
    </div>
  ),
}

/** The model is no longer in the list: the raw id shows. */
export const UnknownValue: Story = {
  render: () => (
    <div style={frame}>
      <Demo models={CLAUDE} initial="claude-legacy-3" initialEffort="low" />
    </div>
  ),
}

/** In the chat's prompt box toolbar. */
export const InPromptToolbar: Story = {
  render: () => (
    <div style={{ ...frame, width: 520 }}>
      <ChatComposer
        placeholder="Message Claude (Enter to send)"
        toolbar={<Demo models={[...CLAUDE, ...CODEX]} initial="sonnet-5.5" initialEffort="medium" />}
        actions={
          <Button variant="primary" size="sm">
            Send
          </Button>
        }
      />
    </div>
  ),
}
