import { useEffect, useMemo, useRef, useState } from 'react'
import { confirmRollback, useGit, type FileChange } from '../../store/git'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { rootLabels } from '../../lib/workspaceRoots'
import { showToast } from '../../store/dialog'
import { resolveConflictsWithAgent, suggestCommitMessage, useAgent } from '../../store/agent'
import { errorMessage } from '../../api/backend'
import { Select } from '../ui/Select'
import { openStashDialog } from '../../store/stash'
import { StashSection } from './StashSection'

const KIND_LETTER: Record<string, string> = {
  modified: 'M', added: 'A', deleted: 'D', renamed: 'R', copied: 'C',
  typechange: 'T', untracked: '?', conflicted: '!',
}

interface Group {
  key: string
  title: string
  files: FileChange[]
}

/** IntelliJ-style Commit tool window: tick changes, write a message, commit (and push). */
export function CommitPanel({ focusToken }: { focusToken: number }) {
  // Field by field: the store also carries counters bumped on every git event.
  const root = useGit((s) => s.root)
  const isRepo = useGit((s) => s.isRepo)
  const status = useGit((s) => s.status)
  const message = useGit((s) => s.message)
  const amend = useGit((s) => s.amend)
  const busy = useGit((s) => s.busy)
  // isIncluded reads these; subscribing re-renders the panel when a file is ticked.
  useGit((s) => s.excluded)
  useGit((s) => s.includedUntracked)
  const { setMessage, setAmend, toggleFile, setGroup, isIncluded, commit, remote, refresh } = useGit.getState()
  const openDiff = useEditor((s) => s.openDiff)
  const openFile = useEditor((s) => s.openFile)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [generating, setGenerating] = useState(false)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const agents = useAgent()
  useFocus(textarea, focusToken)
  useEffect(() => {
    if (!agents.loaded) void agents.load()
  }, [agents])

  const groups = useMemo<Group[]>(() => {
    const files = status?.files ?? []
    return [
      { key: 'conflicts', title: 'Merge Conflicts', files: files.filter((f) => f.kind === 'conflicted') },
      { key: 'changes', title: 'Changes', files: files.filter((f) => f.kind !== 'conflicted' && f.kind !== 'untracked') },
      { key: 'untracked', title: 'Unversioned Files', files: files.filter((f) => f.kind === 'untracked') },
    ].filter((g) => g.files.length > 0)
  }, [status])

  if (!root || !isRepo) {
    return (
      <div className="panel commit-panel">
        <div className="panel-header">
          <RepoSwitcher fallback="Commit" />
        </div>
        <div className="placeholder muted">Not a git repository.</div>
      </div>
    )
  }

  // Recomputed each render so ticking a file updates the count (isIncluded reads the store).
  const selected = (status?.files ?? []).filter((f) => f.kind !== 'conflicted' && isIncluded(f))
  const canCommit = !busy && message.trim() !== '' && (selected.length > 0 || amend)
  const abs = (f: FileChange) => `${root}/${f.path}`

  const conflicted = (status?.files ?? []).filter((f) => f.kind === 'conflicted')
  const resolveWithAgent = () =>
    void resolveConflictsWithAgent(root, status?.operation ?? '', conflicted.map((f) => f.path))

  // Generates a message for exactly what is ticked; the user edits it before committing.
  const generateMessage = async () => {
    const tracked = selected.filter((f) => f.kind !== 'untracked').flatMap((f) => (f.origPath ? [f.path, f.origPath] : [f.path]))
    const untracked = selected.filter((f) => f.kind === 'untracked').map((f) => f.path)
    if (tracked.length + untracked.length === 0) {
      showToast('Tick the files to describe first', 'error')
      return
    }
    setGenerating(true)
    try {
      setMessage(await suggestCommitMessage(root, tracked, untracked))
      textarea.current?.focus()
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      setGenerating(false)
    }
  }

  return (
    <div className="panel commit-panel">
      <div className="panel-header">
        <RepoSwitcher fallback="Commit" />
        <button className="icon" title="Refresh" onClick={() => void refresh()}>↻</button>
        <button className="icon" title="Fetch" disabled={!!busy} onClick={() => void remote('fetch')}>⇣</button>
        <button className="icon" title="Update project (pull)" disabled={!!busy} onClick={() => void remote('pull')}>⤓</button>
        <button className="icon" title="Push (⇧⌘K)" disabled={!!busy} onClick={() => void remote('push')}>⤒</button>
        <button className="icon" title="Stash changes…" disabled={!!busy} onClick={openStashDialog}>⊟</button>
      </div>
      <div className="git-branch-line muted">
        ⎇ {status?.branch || `detached ${status?.head.slice(0, 7) ?? ''}`}
        {status?.upstream && <span> → {status.upstream}</span>}
        {!!status?.ahead && <span className="ahead"> ↑{status.ahead}</span>}
        {!!status?.behind && <span className="behind"> ↓{status.behind}</span>}
        {busy && <span className="busy"> · {busy}…</span>}
      </div>
      {status?.operation && (
        <div className="git-banner">
          <span>{capitalize(status.operation)} in progress{conflicted.length ? ` · ${conflicted.length} conflict(s)` : ''}</span>
          {conflicted.length > 0 && agents.preferred && (
            <button className="link" onClick={resolveWithAgent}>Resolve with {agents.preferred}</button>
          )}
        </div>
      )}

      <div className="tree scroll changes-list">
        {groups.length === 0 && <div className="placeholder muted">No changes.</div>}
        {groups.map((g) => {
          const included = g.files.filter(isIncluded).length
          return (
            <div key={g.key}>
              <div className="tree-row static group-row" onClick={() => setCollapsed({ ...collapsed, [g.key]: !collapsed[g.key] })}>
                <span className="twisty">{collapsed[g.key] ? '▸' : '▾'}</span>
                {g.key !== 'conflicts' && (
                  <input
                    type="checkbox"
                    checked={included === g.files.length}
                    ref={(el) => {
                      if (el) el.indeterminate = included > 0 && included < g.files.length
                    }}
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => setGroup(g.files, e.target.checked)}
                  />
                )}
                <span className="group-title">{g.title}</span>
                {g.key === 'conflicts' && agents.preferred && (
                  <button
                    className="link agent-action"
                    title={`Start ${agents.preferred} in a new terminal to resolve these conflicts`}
                    onClick={(e) => (e.stopPropagation(), resolveWithAgent())}
                  >
                    ✦ Resolve with {agents.preferred}
                  </button>
                )}
                <span className="badge">{g.files.length}</span>
              </div>
              {!collapsed[g.key] &&
                g.files.map((f) => (
                  <div
                    key={g.key + f.path}
                    className="tree-row static change-row"
                    title={f.origPath ? `${f.origPath} → ${f.path}` : f.path}
                    onClick={() => (f.kind === 'conflicted' ? openFile(abs(f)) : openDiff(root, abs(f), f.origPath ? `${root}/${f.origPath}` : abs(f)))}
                    onDoubleClick={() => openFile(abs(f))}
                  >
                    {g.key !== 'conflicts' && (
                      <input type="checkbox" checked={isIncluded(f)} onClick={(e) => e.stopPropagation()} onChange={() => toggleFile(f)} />
                    )}
                    <span className={`change-kind ${f.kind}`}>{KIND_LETTER[f.kind] ?? '?'}</span>
                    <span className={`file-name change-${f.kind}`}>{f.path.split('/').pop()}</span>
                    <span className="muted path-hint">{dirOf(f.path)}</span>
                    <span className="row-actions" onClick={(e) => e.stopPropagation()}>
                      <button className="icon" title="Open file" onClick={() => openFile(abs(f))}>↗</button>
                      {f.kind !== 'conflicted' && (
                        <button className="icon" title="Rollback" onClick={() => void confirmRollback([f])}>↶</button>
                      )}
                    </span>
                  </div>
                ))}
            </div>
          )
        })}
        <StashSection />
      </div>

      <div className="commit-box">
        <div className="commit-box-tools">
          {agents.available.length > 1 ? (
            <Select
              size="sm"
              value={agents.preferred}
              options={agents.available.map((a) => ({ value: a, label: a }))}
              onChange={(a) => agents.setPreferred(a)}
              title="Agent used for generating messages and resolving conflicts"
            />
          ) : (
            <span className="muted">{agents.preferred || 'no agent installed'}</span>
          )}
          <span className="spacer" />
          <button
            className="link"
            disabled={!agents.preferred || generating || selected.length === 0}
            onClick={() => void generateMessage()}
            title="Let the agent write a message for the ticked changes"
          >
            {generating ? 'Generating…' : '✦ Generate message'}
          </button>
        </div>
        <textarea
          ref={textarea}
          value={message}
          placeholder="Commit message"
          spellCheck={false}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && e.metaKey && canCommit) {
              e.preventDefault()
              void commit(e.altKey)
            }
          }}
        />
        <div className="commit-actions">
          <label className="check">
            <input type="checkbox" checked={amend} onChange={(e) => void setAmend(e.target.checked)} /> Amend
          </label>
          <span className="spacer" />
          <span className="muted count">{selected.length} selected</span>
          <button className="primary" disabled={!canCommit} onClick={() => void commit(false)} title="⌘↵">
            {busy === 'commit' ? 'Committing…' : 'Commit'}
          </button>
          <button disabled={!canCommit} onClick={() => void commit(true)} title="⌥⌘↵">
            Commit and Push
          </button>
        </div>
      </div>
    </div>
  )
}

