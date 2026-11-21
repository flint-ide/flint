import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Button } from '@wunderui/react'
import { ChevronDown, FolderOpen, GitBranch } from 'lucide-react'
import { Dropdown, type DropdownItem } from '../../frontend/src/components/ui/Dropdown'
import { Select } from '../../frontend/src/components/ui/Select'

const items: DropdownItem[] = [
  { label: 'Open Folder…', icon: <FolderOpen size={14} />, kbd: '⌘O', onSelect: () => undefined },
  { type: 'separator' },
  { type: 'section', label: 'Recent' },
  { label: 'flint', hint: '~/projects', onSelect: () => undefined },
  { label: 'poc-mailos', hint: '~/work', onSelect: () => undefined },
  { label: 'notes', hint: '~/notes', disabled: true, onSelect: () => undefined },
  { type: 'separator' },
  { label: 'Remove from Recent', danger: true, onSelect: () => undefined },
]

/** Flint's own menu (frontend/src/components/ui/Dropdown.tsx): title bar, "+" menu, selects, context menus. */
const meta = { title: 'Flint/Dropdown & Select' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

export const Menu: Story = {
  render: () => (
    <div style={{ minHeight: 320 }}>
      <Dropdown
        items={items}
        trigger={(t) => (
          <Button {...t} variant="plain">
            Projects <ChevronDown data-icon="inline-end" />
          </Button>
        )}
      />
    </div>
  ),
}

export const Searchable: Story = {
  render: () => (
    <div style={{ minHeight: 360 }}>
      <Dropdown
        searchable
        items={['main', 'feat/ui-kit', 'fix/symlink-review', 'release/0.9', 'dependabot/go_modules'].map((b) => ({
          label: b,
          icon: <GitBranch size={14} />,
          checked: b === 'feat/ui-kit',
          onSelect: () => undefined,
        }))}
        trigger={(t) => (
          <Button {...t} variant="plain">
            <GitBranch data-icon="inline-start" /> feat/ui-kit
          </Button>
        )}
      />
    </div>
  ),
}

export const SelectStory: Story = {
  name: 'Select',
  render: function Render() {
    const [limit, setLimit] = useState(100)
    return (
      <div style={{ minHeight: 240 }}>
        <Select
          label="limit"
          value={limit}
          onChange={setLimit}
          options={[50, 100, 500, 1000].map((n) => ({ value: n, label: String(n) }))}
        />
      </div>
    )
  },
}
