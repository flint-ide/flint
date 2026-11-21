import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Checkbox,
  CheckboxButtonGroup,
  Label,
  RadioButtonGroup,
  RadioGroup,
  RadioGroupItem,
  SelectorItem,
  SelectorItemGroup,
  Switch,
  ToggleButton,
} from '@wunderui/react'
import { Bot, Moon, Sparkles, Sun, Terminal } from 'lucide-react'

const MODELS = [
  { value: 'claude-code', label: 'Claude Code', description: 'Anthropic agent, runs in your shell.' },
  { value: 'codex', label: 'Codex', description: 'OpenAI agent with sandboxed execution.' },
  { value: 'none', label: 'No agent', description: 'Disable agent features for this workspace.' },
]

const meta = {
  title: 'Forms/Radio & Checkbox Groups',
  component: RadioGroup,
  args: { defaultValue: 'claude-code', disabled: false },
  argTypes: {
    defaultValue: { control: 'inline-radio', options: MODELS.map((m) => m.value) },
    disabled: { control: 'boolean' },
  },
} satisfies Meta<typeof RadioGroup>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <RadioGroup key={String(args.defaultValue)} {...args} aria-label="Agent model">
      {MODELS.map((m) => (
        <Label key={m.value}>
          <RadioGroupItem value={m.value} /> {m.label}
        </Label>
      ))}
    </RadioGroup>
  ),
}

export const RadioButtons: Story = {
  name: 'RadioButtonGroup',
  render: function Render() {
    const [theme, setTheme] = useState<string[]>(['dark'])
    const [indent, setIndent] = useState<string[]>(['2'])
    return (
      <div className="sb-stack">
        <div className="sb-row">
          <span className="sb-label">theme</span>
          <RadioButtonGroup value={theme} onValueChange={setTheme} aria-label="Theme">
            <ToggleButton value="light">
              <Sun size={14} /> Light
            </ToggleButton>
            <ToggleButton value="dark">
              <Moon size={14} /> Dark
            </ToggleButton>
            <ToggleButton value="system">System</ToggleButton>
          </RadioButtonGroup>
        </div>
        <div className="sb-row">
          <span className="sb-label">tab width</span>
          <RadioButtonGroup value={indent} onValueChange={setIndent} aria-label="Tab width">
            {['2', '4', '8'].map((w) => (
              <ToggleButton key={w} value={w}>
                {w} spaces
              </ToggleButton>
            ))}
          </RadioButtonGroup>
        </div>
      </div>
    )
  },
}

export const CheckboxButtons: Story = {
  name: 'CheckboxButtonGroup',
  render: function Render() {
    const [langs, setLangs] = useState<string[]>(['go', 'ts'])
    return (
      <div className="sb-stack">
        <span className="sb-label" style={{ width: 'auto' }}>
          Language servers
        </span>
        <CheckboxButtonGroup value={langs} onValueChange={setLangs} aria-label="Language servers">
          {[
            ['go', 'Go (gopls)'],
            ['ts', 'TypeScript'],
            ['rust', 'Rust Analyzer'],
            ['py', 'Pyright'],
            ['yaml', 'YAML'],
          ].map(([value, label]) => (
            <ToggleButton key={value} value={value}>
              {label}
            </ToggleButton>
          ))}
        </CheckboxButtonGroup>
        <span className="sb-label" style={{ width: 'auto' }}>
          enabled: {langs.join(', ') || '—'}
        </span>
      </div>
    )
  },
}

export const Selector: Story = {
  name: 'SelectorItem — radio',
  render: () => (
    <div style={{ maxWidth: 440 }}>
      <RadioGroup defaultValue="claude-code" aria-label="Default agent">
        <SelectorItemGroup>
          {MODELS.map((m) => (
            <SelectorItem
              key={m.value}
              leading={m.value === 'claude-code' ? <Sparkles size={18} /> : m.value === 'codex' ? <Bot size={18} /> : <Terminal size={18} />}
              title={m.label}
              description={m.description}
              control={<RadioGroupItem value={m.value} />}
            />
          ))}
        </SelectorItemGroup>
      </RadioGroup>
    </div>
  ),
}

export const SelectorControls: Story = {
  name: 'SelectorItem — checkbox & switch',
  render: () => (
    <div style={{ maxWidth: 440 }}>
      <SelectorItemGroup>
        <SelectorItem title="Format on save" description="Run the formatter before writing the file." control={<Checkbox defaultChecked />} />
        <SelectorItem title="Trim trailing whitespace" control={<Checkbox />} />
        <SelectorItem title="Auto-fetch remotes" description="Every 5 minutes in the background." control={<Switch defaultChecked />} />
      </SelectorItemGroup>
    </div>
  ),
}
