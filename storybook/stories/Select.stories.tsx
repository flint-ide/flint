import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Field,
  FieldDescription,
  FieldLabel,
  InlineSelect,
  InlineSelectContent,
  InlineSelectItem,
  InlineSelectTrigger,
  InlineSelectValue,
  NativeSelect,
  NativeSelectContent,
  NativeSelectItem,
  NativeSelectTrigger,
  NativeSelectValue,
} from '@wunderui/react'

const MODELS = [
  { value: 'claude-code', label: 'Claude Code' },
  { value: 'codex', label: 'Codex' },
  { value: 'gemini-cli', label: 'Gemini CLI' },
  { value: 'aider', label: 'Aider' },
]

const THEMES = [
  { value: 'system', label: 'System' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
]

const meta = {
  title: 'Forms/Select',
  component: NativeSelect,
  args: { disabled: false, defaultValue: 'claude-code' },
  argTypes: {
    disabled: { control: 'boolean' },
    defaultValue: { control: 'select', options: MODELS.map((m) => m.value) },
  },
  decorators: [
    (Story) => (
      <div style={{ minHeight: 380 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof NativeSelect>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 280 }}>
      <NativeSelect key={String(args.defaultValue)} {...args} items={MODELS}>
        <NativeSelectTrigger aria-label="Agent model">
          <NativeSelectValue />
        </NativeSelectTrigger>
        <NativeSelectContent>
          {MODELS.map((m) => (
            <NativeSelectItem key={m.value} value={m.value}>
              {m.label}
            </NativeSelectItem>
          ))}
        </NativeSelectContent>
      </NativeSelect>
    </div>
  ),
}

export const InField: Story = {
  name: 'In a field',
  render: function Render() {
    const [theme, setTheme] = useState<string | null>('system')
    return (
      <div style={{ maxWidth: 280 }}>
        <Field>
          <FieldLabel>Theme</FieldLabel>
          <NativeSelect value={theme} onValueChange={setTheme} items={THEMES}>
            <NativeSelectTrigger>
              <NativeSelectValue />
            </NativeSelectTrigger>
            <NativeSelectContent>
              {THEMES.map((t) => (
                <NativeSelectItem key={t.value} value={t.value}>
                  {t.label}
                </NativeSelectItem>
              ))}
            </NativeSelectContent>
          </NativeSelect>
          <FieldDescription>Current value: {theme}</FieldDescription>
        </Field>
      </div>
    )
  },
}

export const Disabled: Story = {
  render: () => (
    <div style={{ maxWidth: 280 }}>
      <NativeSelect disabled defaultValue="codex" items={MODELS}>
        <NativeSelectTrigger aria-label="Agent model">
          <NativeSelectValue />
        </NativeSelectTrigger>
        <NativeSelectContent>
          {MODELS.map((m) => (
            <NativeSelectItem key={m.value} value={m.value}>
              {m.label}
            </NativeSelectItem>
          ))}
        </NativeSelectContent>
      </NativeSelect>
    </div>
  ),
}

export const Inline: Story = {
  name: 'InlineSelect',
  render: function Render() {
    const [tab, setTab] = useState<string | null>('2')
    const [ending, setEnding] = useState<string | null>('lf')
    return (
      <div className="sb-row" style={{ fontSize: 13 }}>
        <span>Indent using spaces, width</span>
        <InlineSelect value={tab} onValueChange={setTab}>
          <InlineSelectTrigger aria-label="Tab width">
            <InlineSelectValue />
          </InlineSelectTrigger>
          <InlineSelectContent>
            {['2', '4', '8'].map((w) => (
              <InlineSelectItem key={w} value={w}>
                {w}
              </InlineSelectItem>
            ))}
          </InlineSelectContent>
        </InlineSelect>
        <span>· line endings</span>
        <InlineSelect value={ending} onValueChange={setEnding}>
          <InlineSelectTrigger aria-label="Line endings">
            <InlineSelectValue />
          </InlineSelectTrigger>
          <InlineSelectContent>
            <InlineSelectItem value="lf">LF</InlineSelectItem>
            <InlineSelectItem value="crlf">CRLF</InlineSelectItem>
          </InlineSelectContent>
        </InlineSelect>
      </div>
    )
  },
}
