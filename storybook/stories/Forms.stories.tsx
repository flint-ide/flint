import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Button, Checkbox, Input, Switch, Textarea, Tooltip, TooltipContent, TooltipTrigger } from '@wunderui/react'

const meta = { title: 'Forms/Basics' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

export const Inputs: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <Input placeholder="Search files…" />
      <Input size="sm" placeholder="Small" />
      <Input aria-invalid placeholder="Invalid" defaultValue="feature/ui kit" />
      <Input disabled placeholder="Disabled" />
      <Textarea placeholder="Commit message" />
    </div>
  ),
}

export const Toggles: Story = {
  render: function Render() {
    const [on, setOn] = useState(true)
    return (
      <div className="sb-row">
        <Switch checked={on} onCheckedChange={setOn} aria-label="Auto save" />
        <Switch size="sm" defaultChecked aria-label="Small" />
        <Switch disabled aria-label="Disabled" />
        <Checkbox defaultChecked aria-label="Checked" />
        <Checkbox aria-label="Unchecked" />
        <Checkbox disabled aria-label="Disabled" />
      </div>
    )
  },
}

export const TooltipStory: Story = {
  name: 'Tooltip',
  render: () => (
    <div className="sb-row" style={{ paddingTop: 48 }}>
      <Tooltip>
        <TooltipTrigger render={<Button variant="plain" />}>Hover me</TooltipTrigger>
        <TooltipContent shortcut="⇧⌘P">Command Palette</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger render={<Button variant="plain" />}>Below</TooltipTrigger>
        <TooltipContent side="bottom">Opens below the trigger</TooltipContent>
      </Tooltip>
    </div>
  ),
}
