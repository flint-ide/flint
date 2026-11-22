import { useEffect, useMemo, useState } from 'react'
import { errorMessage } from '../../api/backend'
import { Git, updateProject, useGit, type Branch, type Worktree } from '../../store/git'
import { useWorkspace, type BranchesMode } from '../../store/workspace'
import { useTerminal } from '../../store/terminal'
import { confirmAction, showToast } from '../../store/dialog'
import { Modal } from '../layout/Modal'
import { NewWorktreeForm } from './NewWorktreeForm'
import { Icon } from '../ui/Icon'
import { Button, FormActions, IconButton, Input } from '@flint-ide/ui'
import { ExternalLink, X } from '@flint-ide/ui/icons'
import { PaletteSearch } from '../ui/PaletteSearch'

/** IntelliJ-like branches popup: filter, checkout, new branch, worktrees, remote ops. */
export function BranchesPopup({ onClose, initialMode = 'list' }: { onClose: () => void; initialMode?: BranchesMode }) {
  const root = useGit((s) => s.root)
  const status = useGit((s) => s.status)
  const remote = useGit((s) => s.remote)
  const refresh = useGit((s) => s.refresh)
  const openFolder = useWorkspace((s) => s.openFolder)
  const [branches, setBranches] = useState<Branch[]>([])
  const [worktrees, setWorktrees] = useState<Worktree[]>([])
  const [filter, setFilter] = useState('')
  const [mode, setMode] = useState<BranchesMode>(initialMode)
  const [newBranch, setNewBranch] = useState('')

  const reload = async () => {
    if (!root) return
    try {
      const [b, w] = await Promise.all([Git.Branches(root), Git.Worktrees(root)])
      setBranches(b ?? [])
      setWorktrees(w ?? [])
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  }
  useEffect(() => void reload(), [root]) // eslint-disable-line react-hooks/exhaustive-deps

  const q = filter.toLowerCase()
  const matches = (name: string) => name.toLowerCase().includes(q)
  const local = useMemo(() => branches.filter((b) => !b.remote && matches(b.name)), [branches, q]) // eslint-disable-line react-hooks/exhaustive-deps
  const remotes = useMemo(() => branches.filter((b) => b.remote && matches(b.name)), [branches, q]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!root) return null

  const run = async (action: () => Promise<unknown>, done?: string) => {
    try {
      await action()
      if (done) showToast(done)
      onClose()
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      void refresh()
    }
  }

  const checkout = (b: Branch) => {
    if (b.worktree && b.worktree !== root) {
      // A branch can be checked out in one worktree only: go there instead.
      return run(() => openFolder(b.worktree), `Switched to worktree ${b.worktree}`)
    }
    return run(() => Git.Checkout(root, b.name, b.remote), `Checked out ${b.name}`)
  }

  const deleteBranch = async (b: Branch) => {
    if (!(await confirmAction(`Delete branch ${b.name}?`, 'Delete'))) return
    try {
      await Git.DeleteBranch(root, b.name, false)
    } catch (err) {
      const msg = errorMessage(err)
      if (!msg.includes('not fully merged') || !(await confirmAction(`${b.name} is not fully merged. Delete anyway?`, 'Force delete'))) {
        if (!msg.includes('not fully merged')) showToast(msg, 'error')
        return
      }
      await Git.DeleteBranch(root, b.name, true).catch((e) => showToast(errorMessage(e), 'error'))
    }
    void reload()
  }

  const removeWorktree = async (w: Worktree) => {
    if (!(await confirmAction(`Remove worktree ${w.path}? Its folder is deleted (the branch stays).`, 'Remove'))) return
    try {
      await Git.RemoveWorktree(root, w.path, false)
    } catch (err) {
      const msg = errorMessage(err)
      if (!(await confirmAction(`${msg}\n\nForce removal and discard its changes?`, 'Force remove'))) return
      await Git.RemoveWorktree(root, w.path, true).catch((e) => showToast(errorMessage(e), 'error'))
    }
    void reload()
  }

  if (mode === 'new-worktree') {
    return <NewWorktreeForm root={root} branches={branches} onClose={onClose} onBack={() => setMode('list')} />
  }

  return (
    <Modal onClose={onClose} className="branches-popup">
      {mode === 'new-branch' ? (
        <form
          className="popup-form"
          onSubmit={(e) => {
            e.preventDefault()
            void run(() => Git.CreateBranch(root, newBranch.trim(), '', true), `Created ${newBranch.trim()}`)
          }}
        >
          <div className="modal-title">New branch from {status?.branch || 'HEAD'}</div>
          <Input autoFocus value={newBranch} placeholder="feature/name" spellCheck={false} onChange={(e) => setNewBranch(e.target.value)} />
          <FormActions align="end">
            <Button type="button" variant="plain" onClick={() => setMode('list')}>Back</Button>
            <Button type="submit" variant="primary" disabled={!newBranch.trim()}>Create &amp; checkout</Button>
          </FormActions>
        </form>
      ) : (
        <>
          <PaletteSearch autoFocus placeholder="Search branches and worktrees" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <div className="quick-list">
            <PopupAction label="＋ New Branch…" onClick={() => setMode('new-branch')} />
            <PopupAction label="＋ New Worktree…" hint="separate folder + terminal for an agent" onClick={() => setMode('new-worktree')} />
            <PopupAction label="⇣ Fetch" onClick={() => void remote('fetch').then(onClose)} />
            <PopupAction label="⤓ Update Repository (pull)" onClick={() => (onClose(), updateProject())} />
            <PopupAction label="⤒ Push" hint={status?.ahead ? `${status.ahead} commit(s)` : ''} onClick={() => void remote('push').then(onClose)} />

            {worktrees.length > 1 && <div className="popup-section">Worktrees</div>}
            {worktrees.length > 1 &&
              worktrees
                .filter((w) => matches(w.path) || matches(w.branch))
                .map((w) => (
                  <div key={w.path} className={`quick-item${w.current ? ' current' : ''}`} onClick={() => void run(() => openFolder(w.path))} title={w.path}>
                    <span className="file-name">{w.current ? '● ' : ''}{w.branch || `detached ${w.head.slice(0, 7)}`}</span>
                    <span className="muted path-hint">{w.path}</span>
                    <span className="row-actions inline">
                      <IconButton size="sm" variant="ghost" aria-label="Open in new window" title="Open in new window" onClick={(e) => (e.stopPropagation(), void run(() => Git.OpenWindow(w.path)))}><ExternalLink /></IconButton>
                      <IconButton size="sm" variant="ghost" aria-label="Open terminal here" title="Open terminal here" onClick={(e) => (e.stopPropagation(), useTerminal.getState().create(w.path), onClose())}><Icon name="terminal" size={14} /></IconButton>
                      {!w.main && !w.current && (
                        <IconButton size="sm" variant="ghost" aria-label="Remove worktree" title="Remove worktree" onClick={(e) => (e.stopPropagation(), void removeWorktree(w))}><X /></IconButton>
                      )}
                    </span>
                  </div>
                ))}

            <div className="popup-section">Local</div>
            {local.map((b) => (
              <div key={b.name} className={`quick-item${b.current ? ' current' : ''}`} onClick={() => !b.current && void checkout(b)}>
                <span className="file-name">{b.current ? '● ' : ''}{b.name}</span>
                <span className="muted path-hint">
                  {b.worktree && b.worktree !== root ? `in worktree ${b.worktree.split('/').pop()}` : b.track || b.upstream}
                </span>
                {!b.current && !b.worktree && (
                  <span className="row-actions inline">
                    <IconButton size="sm" variant="ghost" aria-label="Delete branch" title="Delete branch" onClick={(e) => (e.stopPropagation(), void deleteBranch(b))}><X /></IconButton>
                  </span>
                )}
              </div>
            ))}

            {remotes.length > 0 && <div className="popup-section">Remote</div>}
            {remotes.map((b) => (
              <div key={b.name} className="quick-item" onClick={() => void checkout(b)}>
                <span className="file-name">{b.name}</span>
              </div>
            ))}
          </div>
        </>
      )}
    </Modal>
  )
}

function PopupAction({ label, hint, onClick }: { label: string; hint?: string; onClick: () => void }) {
  return (
    <div className="quick-item action" onClick={onClick}>
      <span>{label}</span>
      {hint && <span className="muted path-hint">{hint}</span>}
    </div>
  )
}
