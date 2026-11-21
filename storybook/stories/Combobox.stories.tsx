import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Autocomplete,
  AutocompleteEmpty,
  AutocompleteInput,
  AutocompleteInputGroup,
  AutocompleteItem,
  AutocompleteList,
  AutocompletePopup,
  Combobox,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxGroupLabel,
  ComboboxInput,
  ComboboxInputGroup,
  ComboboxItem,
  ComboboxList,
  ComboboxPopup,
  ComboboxTrigger,
  Field,
  FieldDescription,
  FieldLabel,
} from '@wunderui/react'
import { GitBranch, Terminal } from 'lucide-react'

const BRANCHES = [
  'main',
  'develop',
  'feature/ui-kit',
  'feature/agent-sessions',
  'fix/symlink-review-drafts',
  'chore/dependabot-go-minor',
  'release/0.4',
]

const BRANCH_GROUPS = [
  { label: 'Local', items: ['main', 'feature/ui-kit', 'fix/symlink-review-drafts'] },
  { label: 'origin', items: ['origin/main', 'origin/develop', 'origin/release/0.4'] },
]

const COMMANDS = [
  'git status',
  'git switch -c',
  'git rebase -i origin/main',
  'go test ./...',
  'npm run storybook',
  'task build',
  'task lint',
]

type PlaygroundArgs = { placeholder: string; disabled: boolean }

const meta = {
  title: 'Forms/Combobox',
  args: { placeholder: 'Switch to branch…', disabled: false },
  argTypes: {
    placeholder: { control: 'text' },
    disabled: { control: 'boolean' },
  },
  decorators: [
    (Story) => (
      <div style={{ minHeight: 380 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<PlaygroundArgs>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: function Render(args) {
    const [branch, setBranch] = useState<string | null>('main')
    return (
      <div className="sb-stack" style={{ maxWidth: 320 }}>
        <Combobox items={BRANCHES} value={branch} onValueChange={setBranch} disabled={args.disabled}>
          <ComboboxInputGroup>
            <GitBranch size={16} />
            <ComboboxInput placeholder={args.placeholder} aria-label="Branch" />
            <ComboboxTrigger />
          </ComboboxInputGroup>
          <ComboboxPopup>
            <ComboboxEmpty>No branch matches.</ComboboxEmpty>
            <ComboboxList>
              {(item: string) => (
                <ComboboxItem key={item} value={item}>
                  {item}
                </ComboboxItem>
              )}
            </ComboboxList>
          </ComboboxPopup>
        </Combobox>
        <span className="sb-label" style={{ width: 'auto' }}>
          checked out: {branch ?? '—'}
        </span>
      </div>
    )
  },
}

export const Grouped: Story = {
  name: 'Grouped (local / remote)',
  render: () => (
    <div style={{ maxWidth: 320 }}>
      <Field>
        <FieldLabel>Base branch</FieldLabel>
        <Combobox items={BRANCH_GROUPS} defaultValue="origin/main">
          <ComboboxInputGroup>
            <GitBranch size={16} />
            <ComboboxInput placeholder="Search branches…" />
            <ComboboxTrigger />
          </ComboboxInputGroup>
          <ComboboxPopup>
            <ComboboxEmpty>No branch matches.</ComboboxEmpty>
            <ComboboxList>
              {(group: (typeof BRANCH_GROUPS)[number]) => (
                <ComboboxGroup key={group.label} items={group.items}>
                  <ComboboxGroupLabel>{group.label}</ComboboxGroupLabel>
                  {group.items.map((item) => (
                    <ComboboxItem key={item} value={item}>
                      {item}
                    </ComboboxItem>
                  ))}
                </ComboboxGroup>
              )}
            </ComboboxList>
          </ComboboxPopup>
        </Combobox>
        <FieldDescription>The pull request merges into this branch.</FieldDescription>
      </Field>
    </div>
  ),
}

export const Multiple: Story = {
  name: 'Multiple',
  render: function Render() {
    const [selected, setSelected] = useState<string[]>(['main', 'develop'])
    return (
      <div className="sb-stack" style={{ maxWidth: 320 }}>
        <Combobox items={BRANCHES} multiple value={selected} onValueChange={setSelected}>
          <ComboboxInputGroup>
            <ComboboxInput placeholder="Protected branches…" aria-label="Protected branches" />
            <ComboboxTrigger />
          </ComboboxInputGroup>
          <ComboboxPopup>
            <ComboboxEmpty>No branch matches.</ComboboxEmpty>
            <ComboboxList>
              {(item: string) => (
                <ComboboxItem key={item} value={item}>
                  {item}
                </ComboboxItem>
              )}
            </ComboboxList>
          </ComboboxPopup>
        </Combobox>
        <span className="sb-label" style={{ width: 'auto' }}>
          protected: {selected.join(', ') || '—'}
        </span>
      </div>
    )
  },
}

export const AutocompleteStory: Story = {
  name: 'Autocomplete',
  render: function Render() {
    const [value, setValue] = useState('')
    return (
      <div className="sb-stack" style={{ maxWidth: 360 }}>
        <Autocomplete items={COMMANDS} value={value} onValueChange={setValue}>
          <AutocompleteInputGroup>
            <Terminal size={16} />
            <AutocompleteInput placeholder="Run a command…" aria-label="Command" />
          </AutocompleteInputGroup>
          <AutocompletePopup>
            <AutocompleteEmpty>No matching command.</AutocompleteEmpty>
            <AutocompleteList>
              {(item: string) => (
                <AutocompleteItem key={item} value={item}>
                  {item}
                </AutocompleteItem>
              )}
            </AutocompleteList>
          </AutocompletePopup>
        </Autocomplete>
        <span className="sb-label" style={{ width: 'auto' }}>
          input: {value || '—'}
        </span>
      </div>
    )
  },
}
