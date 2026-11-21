import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Rating, type RatingLabel, type RatingType } from '@wunderui/react'

const TYPES: RatingType[] = ['stars', 'hearts', 'emojis', 'scale']
const LABELS: RatingLabel[] = ['none', 'left', 'right', 'tooltip']

const meta = {
  title: 'Forms/Rating',
  component: Rating,
  args: { value: 4, max: 5, type: 'stars', label: 'right', readOnly: false, 'aria-label': 'Rate this agent response' },
  argTypes: {
    value: { control: { type: 'number', min: 0, max: 10, step: 0.5 } },
    max: { control: { type: 'number', min: 1, max: 10 } },
    type: { control: 'inline-radio', options: TYPES },
    label: { control: 'inline-radio', options: LABELS },
    readOnly: { control: 'boolean' },
  },
} satisfies Meta<typeof Rating>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: function Render(args) {
    const [value, setValue] = useState(args.value)
    return <Rating {...args} value={value} onValueChange={setValue} />
  },
}

export const Types: Story = {
  render: function Render() {
    const [stars, setStars] = useState(4)
    const [hearts, setHearts] = useState(3)
    const [emoji, setEmoji] = useState(2)
    const [nps, setNps] = useState(9)
    return (
      <div className="sb-stack">
        <div className="sb-row">
          <span className="sb-label">stars</span>
          <Rating type="stars" value={stars} onValueChange={setStars} label="right" aria-label="Response quality" />
        </div>
        <div className="sb-row">
          <span className="sb-label">hearts</span>
          <Rating type="hearts" value={hearts} onValueChange={setHearts} label="tooltip" aria-label="Theme rating" />
        </div>
        <div className="sb-row">
          <span className="sb-label">emojis</span>
          <Rating type="emojis" value={emoji} onValueChange={setEmoji} aria-label="How was this session?" />
        </div>
        <div className="sb-row">
          <span className="sb-label">scale (NPS)</span>
          <Rating type="scale" max={10} value={nps} onValueChange={setNps} aria-label="How likely are you to recommend Flint?" />
        </div>
      </div>
    )
  },
}

export const ReadOnly: Story = {
  name: 'Read-only',
  render: () => (
    <div className="sb-stack">
      <div className="sb-row">
        <span className="sb-label">extension</span>
        <Rating readOnly value={4.5} label="right" aria-label="Go extension rating" />
      </div>
      <div className="sb-row">
        <span className="sb-label">theme</span>
        <Rating readOnly type="hearts" value={3.5} label="left" />
      </div>
    </div>
  ),
}
