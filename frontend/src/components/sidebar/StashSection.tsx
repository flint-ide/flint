import { useEffect, useState } from 'react'
import { Button, Checkbox, IconButton, Input } from '@flint-ide/ui'
import { X } from '@flint-ide/ui/icons'
import { useGit } from '../../store/git'
import {
  applyStash,
  closeStashDialog,
  dropStash,
  loadStashes,
  openStashFileDiff,
  popStash,
  stashChanges,
  toggleStashFiles,
  useStash,
} from '../../store/stash'
import { formatDateTime } from '../../lib/blame'
import { Modal } from '../layout/Modal'

const KIND_LETTER: Record<string, string> = {
  modified: 'M', added: 'A', deleted: 'D', renamed: 'R', copied: 'C', typechange: 'T', untracked: 'U',
}

/** Collapsible list of stashes at the bottom of the Commit panel, plus the stash dialog. */
export function StashSection() {
  const root = useGit((s) => s.root)
  const refsVersion = useGit((s) => s.refsVersion)
  const { stashes, files, busy, dialogOpen } = useStash()
  const [collapsed, setCollapsed] = useState(false)

  // Stashing moves refs/stash, which bumps refsVersion (git:changed).
  useEffect(() => void loadStashes(), [root, refsVersion])

  return (
    <>
      {stashes.length > 0 && (
        <div>
          <div className="tree-row static group-row" onClick={() => setCollapsed(!collapsed)}>
            <span className="twisty">{collapsed ? '▸' : '▾'}</span>
            <span className="group-title">Stashes</span>
            <span className="badge">{stashes.length}</span>
          </div>
          {!collapsed &&
            stashes.map((s) => (
              <div key={s.hash}>
                <div
                  className="tree-row static change-row stash-row"
                  title={`stash@{${s.index}} · ${s.hash.slice(0, 7)} · ${formatDateTime(s.time)}${s.untracked ? '\nincludes untracked files' : ''}`}
                  onClick={() => void toggleStashFiles(s.hash)}
                >
                  <span className="twisty">{files[s.hash] ? '▾' : '▸'}</span>
                  <span className="file-name">{stashLabel(s.message)}</span>
                  <span className="muted path-hint">{formatDateTime(s.time)}</span>
                  <span className="row-actions" onClick={(e) => e.stopPropagation()}>
                    <Button size="sm" variant="ghost" disabled={busy} title="Apply and keep the stash" onClick={() => void applyStash(s.hash)}>
                      Apply
                    </Button>
                    <Button size="sm" variant="ghost" disabled={busy} title="Apply and drop the stash" onClick={() => void popStash(s.hash)}>
                      Pop
                    </Button>
                    <IconButton size="sm" variant="ghost" aria-label="Drop" disabled={busy} title="Drop" onClick={() => void dropStash(s.hash)}><X /></IconButton>
                  </span>
                </div>
                {files[s.hash]?.map((f) => (
                  <div
                    key={f.path}
                    className="tree-row static change-row indent-2"
                    title={f.origPath ? `${f.origPath} → ${f.path}` : f.path}
                    onClick={() => openStashFileDiff(s.hash, f)}
                  >
                    <span className={`change-kind ${f.kind}`}>{KIND_LETTER[f.kind] ?? '?'}</span>
                    <span className={`file-name change-${f.kind}`}>{f.path.split('/').pop()}</span>
                    <span className="muted path-hint">{f.path.includes('/') ? f.path.slice(0, f.path.lastIndexOf('/')) : ''}</span>
                  </div>
                ))}
              </div>
            ))}
        </div>
      )}
      {dialogOpen && <StashDialog busy={busy} />}
    </>
  )
}

/** "On main: wip" → "wip (main)"; git's default "WIP on main: abc123 subject" stays as is. */
function stashLabel(message: string): string {
  const m = /^On ([^:]+): (.*)$/.exec(message)
  return m ? `${m[2]} (${m[1]})` : message
}

function StashDialog({ busy }: { busy: boolean }) {
  const [message, setMessage] = useState('')
  const [untracked, setUntracked] = useState(false)
  const submit = () => void stashChanges(message, untracked)
  return (
    <Modal title="Stash Changes" onClose={closeStashDialog}>
      <div className="form">
        <label>
          Message
          <Input
            autoFocus
            value={message}
            placeholder="Optional"
            spellCheck={false}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && !busy && submit()}
          />
        </label>
        <label className="check">
          <Checkbox checked={untracked} onCheckedChange={setUntracked} /> Include untracked files
        </label>
        <div className="form-actions">
          <span className="spacer" />
          <Button variant="plain" onClick={closeStashDialog}>Cancel</Button>
          <Button variant="primary" disabled={busy} onClick={submit}>
            {busy ? 'Stashing…' : 'Stash'}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
