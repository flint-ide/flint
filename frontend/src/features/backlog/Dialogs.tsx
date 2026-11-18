import { useState } from 'react'
import { Modal } from '../../components/layout/Modal'
import { closeDialog, createBacklog, createItem, useBacklog, type DialogHost } from './store'

const basename = (p: string) => p.replace(/\/+$/, '').slice(p.replace(/\/+$/, '').lastIndexOf('/') + 1)

/** The dialog of the panel or of a board: only the host it was asked for draws it. */
export function BacklogDialogs({ host }: { host: DialogHost }) {
  const dialog = useBacklog((s) => s.dialog)
  if (!dialog) return null
  if (dialog.kind === 'newBacklog') return host === 'panel' ? <NewBacklogDialog folder={dialog.folder} /> : null
  return dialog.host === host ? <NewItemDialog folder={dialog.folder} /> : null
}

/** New Backlog…: names the backlog in the folder the user picked (optional). */
function NewBacklogDialog({ folder }: { folder: string }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const valid = name.length <= 80
  const create = async () => {
    if (!valid || busy) return
    setBusy(true)
    const err = await createBacklog(folder, name)
    setBusy(false)
    if (err) return setError(err)
    closeDialog()
  }
  return (
    <Modal title="New Backlog" onClose={closeDialog} className="new-prompt">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <span className="muted selectable" title={folder}>Creates flint-backlog.json and an items folder in {folder}</span>
        <label>
          Name (shown in the panel; optional)
          <input
            autoFocus
            value={name}
            placeholder={basename(folder)}
            onChange={(e) => {
              setName(e.target.value)
              setError('')
            }}
          />
        </label>
        {!valid && <span className="form-status error">At most 80 characters.</span>}
        {error && <span className="form-status error">{error}</span>}
        <div className="form-actions">
          <button type="button" onClick={closeDialog}>Cancel</button>
          <button type="submit" className="primary" disabled={!valid || busy}>Create</button>
        </div>
      </form>
    </Modal>
  )
}

/** New Item…: asks for the title; Go derives the id and writes the file, which opens in the editor. */
function NewItemDialog({ folder }: { folder: string }) {
  const [title, setTitle] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const valid = title.trim() !== '' && title.length <= 120
  const create = async () => {
    if (!valid || busy) return
    setBusy(true)
    const err = await createItem(folder, title)
    setBusy(false)
    if (err) return setError(err)
    closeDialog()
  }
  return (
    <Modal title="New Backlog Item" onClose={closeDialog} className="new-prompt">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <label>
          Title
          <input
            autoFocus
            value={title}
            placeholder="Rate limit the checkout API"
            onChange={(e) => {
              setTitle(e.target.value)
              setError('')
            }}
          />
        </label>
        {title.length > 120 && <span className="form-status error">At most 120 characters.</span>}
        {error && <span className="form-status error">{error}</span>}
        <div className="form-actions">
          <button type="button" onClick={closeDialog}>Cancel</button>
          <button type="submit" className="primary" disabled={!valid || busy}>Create and Open</button>
        </div>
      </form>
    </Modal>
  )
}
