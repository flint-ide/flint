import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, ButtonGroup, IconButton, Spinner } from '@wunderui/react'
import { Check, GitBranch, Play, Plus, RefreshCw, Settings, Sparkles } from 'lucide-react'

const VARIANTS = ['primary', 'gradient', 'plain', 'outline', 'tint', 'ghost', 'dark', 'light', 'destructive', 'link'] as const
const SIZES = ['sm', 'md', 'lg', 'xl', '2xl'] as const

const meta = {
  title: 'Actions/Button',
  component: Button,
  args: { children: 'Run', variant: 'primary', size: 'md', shape: 'rounded', disabled: false },
  argTypes: {
    variant: { control: 'select', options: VARIANTS },
    size: { control: 'inline-radio', options: SIZES },
    shape: { control: 'inline-radio', options: ['rounded', 'pill'] },
  },
} satisfies Meta<typeof Button>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Variants: Story = {
  render: (args) => (
    <div className="sb-stack">
      {(['sm', 'md', 'lg'] as const).map((size) => (
        <div key={size} className="sb-row">
          <span className="sb-label">{size}</span>
          {VARIANTS.map((variant) => (
            <Button key={variant} {...args} variant={variant} size={size}>
              {variant}
            </Button>
          ))}
        </div>
      ))}
    </div>
  ),
}

export const WithIcons: Story = {
  render: () => (
    <div className="sb-row">
      <Button>
        <Play data-icon="inline-start" /> Run
      </Button>
      <Button variant="plain">
        <GitBranch data-icon="inline-start" /> main
      </Button>
      <Button variant="tint" shape="pill">
        <Sparkles data-icon="inline-start" /> Ask the agent
      </Button>
    </div>
  ),
}

export const States: Story = {
  render: () => (
    <div className="sb-row">
      <Button variant="plain" aria-busy>
        <Spinner data-icon="inline-start" /> Saving…
      </Button>
      <Button variant="plain">
        <Check data-icon="inline-start" /> Saved
      </Button>
      <Button variant="destructive">
        <RefreshCw data-icon="inline-start" /> Retry
      </Button>
      <Button disabled>Disabled</Button>
    </div>
  ),
}

export const IconButtons: Story = {
  render: () => (
    <div className="sb-row">
      {(['plain', 'primary', 'gradient', 'outline', 'ghost', 'dark', 'destructive'] as const).map((variant) => (
        <IconButton key={variant} variant={variant} aria-label={variant}>
          <Settings />
        </IconButton>
      ))}
      <IconButton shape="circle" variant="plain" aria-label="Add">
        <Plus />
      </IconButton>
    </div>
  ),
}

export const Group: Story = {
  render: () => (
    <div className="sb-stack">
      <ButtonGroup>
        <Button variant="plain">
          <GitBranch data-icon="inline-start" /> main
        </Button>
        <Button variant="plain">Pull</Button>
        <Button variant="plain">Push</Button>
      </ButtonGroup>
      <ButtonGroup shape="pill">
        <Button variant="plain">Day</Button>
        <Button variant="plain">Week</Button>
        <Button variant="plain">Month</Button>
      </ButtonGroup>
    </div>
  ),
}
