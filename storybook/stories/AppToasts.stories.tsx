import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button } from '@flint-ide/ui'
import { Toasts } from '../../frontend/src/components/layout/Toasts'
import { showToast } from '../../frontend/src/store/dialog'

/** The app's toast stack (frontend/src/components/layout/Toasts.tsx): what showToast() shows. */
const meta = { title: 'Flint/Toasts', parameters: { layout: 'fullscreen' } } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

/** Toasts stack bottom right; hover to expand and pause them. The same message twice shows once. */
export const ShowToast: Story = {
  render: () => (
    <div style={{ minHeight: 420, padding: 24, display: 'flex', gap: 8, alignItems: 'flex-start' }}>
      <Button variant="outline" size="sm" onClick={() => showToast('Copied path')}>
        Info
      </Button>
      <Button variant="outline" size="sm" onClick={() => showToast('Pushed 3 commits to origin/main', 'success')}>
        Success
      </Button>
      <Button variant="outline" size="sm" onClick={() => showToast('gopls restarted after a crash', 'warning')}>
        Warning
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={() => showToast('git push failed:\nUpdates were rejected because the remote contains work that you do not have locally.', 'error')}
      >
        Error (two lines)
      </Button>
      <Toasts />
    </div>
  ),
}
