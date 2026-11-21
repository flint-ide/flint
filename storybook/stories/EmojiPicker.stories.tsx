import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { EmojiPicker, EmojiReactionButton } from '@wunderui/react'

const REVIEW_EMOJIS = ['👍', '👎', '🎉', '🚀', '👀', '❤️', '😕', '🐛']

const meta = {
  title: 'Forms/Emoji Picker',
  component: EmojiPicker,
} satisfies Meta<typeof EmojiPicker>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: function Render(args) {
    const [picked, setPicked] = useState<string | null>(null)
    return (
      <div className="sb-stack" style={{ maxWidth: 360 }}>
        <EmojiPicker {...args} onSelect={setPicked} />
        <span className="sb-label" style={{ width: 'auto' }}>
          picked: {picked ?? '—'}
        </span>
      </div>
    )
  },
}

export const CustomSet: Story = {
  name: 'Custom set (code review)',
  render: () => (
    <div style={{ maxWidth: 360 }}>
      <EmojiPicker emojis={REVIEW_EMOJIS} />
    </div>
  ),
}

type Reaction = { emoji: string; count: number; active: boolean }

export const Reactions: Story = {
  name: 'Reactions on a review comment',
  render: function Render() {
    const [reactions, setReactions] = useState<Reaction[]>([
      { emoji: '👍', count: 3, active: true },
      { emoji: '🚀', count: 1, active: false },
      { emoji: '👀', count: 2, active: false },
    ])
    const toggle = (emoji: string) =>
      setReactions((list) =>
        list.map((r) => (r.emoji === emoji ? { ...r, active: !r.active, count: r.count + (r.active ? -1 : 1) } : r)),
      )
    const add = (emoji: string) =>
      setReactions((list) =>
        list.some((r) => r.emoji === emoji) ? list : [...list, { emoji, count: 1, active: true }],
      )
    return (
      <div className="sb-stack" style={{ maxWidth: 360 }}>
        <div className="sb-row">
          {reactions.map((r) => (
            <EmojiReactionButton key={r.emoji} emoji={r.emoji} count={r.count} active={r.active} onToggle={() => toggle(r.emoji)} />
          ))}
        </div>
        <EmojiPicker emojis={REVIEW_EMOJIS} onSelect={add} />
      </div>
    )
  },
}
