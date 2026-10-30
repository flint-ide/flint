import { useEffect, useState } from 'react'
import { errorMessage } from '../../api/backend'
import { Git, useGit, type Branch } from '../../store/git'
import { useWorkspace } from '../../store/workspace'
import { useTerminal } from '../../store/terminal'
import { showToast } from '../../store/dialog'
import { Modal } from '../layout/Modal'

type After = 'terminal' | 'window' | 'switch'

interface Props {
  root: string
  branches: Branch[]
  onClose: () => void
  onBack: () => void
}

/**
 * Creates a worktree — the AI-first way to let an agent work on its own branch in its
 * own folder while you keep yours. Optionally opens a terminal or a window there.
 */
export function NewWorktreeForm({ root, branches, onClose, onBack }: Props) {
  const current = useGit((s) => s.status?.branch ?? '')
  const [branch, setBranch] = useState('')
  const [isNew, setIsNew] = useState(true)
  const [base, setBase] = useState(current)
  const [path, setPath] = useState('')
  const [pathTouched, setPathTouched] = useState(false)
  const [after, setAfter] = useState<After>('terminal')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const free = branches.filter((b) => !b.remote && !b.worktree)

  // Suggest "<repo>.worktrees/<branch>" next to the main checkout until edited.
  useEffect(() => {
    if (pathTouched || !branch.trim()) return
    const t = setTimeout(() => {
      Git.DefaultWorktreePath(root, branch.trim()).then(setPath, () => undefined)
    }, 150)
    return () => clearTimeout(t)
  }, [root, branch, pathTouched])

  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      await Git.AddWorktree(root, path, branch.trim(), isNew, isNew ? base : '')
      showToast(`Worktree ${branch.trim()} created`)
      if (after === 'terminal') useTerminal.getState().create(path)
      if (after === 'window') await Git.OpenWindow(path)
      if (after === 'switch') await useWorkspace.getState().openFolder(path)
      void useGit.getState().refresh()
      onClose()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="New worktree" onClose={onClose}>
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <div className="form-row checks">
          <label className="check">
            <input type="radio" checked={isNew} onChange={() => setIsNew(true)} /> New branch
          </label>
          <label className="check">
            <input type="radio" checked={!isNew} onChange={() => setIsNew(false)} disabled={free.length === 0} /> Existing branch
          </label>
        </div>
        {isNew ? (
          <div className="form-row">
            <label className="grow">
              Branch
              <input autoFocus value={branch} placeholder="agent/feature-x" spellCheck={false} onChange={(e) => setBranch(e.target.value)} required />
            </label>
            <label className="grow">
              From
              <select value={base} onChange={(e) => setBase(e.target.value)}>
                {branches.filter((b) => !b.remote).map((b) => (
                  <option key={b.name} value={b.name}>{b.name}</option>
                ))}
              </select>
            </label>
          </div>
        ) : (
          <label>
            Branch
            <select value={branch} onChange={(e) => setBranch(e.target.value)} required>
              <option value="">— pick a branch —</option>
              {free.map((b) => (
                <option key={b.name} value={b.name}>{b.name}</option>
              ))}
            </select>
          </label>
        )}
        <label>
          Folder
          <input
            value={path}
            spellCheck={false}
            onChange={(e) => {
              setPath(e.target.value)
              setPathTouched(true)
            }}
            required
          />
        </label>
        <label>
          Then
          <select value={after} onChange={(e) => setAfter(e.target.value as After)}>
            <option value="terminal">Open a terminal there (for claude / codex)</option>
            <option value="window">Open in a new window</option>
            <option value="switch">Switch this window to it</option>
          </select>
        </label>
        {error && <div className="form-status error">{error}</div>}
        <div className="form-actions">
          <button type="button" onClick={onBack}>Back</button>
          <span className="spacer" />
          <button type="submit" className="primary" disabled={busy || !branch.trim() || !path}>
            {busy ? 'Creating…' : 'Create worktree'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
