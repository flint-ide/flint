import { useEffect, useState } from 'react'
import { Button, Checkbox, Form, FormActions, IconButton, Input, TreeRow, TreeSection } from '@flint-ide/ui'
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

  // Stashing moves refs/stash, which bumps refsVersion (git:changed).
  useEffect(() => void loadStashes(), [root, refsVersion])

  return (
    <>
      {stashes.length > 0 && (
        <TreeSection title="Stashes" count={stashes.length}>
          {stashes.map((s) => (
            <div key={s.hash}>
              <TreeRow
                depth={1}
                twisty={files[s.hash] ? 'open' : 'closed'}
                title={`stash@{${s.index}} · ${s.hash.slice(0, 7)} · ${formatDateTime(s.time)}${s.untracked ? '\nincludes untracked files' : ''}`}
                onClick={() => void toggleStashFiles(s.hash)}
                file
                label={stashLabel(s.message)}
                hint={formatDateTime(s.time)}
                actions={
                  <>
                    <Button size="sm" variant="ghost" disabled={busy} title="Apply and keep the stash" onClick={() => void applyStash(s.hash)}>
                      Apply
                    </Button>
                    <Button size="sm" variant="ghost" disabled={busy} title="Apply and drop the stash" onClick={() => void popStash(s.hash)}>
                      Pop
                    </Button>
                    <IconButton size="sm" variant="ghost" aria-label="Drop" disabled={busy} title="Drop" onClick={() => void dropStash(s.hash)}><X /></IconButton>
                  </>
                }
              />
              {files[s.hash]?.map((f) => (
                <TreeRow
                  key={f.path}
                  depth={2}
                  title={f.origPath ? `${f.origPath} → ${f.path}` : f.path}
                  onClick={() => openStashFileDiff(s.hash, f)}
                  icon={<span className={`change-kind ${f.kind}`}>{KIND_LETTER[f.kind] ?? '?'}</span>}
                  label={<span className={`change-${f.kind}`}>{f.path.split('/').pop()}</span>}
                  hint={f.path.includes('/') ? f.path.slice(0, f.path.lastIndexOf('/')) : ''}
                />
              ))}
            </div>
          ))}
        </TreeSection>
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
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          if (!busy) submit()
        }}
      >
        <Input
          label="Message"
          autoFocus
          value={message}
          placeholder="Optional"
          spellCheck={false}
          onChange={(e) => setMessage(e.target.value)}
        />
        <Checkbox label="Include untracked files" checked={untracked} onCheckedChange={setUntracked} />
        <FormActions align="end">
          <Button type="button" variant="plain" onClick={closeStashDialog}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? 'Stashing…' : 'Stash'}
          </Button>
        </FormActions>
      </Form>
    </Modal>
  )
}
