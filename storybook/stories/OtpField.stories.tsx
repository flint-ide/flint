import type { Meta, StoryObj } from '@storybook/react-vite'
import { useId, useState } from 'react'
import { Field, FieldDescription, FieldLabel, OtpField, OtpFieldInput } from '@wunderui/react'

const meta = {
  title: 'Forms/OTP Field',
  component: OtpField,
  args: { length: 6, disabled: false, mask: false },
  argTypes: {
    length: { control: 'inline-radio', options: [4, 6, 8] },
    disabled: { control: 'boolean' },
    mask: { control: 'boolean' },
  },
} satisfies Meta<typeof OtpField>

export default meta
type Story = StoryObj<typeof meta>

function Slots({ length, invalid }: { length: number; invalid?: boolean }) {
  const firstId = useId()
  return (
    <>
      <span id={firstId} hidden>
        Digit 1 of {length}
      </span>
      {Array.from({ length }, (_, i) => (
        <OtpFieldInput
          key={i}
          aria-labelledby={i === 0 ? firstId : undefined}
          aria-label={i === 0 ? undefined : `Digit ${i + 1} of ${length}`}
          aria-invalid={invalid || undefined}
        />
      ))}
    </>
  )
}

export const Playground: Story = {
  render: (args) => (
    <OtpField key={args.length} {...args}>
      <Slots length={args.length} />
    </OtpField>
  ),
}

export const DeviceLogin: Story = {
  name: 'Device login code',
  render: function Render() {
    const [code, setCode] = useState('')
    const [done, setDone] = useState<string | null>(null)
    return (
      <Field style={{ maxWidth: 420 }}>
        <FieldLabel>Enter the code shown by `claude login`</FieldLabel>
        <OtpField length={6} value={code} onValueChange={setCode} onValueComplete={setDone}>
          <Slots length={6} />
        </OtpField>
        <FieldDescription>{done ? `Verifying ${done}…` : 'The code expires in 10 minutes.'}</FieldDescription>
      </Field>
    )
  },
}

export const States: Story = {
  render: () => (
    <div className="sb-stack">
      <div className="sb-row">
        <span className="sb-label">4 digits</span>
        <OtpField length={4} defaultValue="12">
          <Slots length={4} />
        </OtpField>
      </div>
      <div className="sb-row">
        <span className="sb-label">invalid</span>
        <OtpField length={6} defaultValue="000000">
          <Slots length={6} invalid />
        </OtpField>
      </div>
      <div className="sb-row">
        <span className="sb-label">disabled</span>
        <OtpField length={6} defaultValue="482" disabled>
          <Slots length={6} />
        </OtpField>
      </div>
    </div>
  ),
}
