import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Field,
  FieldDescription,
  FieldLabel,
  NumberField,
  NumberFieldDecrement,
  NumberFieldGroup,
  NumberFieldIncrement,
  NumberFieldInput,
  NumberStepper,
} from '@wunderui/react'

const meta = {
  title: 'Forms/Number',
  component: NumberField,
  args: { defaultValue: 13, min: 8, max: 32, step: 1, disabled: false },
  argTypes: {
    defaultValue: { control: 'number' },
    min: { control: 'number' },
    max: { control: 'number' },
    step: { control: 'number' },
    disabled: { control: 'boolean' },
  },
} satisfies Meta<typeof NumberField>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <NumberField key={String(args.defaultValue)} {...args}>
      <NumberFieldGroup>
        <NumberFieldDecrement aria-label="Decrease font size" />
        <NumberFieldInput aria-label="Font size" />
        <NumberFieldIncrement aria-label="Increase font size" />
      </NumberFieldGroup>
    </NumberField>
  ),
}

export const EditorSettings: Story = {
  name: 'Editor settings',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 360 }}>
      <Field>
        <FieldLabel>Font size</FieldLabel>
        <NumberField defaultValue={13} min={8} max={32}>
          <NumberFieldGroup>
            <NumberFieldDecrement />
            <NumberFieldInput />
            <NumberFieldIncrement />
          </NumberFieldGroup>
        </NumberField>
        <FieldDescription>Editor and terminal font size in pixels.</FieldDescription>
      </Field>
      <Field>
        <FieldLabel>Tab width</FieldLabel>
        <NumberField defaultValue={2} min={1} max={16}>
          <NumberFieldGroup>
            <NumberFieldDecrement />
            <NumberFieldInput />
            <NumberFieldIncrement />
          </NumberFieldGroup>
        </NumberField>
      </Field>
      <Field>
        <FieldLabel>Port</FieldLabel>
        <NumberField defaultValue={5432} min={1} max={65535} format={{ useGrouping: false }}>
          <NumberFieldGroup>
            <NumberFieldDecrement />
            <NumberFieldInput />
            <NumberFieldIncrement />
          </NumberFieldGroup>
        </NumberField>
      </Field>
      <Field disabled>
        <FieldLabel>Line height</FieldLabel>
        <NumberField defaultValue={1.5} step={0.1} disabled>
          <NumberFieldGroup>
            <NumberFieldDecrement />
            <NumberFieldInput />
            <NumberFieldIncrement />
          </NumberFieldGroup>
        </NumberField>
        <FieldDescription>Inherited from the theme.</FieldDescription>
      </Field>
    </div>
  ),
}

export const Stepper: Story = {
  name: 'NumberStepper',
  render: function Render() {
    const [agents, setAgents] = useState(2)
    const [zoom, setZoom] = useState(100)
    return (
      <div className="sb-stack">
        <div className="sb-row">
          <span className="sb-label">parallel agents</span>
          <NumberStepper value={agents} onValueChange={setAgents} min={1} max={8} aria-label="Parallel agents" />
        </div>
        <div className="sb-row">
          <span className="sb-label">zoom (step 10)</span>
          <NumberStepper value={zoom} onValueChange={setZoom} min={50} max={200} step={10} aria-label="Zoom" />
          <span className="sb-label">{zoom}%</span>
        </div>
      </div>
    )
  },
}
