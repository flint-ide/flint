import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, Toaster, useToast } from '@wunderui/react'

function Triggers() {
  const toast = useToast()
  return (
    <div className="sb-row">
      <Button variant="plain" onClick={() => toast.add({ title: 'Pushed to origin/main', type: 'success' })}>
        Success
      </Button>
      <Button variant="plain" onClick={() => toast.add({ title: 'Indexing workspace', description: '12,000 files', type: 'info' })}>
        Info
      </Button>
      <Button variant="plain" onClick={() => toast.add({ title: 'Language server restarted', type: 'warning' })}>
        Warning
      </Button>
      <Button
        variant="plain"
        onClick={() =>
          toast.add({
            title: 'go test failed',
            description: 'TestRenameVars: 1 of 8 cases',
            type: 'error',
            actionProps: { children: 'Ask the agent', onClick: () => undefined },
          })
        }
      >
        Error with action
      </Button>
    </div>
  )
}

type Args = { position: 'top-right' | 'bottom-right' }

/** Toaster provides the stack; useToast().add() shows one. Hover the stack to expand it. */
const meta = {
  title: 'Feedback/Toast',
  args: { position: 'bottom-right' },
  argTypes: { position: { control: 'inline-radio', options: ['top-right', 'bottom-right'] } },
  parameters: { layout: 'fullscreen' },
  render: ({ position }) => (
    <div style={{ minHeight: 420, padding: 24 }}>
      <Toaster position={position}>
        <Triggers />
      </Toaster>
    </div>
  ),
} satisfies Meta<Args>

export default meta
export const Toasts: StoryObj<typeof meta> = {}
