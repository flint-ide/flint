import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Modal } from '../../frontend/src/components/layout/Modal'
import { ConfirmDialog } from '../../frontend/src/components/layout/ConfirmDialog'
import { ContextMenu as AppContextMenu } from '../../frontend/src/components/ui/ContextMenu'
import { Dropdown } from '../../frontend/src/components/ui/Dropdown'
import { PaletteSearch } from '../../frontend/src/components/ui/PaletteSearch'
import { Select } from '../../frontend/src/components/ui/Select'
import { openContextMenu } from '../../frontend/src/store/contextMenu'
import { confirmAction } from '../../frontend/src/store/dialog'

/** The app's dialog (frontend/src/components/layout/Modal.tsx): the kit's Dialog in the app's `.modal` look. */
const meta = { title: 'Flint/Modal', parameters: { layout: 'fullscreen' } } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const branches = ['main', 'feat/ui-kit', 'fix/symlink-review', 'release/0.9', 'dependabot/go_modules']

/**
 * A form with what the app's dialogs hold: a filter menu, a select, a context menu and a
 * confirmation over it. Escape closes the innermost thing; only the backdrop closes it by mouse.
 */
export const Form: Story = {
  render: function Render() {
    const [open, setOpen] = useState(true)
    const [branch, setBranch] = useState('main')
    const [limit, setLimit] = useState(100)
    const [log, setLog] = useState('—')
    return (
      <div style={{ minHeight: 560, padding: 24 }}>
        <button onClick={() => setOpen(true)}>Open dialog</button> <span id="log">{log}</span>
        {open && (
          <Modal title="New worktree" className="new-worktree" onClose={() => setOpen(false)}>
            <div style={{ display: 'grid', gap: 8 }}>
              <input placeholder="feat/ui-kit" autoFocus />
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <Dropdown
                  searchable
                  items={branches.map((b) => ({ label: b, checked: b === branch, onSelect: () => setBranch(b) }))}
                  trigger={(t) => <button {...t}>from: {branch}</button>}
                />
                <Select label="depth" value={limit} onChange={setLimit} options={[50, 100, 500].map((n) => ({ value: n, label: String(n) }))} />
              </div>
              <div
                className="ctx-area"
                style={{ padding: 12, border: '1px dashed var(--border)', borderRadius: 8, color: 'var(--fg-muted)' }}
                onContextMenu={(e) => {
                  e.preventDefault()
                  openContextMenu(e.clientX, e.clientY, [{ label: 'Copy Branch Name', onSelect: () => setLog('copied') }])
                }}
              >
                Right-click here
              </div>
              <div className="form-actions">
                <span style={{ flex: 1 }} />
                <button onClick={async () => setLog(String(await confirmAction('Discard the new worktree?', 'Discard')))}>Discard…</button>
                <button className="primary" onClick={() => setOpen(false)}>
                  Create
                </button>
              </div>
            </div>
          </Modal>
        )}
        <AppContextMenu />
        <ConfirmDialog />
      </div>
    )
  },
}

/** A palette (Go to File, commands, symbols): `.modal.quick-open`, the kit's Input (PaletteSearch) over a list, no title. */
export const Palette: Story = {
  render: function Render() {
    const [open, setOpen] = useState(true)
    const files = ['src/main.tsx', 'src/style.css', 'src/components/layout/Modal.tsx', 'src/lib/modalDismiss.ts']
    return (
      <div style={{ minHeight: 420, padding: 24 }}>
        <button onClick={() => setOpen(true)}>Go to File…</button>
        {open && (
          <Modal onClose={() => setOpen(false)} className="quick-open">
            <PaletteSearch autoFocus placeholder="Go to file… (1,284 files)" />
            <div className="quick-list">
              {files.map((f, i) => (
                <div key={f} className={`quick-item${i === 0 ? ' selected' : ''}`}>
                  <span className="quick-name">{f.split('/').pop()}</span> <span className="quick-path">{f}</span>
                </div>
              ))}
            </div>
          </Modal>
        )}
      </div>
    )
  },
}
