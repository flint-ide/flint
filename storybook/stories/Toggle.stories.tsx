import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Toggle, ToggleButton } from '@wunderui/react'
import { Bold, Columns2, Italic, Map as MapIcon, PanelLeft, Underline, WrapText } from 'lucide-react'

const meta = {
  title: 'Forms/Toggle',
  component: Toggle,
  args: { size: 'default', disabled: false, defaultPressed: false, 'aria-label': 'Bold' },
  argTypes: {
    size: { control: 'inline-radio', options: ['sm', 'default', 'lg'] },
    disabled: { control: 'boolean' },
    defaultPressed: { control: 'boolean' },
  },
} satisfies Meta<typeof Toggle>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <Toggle key={String(args.defaultPressed)} {...args}>
      <Bold size={16} />
    </Toggle>
  ),
}

export const TextStyle: Story = {
  name: 'Text style',
  render: () => (
    <div className="sb-stack">
      {(['sm', 'default', 'lg'] as const).map((size) => (
        <div key={size} className="sb-row">
          <span className="sb-label">{size}</span>
          <Toggle size={size} aria-label="Bold" defaultPressed>
            <Bold size={16} />
          </Toggle>
          <Toggle size={size} aria-label="Italic">
            <Italic size={16} />
          </Toggle>
          <Toggle size={size} aria-label="Underline">
            <Underline size={16} />
          </Toggle>
          <Toggle size={size} aria-label="Disabled" disabled>
            <WrapText size={16} />
          </Toggle>
        </div>
      ))}
    </div>
  ),
}

export const ViewToggles: Story = {
  name: 'View toggles (ToggleButton)',
  render: function Render() {
    const [sidebar, setSidebar] = useState(true)
    const [minimap, setMinimap] = useState(false)
    const [split, setSplit] = useState(false)
    const [wrap, setWrap] = useState(true)
    return (
      <div className="sb-row">
        <ToggleButton pressed={sidebar} onPressedChange={setSidebar}>
          <PanelLeft size={14} /> Sidebar
        </ToggleButton>
        <ToggleButton pressed={minimap} onPressedChange={setMinimap}>
          <MapIcon size={14} /> Minimap
        </ToggleButton>
        <ToggleButton pressed={split} onPressedChange={setSplit}>
          <Columns2 size={14} /> Split diff
        </ToggleButton>
        <ToggleButton pressed={wrap} onPressedChange={setWrap}>
          <WrapText size={14} /> Word wrap
        </ToggleButton>
        <ToggleButton disabled>Zen mode</ToggleButton>
      </div>
    )
  },
}
