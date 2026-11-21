import type { Meta, StoryObj } from '@storybook/react-vite'
import { Alert, AlertDescription, AlertTitle, Button } from '@wunderui/react'
import { Megaphone, ShieldAlert } from 'lucide-react'

const VARIANTS = ['plain', 'primary', 'positive', 'warning', 'negative'] as const

type Args = { variant: (typeof VARIANTS)[number]; size: 'small' | 'large'; title: string; description: string; dismissible: boolean }

/** Broadcasts: inline alerts (callouts) and full-width banners, both built on Alert. */
const meta = {
  title: 'Feedback/Broadcasts',
  args: {
    variant: 'warning',
    size: 'large',
    title: 'Restricted Mode',
    description: "Flint doesn't run this folder's code on its own until you trust it.",
    dismissible: true,
  },
  argTypes: {
    variant: { control: 'inline-radio', options: VARIANTS },
    size: { control: 'inline-radio', options: ['small', 'large'] },
  },
  render: ({ variant, size, title, description, dismissible }) => (
    <Alert variant={variant} size={size} onClose={dismissible ? () => undefined : undefined}>
      <AlertTitle>{title}</AlertTitle>
      {size === 'large' && <AlertDescription>{description}</AlertDescription>}
    </Alert>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Callout: Story = {}

export const AllVariants: Story = {
  render: () => (
    <div className="sb-stack">
      {VARIANTS.map((variant) => (
        <Alert key={variant} variant={variant} onClose={() => undefined}>
          <AlertTitle>{variant}</AlertTitle>
          <AlertDescription>Restricted Mode: Flint doesn't run this folder's code on its own until you trust it.</AlertDescription>
        </Alert>
      ))}
    </div>
  ),
}

export const Small: Story = {
  render: () => (
    <div className="sb-row">
      {VARIANTS.map((variant) => (
        <Alert key={variant} variant={variant} size="small">
          <AlertTitle>Update available</AlertTitle>
        </Alert>
      ))}
    </div>
  ),
}

/** A banner spans its container edge to edge, e.g. above the editor area. */
export const Banner: Story = {
  parameters: { layout: 'fullscreen' },
  render: () => (
    <div className="sb-stack">
      <Alert variant="primary" icon={<Megaphone />} onClose={() => undefined} style={{ borderRadius: 0, borderInline: 0 }}>
        <AlertTitle>Flint 0.9 is ready</AlertTitle>
        <AlertDescription>Restart to update. Your tabs and terminals come back where you left them.</AlertDescription>
      </Alert>
      <Alert variant="warning" icon={<ShieldAlert />} onClose={() => undefined} style={{ borderRadius: 0, borderInline: 0 }}>
        <AlertTitle>Restricted Mode in flint</AlertTitle>
        <AlertDescription>
          No automatic terminal, no Rust or Python language servers, and agents ignore the project's hooks and settings.{' '}
          <Button size="sm" variant="plain">
            Trust Folder
          </Button>
        </AlertDescription>
      </Alert>
    </div>
  ),
}
