import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState, type FormEvent } from 'react'
import {
  Button,
  CheckboxField,
  Field,
  FieldControl,
  FieldDescription,
  FieldError,
  FieldLabel,
  Fieldset,
  FieldsetLegend,
  Form,
  FormActions,
  Input,
  Label,
  Switch,
  TextareaField,
  inputVariants,
} from '@wunderui/react'
import { Database, Plug } from 'lucide-react'

const meta = {
  title: 'Forms/Field',
  component: Field,
  args: { disabled: false, invalid: false },
  argTypes: {
    disabled: { control: 'boolean' },
    invalid: { control: 'boolean' },
  },
} satisfies Meta<typeof Field>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 360 }}>
      <Field {...args}>
        <FieldLabel>Shell path</FieldLabel>
        <FieldControl className={inputVariants()} defaultValue="/bin/zsh" placeholder="/bin/zsh" />
        <FieldDescription>Used for the integrated terminal and agent commands.</FieldDescription>
        <FieldError match={args.invalid ? true : undefined}>The shell at this path could not be found.</FieldError>
      </Field>
    </div>
  ),
}

export const States: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 360 }}>
      <Field>
        <FieldLabel>Branch name</FieldLabel>
        <Input placeholder="feature/ui-kit" />
        <FieldDescription>Lowercase letters, digits, dashes and slashes.</FieldDescription>
      </Field>
      <Field invalid>
        <FieldLabel>Branch name</FieldLabel>
        <Input aria-invalid defaultValue="feature/ui kit" />
        <FieldError match>Branch names cannot contain spaces.</FieldError>
      </Field>
      <Field disabled>
        <FieldLabel>Default branch</FieldLabel>
        <Input disabled defaultValue="main" />
        <FieldDescription>Set by the repository.</FieldDescription>
      </Field>
    </div>
  ),
}

export const NativeValidation: Story = {
  name: 'Validation on submit',
  render: () => (
    <Form style={{ maxWidth: 360 }} onSubmit={(e: FormEvent<HTMLFormElement>) => e.preventDefault()}>
      <Field name="tabWidth">
        <FieldLabel>Tab width</FieldLabel>
        <FieldControl className={inputVariants()} type="number" required min={1} max={16} defaultValue="" placeholder="2" />
        <FieldDescription>Between 1 and 16 spaces.</FieldDescription>
        <FieldError />
      </Field>
      <FormActions>
        <Button type="submit">Save</Button>
      </FormActions>
    </Form>
  ),
}

export const LabelStory: Story = {
  name: 'Label',
  render: () => (
    <div className="sb-stack">
      <Label>
        <Switch defaultChecked aria-label="Format on save" /> Format on save
      </Label>
      <Label>
        <Switch aria-label="Word wrap" /> Word wrap
      </Label>
    </div>
  ),
}

export const DatabaseConnection: Story = {
  name: 'New database connection',
  render: function Render() {
    const [saved, setSaved] = useState<string | null>(null)
    const onSubmit = (e: FormEvent<HTMLFormElement>) => {
      e.preventDefault()
      const data = new FormData(e.currentTarget)
      setSaved(`${data.get('user')}@${data.get('host')}:${data.get('port')}/${data.get('database')}`)
    }
    return (
      <Form style={{ maxWidth: 520 }} onSubmit={onSubmit}>
        <Fieldset>
          <FieldsetLegend>Connection</FieldsetLegend>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 120px', gap: 12 }}>
            <Field name="host">
              <FieldLabel>Host</FieldLabel>
              <Input name="host" required defaultValue="localhost" placeholder="db.example.com" />
            </Field>
            <Field name="port">
              <FieldLabel>Port</FieldLabel>
              <Input name="port" type="number" required min={1} max={65535} defaultValue="5432" />
            </Field>
          </div>
          <Field name="database">
            <FieldLabel>Database</FieldLabel>
            <Input name="database" required defaultValue="flint_dev" />
            <FieldDescription>The database opened when you connect.</FieldDescription>
          </Field>
        </Fieldset>
        <Fieldset>
          <FieldsetLegend>Authentication</FieldsetLegend>
          <Field name="user">
            <FieldLabel>User</FieldLabel>
            <Input name="user" required defaultValue="postgres" autoComplete="username" />
          </Field>
          <Field name="password">
            <FieldLabel>Password</FieldLabel>
            <Input name="password" type="password" placeholder="••••••••" autoComplete="current-password" />
            <FieldDescription>Stored in the system keychain.</FieldDescription>
          </Field>
          <CheckboxField name="ssl" defaultChecked label="Require SSL" description="Refuse to connect without TLS." />
        </Fieldset>
        <TextareaField
          name="notes"
          label="Notes"
          optional
          helper="Visible only to you."
          placeholder="Read replica — do not run migrations here."
        />
        <FormActions align="between">
          <Button type="button" variant="plain">
            <Plug data-icon="inline-start" /> Test connection
          </Button>
          <div className="sb-row">
            <Button type="reset" variant="ghost">
              Cancel
            </Button>
            <Button type="submit">
              <Database data-icon="inline-start" /> Connect
            </Button>
          </div>
        </FormActions>
        {saved && <span className="sb-label" style={{ width: 'auto' }}>Saved {saved}</span>}
      </Form>
    )
  },
}

export const TextareaAndCheckbox: Story = {
  name: 'TextareaField & CheckboxField',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <TextareaField label="Commit message" helper="The first line becomes the summary." placeholder="fix: …" />
      <TextareaField label="Commit message" error="A commit message is required." aria-invalid />
      <CheckboxField label="Sign commits" description="Uses your configured GPG key." defaultChecked />
      <CheckboxField label="Amend previous commit" />
      <CheckboxField label="Push after commit" disabled />
    </div>
  ),
}
