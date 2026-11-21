import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Slider, SliderLabel, SliderValue } from '@wunderui/react'

const meta = {
  title: 'Forms/Slider',
  component: Slider,
  args: { defaultValue: 13, min: 8, max: 32, step: 1, disabled: false, 'aria-label': 'Font size' },
  argTypes: {
    defaultValue: { control: 'number' },
    min: { control: 'number' },
    max: { control: 'number' },
    step: { control: 'number' },
    disabled: { control: 'boolean' },
  },
} satisfies Meta<typeof Slider>

export default meta
type Story = StoryObj<typeof meta>

const header = { display: 'flex', alignItems: 'center' } as const

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 360 }}>
      <Slider key={String(args.defaultValue)} {...args}>
        <div style={header}>
          <SliderLabel>Font size</SliderLabel>
          <SliderValue />
        </div>
      </Slider>
    </div>
  ),
}

export const EditorTypography: Story = {
  name: 'Editor typography',
  render: function Render() {
    const [size, setSize] = useState(13)
    const [lineHeight, setLineHeight] = useState(1.5)
    return (
      <div className="sb-stack" style={{ maxWidth: 360, gap: 24 }}>
        <Slider value={size} onValueChange={(v) => setSize(v as number)} min={8} max={32}>
          <div style={header}>
            <SliderLabel>Font size</SliderLabel>
            <SliderValue>{(formatted) => `${formatted} px`}</SliderValue>
          </div>
        </Slider>
        <Slider value={lineHeight} onValueChange={(v) => setLineHeight(v as number)} min={1} max={2.4} step={0.1}>
          <div style={header}>
            <SliderLabel>Line height</SliderLabel>
            <SliderValue />
          </div>
        </Slider>
        <pre
          style={{
            margin: 0,
            padding: 12,
            borderRadius: 8,
            background: 'var(--bg-hover)',
            fontFamily: 'var(--font-mono)',
            fontSize: size,
            lineHeight,
          }}
        >
          {'func main() {\n\tfmt.Println("flint")\n}'}
        </pre>
      </div>
    )
  },
}

export const Range: Story = {
  render: () => (
    <div style={{ maxWidth: 360 }}>
      <Slider defaultValue={[80, 120]} min={40} max={200} aria-label="Ruler columns">
        <div style={header}>
          <SliderLabel>Rulers (columns)</SliderLabel>
          <SliderValue />
        </div>
      </Slider>
    </div>
  ),
}

export const Disabled: Story = {
  render: () => (
    <div style={{ maxWidth: 360 }}>
      <Slider defaultValue={50} disabled aria-label="Minimap scale">
        <div style={header}>
          <SliderLabel>Minimap scale</SliderLabel>
          <SliderValue />
        </div>
      </Slider>
    </div>
  ),
}