/** Focuses the message box whenever ⌘K asks for it (token changes). */
function useFocus(ref: React.RefObject<HTMLTextAreaElement | null>, token: number) {
  useEffect(() => {
    if (token > 0) ref.current?.focus()
  }, [ref, token])
}

function dirOf(path: string): string {
  const i = path.lastIndexOf('/')
  return i < 0 ? '' : path.slice(0, i)
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

/**
 * With several workspace folders: which one's repository the panel shows (it follows
 * the active tab's folder), with the number of changes in each.
 */
function RepoSwitcher({ fallback }: { fallback: string }) {
  const roots = useWorkspace((s) => s.roots)
  const folder = useGit((s) => s.folder)
  const counts = useGit((s) => s.counts)
  if (roots.length < 2 || !folder) return <span className="panel-title">{fallback}</span>
  const labels = rootLabels(roots)
  return (
    <span className="panel-title repo-switcher">
      <Select
        size="sm"
        title="Repository shown in the Commit panel (follows the active file)"
        value={folder}
        options={roots.map((r, i) => ({
          value: r,
          label: labels[i],
          hint: r in counts ? `${counts[r]} change${counts[r] === 1 ? '' : 's'}` : 'not a repository',
        }))}
        onChange={(r) => void useGit.getState().load(r)}
      />
    </span>
  )
}
