import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { ColorPicker, ColorPickerTrigger, Field, FieldDescription, FieldLabel, type ColorFormat } from '@wunderui/react'

const THEME_SWATCHES = ['#6366F1', '#22C55E', '#F59E0B', '#EF4444', '#0EA5E9', '#A855F7']

const meta = {
  title: 'Forms/Color Picker',
  component: ColorPicker,
  args: { variant: 'spectrum', defaultValue: '#6366F1', showAlpha: false, title: 'Accent color', defaultFormat: 'hex' },
  argTypes: {
    variant: { control: 'inline-radio', options: ['spectrum', 'grid'] },
    defaultValue: { control: 'color' },
    showAlpha: { control: 'boolean' },
    title: { control: 'text' },
    defaultFormat: { control: 'inline-radio', options: ['hex', 'rgb', 'hsl'] satisfies ColorFormat[] },
  },
  decorators: [
    (Story) => (
      <div style={{ minHeight: 380 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof ColorPicker>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => <ColorPicker key={`${args.variant}-${args.defaultValue}`} defaultSwatches={THEME_SWATCHES} {...args} />,
}

export const Variants: Story = {
  render: () => (
    <div className="sb-row" style={{ alignItems: 'flex-start', gap: 24 }}>
      <ColorPicker variant="spectrum" title="Spectrum" defaultValue="#22C55E" defaultSwatches={THEME_SWATCHES} />
      <ColorPicker variant="grid" title="Grid" defaultValue="#F59E0B" />
      <ColorPicker title="With opacity" defaultValue="#0EA5E9" defaultAlpha={0.6} showAlpha />
    </div>
  ),
}

export const ThemeOverride: Story = {
  name: 'Theme color override (trigger)',
  render: function Render() {
    const [accent, setAccent] = useState('#6366F1')
    const [selection, setSelection] = useState('#264F78')
    return (
      <div className="sb-stack" style={{ maxWidth: 320 }}>
        <Field>
          <FieldLabel>Accent color</FieldLabel>
          <ColorPickerTrigger
            label="Accent color"
            value={accent}
            onValueChange={setAccent}
            pickerProps={{ defaultSwatches: THEME_SWATCHES, title: 'Accent color' }}
          />
          <FieldDescription>Overrides the theme&apos;s focus rings, links and primary buttons.</FieldDescription>
        </Field>
        <Field>
          <FieldLabel>Editor selection</FieldLabel>
          <ColorPickerTrigger label="Editor selection" value={selection} onValueChange={setSelection} pickerProps={{ variant: 'grid' }} />
        </Field>
        <div style={{ display: 'flex', gap: 8 }}>
          <span style={{ width: 32, height: 32, borderRadius: 6, background: accent }} />
          <span style={{ width: 32, height: 32, borderRadius: 6, background: selection }} />
        </div>
      </div>
    )
  },
}
