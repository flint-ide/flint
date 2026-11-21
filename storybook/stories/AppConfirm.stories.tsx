import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Button } from '@flint-ide/ui'
import { ConfirmDialog } from '../../frontend/src/components/layout/ConfirmDialog'
import { chooseAction, confirmAction } from '../../frontend/src/store/dialog'

/** The app's confirmation (frontend/src/components/layout/ConfirmDialog.tsx): what confirmAction() and chooseAction() ask. */
const meta = { title: 'Flint/Confirm', parameters: { layout: 'fullscreen' } } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

/** The question is the title, the rest the description; the confirm button is red only when it loses something. */
export const ConfirmAction: Story = {
  render: () => {
    const [answer, setAnswer] = useState('—')
    return (
      <div style={{ minHeight: 420, padding: 24, display: 'flex', gap: 8, alignItems: 'flex-start' }}>
        <Button variant="outline" size="sm" onClick={async () => setAnswer(String(await confirmAction('Delete connection "prod"? Its password is removed from the macOS Keychain.', 'Delete')))}>
          Destructive
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={async () => setAnswer(String(await confirmAction('Trust this folder?\nTasks and launch configurations in it can run commands on this computer.', 'Trust Folder')))}
        >
          Go ahead
        </Button>
        <Button variant="outline" size="sm" onClick={async () => setAnswer(await chooseAction('Close notes.md? It has unsaved changes.', 'Save', "Don't Save"))}>
          Three ways
        </Button>
        <span style={{ fontSize: 12, color: 'var(--fg-muted)', alignSelf: 'center' }}>Answer: {answer}</span>
        <ConfirmDialog />
      </div>
    )
  },
}
