import { useEffect, useMemo, useRef, useState } from 'react'
import { confirmRollback, useGit, type FileChange, type GitStatus } from '../../store/git'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { rootLabels } from '../../lib/workspaceRoots'
import { showToast } from '../../store/dialog'
import { resolveConflictsWithAgent, suggestCommitMessage, useAgent } from '../../store/agent'
import { errorMessage } from '../../api/backend'
import { conflictHint } from '../../lib/customAgents'
import { Select } from '../ui/Select'
import { openStashDialog } from '../../store/stash'
import { StashSection } from './StashSection'
import { ReviewSection } from '../review/ReviewSection'
import { reviewChanges, useAgentReview } from '../../store/agentReview'
import { formatKeys } from '../../lib/commands'
import { modKey } from '../../lib/platform'
import { Icon } from '../ui/Icon'
import { openMergeEditor } from '../../store/mergeEditor'

const KIND_LETTER: Record<string, string> = {
  modified: 'M', added: 'A', deleted: 'D', renamed: 'R', copied: 'C',
  typechange: 'T', untracked: '?', conflicted: '!',
}

interface Group {
  key: string
  title: string
  files: FileChange[]
}

/** A repository the panel lists: the shown one alone, or every one of the workspace. */
interface Section {
  folder: string
  root: string
  status: GitStatus
}

function groupsOf(status: GitStatus | null): Group[] {
  const files = status?.files ?? []
  return [
    { key: 'conflicts', title: 'Merge Conflicts', files: files.filter((f) => f.kind === 'conflicted') },
    { key: 'changes', title: 'Changes', files: files.filter((f) => f.kind !== 'conflicted' && f.kind !== 'untracked') },
    { key: 'untracked', title: 'Unversioned Files', files: files.filter((f) => f.kind === 'untracked') },
  ].filter((g) => g.files.length > 0)
}

/** IntelliJ-style Commit tool window: tick changes, write a message, commit (and push). */
export function CommitPanel({ focusToken }: { focusToken: number }) {
  // Field by field: the store also carries counters bumped on every git event.
  const root = useGit((s) => s.root)
  const folder = useGit((s) => s.folder)
  const isRepo = useGit((s) => s.isRepo)
  const status = useGit((s) => s.status)
  const repos = useGit((s) => s.repos)
  const message = useGit((s) => s.message)
  const amend = useGit((s) => s.amend)
  const busy = useGit((s) => s.busy)
  // isIncluded reads these; subscribing re-renders the panel when a file is ticked.
  useGit((s) => s.excluded)
  useGit((s) => s.includedUntracked)
  const { setMessage, setAmend, isIncluded, commit, remote, refreshAll } = useGit.getState()
  const roots = useWorkspace((s) => s.roots)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [generating, setGenerating] = useState(false)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const agents = useAgent()
  const reviewing = useAgentReview((s) => !!s.running)
  useFocus(textarea, focusToken)
  useEffect(() => {
    if (!agents.loaded) void agents.load()
  }, [agents])

  // Several repositories: all of them, the shown one with its freshest status.
  const multi = repos.length > 1
  const sections = useMemo<Section[]>(() => {
    if (multi) return repos.map((r) => ({ folder: r.folder, root: r.root, status: r.root === root ? (status ?? r.status) : r.status }))
    return root && status ? [{ folder: folder ?? root, root, status }] : []
  }, [multi, repos, root, folder, status])
  const toggle = (key: string) => setCollapsed((c) => ({ ...c, [key]: !c[key] }))

  if (!multi && (!root || !isRepo)) {
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
  const tickedIn = (sec: Section) => (sec.status.files ?? []).filter((f) => f.kind !== 'conflicted' && isIncluded(f, sec.root))
  // Amend only rewrites the shown repository's last commit (store/git.ts commitTargets).
  const committing = amend ? sections.filter((sec) => sec.root === root) : sections
  const selected = committing.flatMap(tickedIn)
  const canCommit = !busy && message.trim() !== '' && (selected.length > 0 || amend)
  const anyChanges = sections.some((sec) => (sec.status.files ?? []).length > 0)
  const conflicted = (status?.files ?? []).filter((f) => f.kind === 'conflicted')
  const labels = rootLabels(roots)
  const labelOfFolder = (f: string) => labels[roots.indexOf(f)] ?? f.slice(f.lastIndexOf('/') + 1)

  // Generates a message for exactly what is ticked (in the shown repository, else the first ticked one).
  const generateMessage = async () => {
    const sec = sections.find((x) => x.root === root && tickedIn(x).length > 0) ?? sections.find((x) => tickedIn(x).length > 0)
    if (!sec) {
      showToast('Tick the files to describe first', 'error')
      return
    }
    const ticked = tickedIn(sec)
    const tracked = ticked.filter((f) => f.kind !== 'untracked').flatMap((f) => (f.origPath ? [f.path, f.origPath] : [f.path]))
    const untracked = ticked.filter((f) => f.kind === 'untracked').map((f) => f.path)
    setGenerating(true)
    try {
      setMessage(await suggestCommitMessage(sec.root, tracked, untracked))
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
        <button className="icon" title="Refresh" onClick={() => void refreshAll()}>↻</button>
        <button className="icon" title="Fetch" disabled={!!busy || !isRepo} onClick={() => void remote('fetch')}>⇣</button>
        <button className="icon" title="Update project (pull)" disabled={!!busy || !isRepo} onClick={() => void remote('pull')}>⤓</button>
        <button className="icon" title={`Push (${formatKeys('Mod+Shift+K')})`} disabled={!!busy || !isRepo} onClick={() => void remote('push')}>⤒</button>
        <button className="icon" title="Stash changes…" disabled={!!busy || !isRepo} onClick={openStashDialog}>⊟</button>
      </div>
      {isRepo && status && (
        <div className="git-branch-line muted" title={multi ? 'Branch of the repository the toolbar acts on (click a repository below to switch)' : undefined}>
          {multi && folder && <span className="repo-name">{labelOfFolder(folder)} · </span>}
          <BranchLine status={status} />
          {busy && <span className="busy"> · {busy}…</span>}
        </div>
      )}
      {status?.operation && root && (
        <div className="git-banner">
          <span>{capitalize(status.operation)} in progress{conflicted.length ? ` · ${conflicted.length} conflict(s)` : ''}</span>
          {conflicted.length > 0 && agents.conflictAgent && (
            <button
              className="link"
              onClick={() => void resolveConflictsWithAgent(root, status.operation, conflicted.map((f) => f.path))}
              title={conflictHint(agents.preferred, agents.conflictAgent)}
            >
              Resolve with {agents.conflictAgent}
            </button>
          )}
        </div>
      )}

      <div className="tree scroll changes-list">
        {!anyChanges && !multi && <div className="placeholder muted">No changes.</div>}
        {multi
          ? sections.map((sec) => (
              <RepoSection
                key={sec.root}
                section={sec}
                label={labelOfFolder(sec.folder)}
                current={sec.root === root}
                collapsed={collapsed}
                toggle={toggle}
              />
            ))
          : sections.map((sec) => <ChangeGroups key={sec.root} section={sec} collapsed={collapsed} toggle={toggle} />)}
        {root && isRepo && <ReviewSection root={root} />}
        {isRepo && <StashSection />}
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
            disabled={!agents.preferred || reviewing || (status?.files ?? []).length === 0}
            onClick={() => void reviewChanges()}
            title="Let the agent review the ticked changes (or all of them: Settings ▸ Agents ▸ Review) before you commit"
          >
            {reviewing ? 'Reviewing…' : '✦ Review'}
          </button>
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
            if (e.key === 'Enter' && modKey(e) && canCommit) {
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
          <span className="muted count" title={multi ? 'One commit per repository with ticked files, same message' : undefined}>
            {selected.length} selected{multi ? (amend ? ' (amend: this repo only)' : reposNote(sections, tickedIn)) : ''}
          </span>
          <button className="primary" disabled={!canCommit} onClick={() => void commit(false)} title={formatKeys('Mod+Enter')}>
            {busy === 'commit' ? 'Committing…' : 'Commit'}
          </button>
          <button disabled={!canCommit} onClick={() => void commit(true)} title={formatKeys('Alt+Mod+Enter')}>
            Commit and Push
          </button>
        </div>
      </div>
    </div>
  )
}

/** " in 2 repos" when the ticked files span several repositories. */
function reposNote(sections: Section[], tickedIn: (s: Section) => FileChange[]): string {
  const n = sections.filter((s) => tickedIn(s).length > 0).length
  return n > 1 ? ` in ${n} repos` : ''
}

function BranchLine({ status }: { status: GitStatus }) {
  return (
    <>
      <Icon name="branch" size={12} /> {status.branch || `detached ${status.head.slice(0, 7)}`}
      {status.upstream && <span> → {status.upstream}</span>}
      {!!status.ahead && <span className="ahead"> ↑{status.ahead}</span>}
      {!!status.behind && <span className="behind"> ↓{status.behind}</span>}
    </>
  )
}

/** One repository of several: a header (tick all, branch, count; click to make it the shown one) and its changes. */
function RepoSection({
  section, label, current, collapsed, toggle,
}: { section: Section; label: string; current: boolean; collapsed: Record<string, boolean>; toggle: (key: string) => void }) {
  const { isIncluded, setGroup, load } = useGit.getState()
  const files = section.status.files ?? []
  const tickable = files.filter((f) => f.kind !== 'conflicted')
  const included = tickable.filter((f) => isIncluded(f, section.root)).length
  const key = `repo:${section.root}`
  const open = !collapsed[key]
  return (
    <div className={`repo-section${current ? ' current' : ''}`}>
      <div
        className="tree-row static group-row repo-row"
        title={current ? `${section.root} (the toolbar and branch line act on this repository)` : `${section.root}\nClick to make the toolbar act on this repository`}
        onClick={() => {
          if (!current) void load(section.folder)
          else toggle(key)
        }}
      >
        <span className="twisty" onClick={(e) => (e.stopPropagation(), toggle(key))}>{open ? '▾' : '▸'}</span>
        {tickable.length > 0 && (
          <input
            type="checkbox"
            checked={included === tickable.length}
            ref={(el) => {
              if (el) el.indeterminate = included > 0 && included < tickable.length
            }}
            onClick={(e) => e.stopPropagation()}
            onChange={(e) => setGroup(tickable, e.target.checked, section.root)}
          />
        )}
        <span className="group-title repo-title">{label}</span>
        <span className="muted repo-branch">
          <BranchLine status={section.status} />
        </span>
        {files.length > 0 && <span className="badge">{files.length}</span>}
      </div>
      {open &&
        (files.length > 0 ? (
          <div className="repo-changes">
            <ChangeGroups section={section} collapsed={collapsed} toggle={toggle} />
          </div>
        ) : (
          <div className="repo-changes muted repo-clean">No changes</div>
        ))}
    </div>
  )
}

/** The conflicts / changes / unversioned groups of one repository. */
function ChangeGroups({ section, collapsed, toggle }: { section: Section; collapsed: Record<string, boolean>; toggle: (key: string) => void }) {
  const { root, status } = section
  const { isIncluded, setGroup, toggleFile } = useGit.getState()
  const openDiff = useEditor((s) => s.openDiff)
  const openFile = useEditor((s) => s.openFile)
  const agents = useAgent()
  const groups = groupsOf(status)
  const abs = (f: FileChange) => `${root}/${f.path}`
  const resolve = (paths: string[]) => void resolveConflictsWithAgent(root, status.operation ?? '', paths)
  return (
    <>
      {groups.map((g) => {
        const included = g.files.filter((f) => isIncluded(f, root)).length
        const key = `${root}:${g.key}`
        return (
          <div key={g.key}>
            <div className="tree-row static group-row" onClick={() => toggle(key)}>
              <span className="twisty">{collapsed[key] ? '▸' : '▾'}</span>
              {g.key !== 'conflicts' && (
                <input
                  type="checkbox"
                  checked={included === g.files.length}
                  ref={(el) => {
                    if (el) el.indeterminate = included > 0 && included < g.files.length
                  }}
                  onClick={(e) => e.stopPropagation()}
                  onChange={(e) => setGroup(g.files, e.target.checked, root)}
                />
              )}
              <span className="group-title">{g.title}</span>
              {g.key === 'conflicts' && agents.conflictAgent && (
                <button
                  className="link agent-action"
                  title={conflictHint(agents.preferred, agents.conflictAgent)}
                  onClick={(e) => (e.stopPropagation(), resolve(g.files.map((f) => f.path)))}
                >
                  ✦ Resolve with {agents.conflictAgent}
                </button>
              )}
              <span className="badge">{g.files.length}</span>
            </div>
            {!collapsed[key] &&
              g.files.map((f) => (
                <div
                  key={g.key + f.path}
                  className="tree-row static change-row"
                  title={f.origPath ? `${f.origPath} → ${f.path}` : f.path}
                  onClick={() => (f.kind === 'conflicted' ? openMergeEditor(root, abs(f)) : openDiff(root, abs(f), f.origPath ? `${root}/${f.origPath}` : abs(f)))}
                  onDoubleClick={() => openFile(abs(f))}
                >
                  {g.key !== 'conflicts' && (
                    <input type="checkbox" checked={isIncluded(f, root)} onClick={(e) => e.stopPropagation()} onChange={() => toggleFile(f, root)} />
                  )}
                  <span className={`change-kind ${f.kind}`}>{KIND_LETTER[f.kind] ?? '?'}</span>
                  <span className={`file-name change-${f.kind}`}>{f.path.split('/').pop()}</span>
                  <span className="muted path-hint">{dirOf(f.path)}</span>
                  {!!f.conflicts && <span className="conflict-count" title="Conflicts left in the file">{f.conflicts}</span>}
                  <span className="row-actions" onClick={(e) => e.stopPropagation()}>
                    {f.kind === 'conflicted' && (
                      <>
                        <button className="link" title="Resolve in the merge editor" onClick={() => openMergeEditor(root, abs(f))}>Resolve…</button>
                        {agents.conflictAgent && (
                          <button className="icon" title={`Resolve with ${agents.conflictAgent} in a terminal`} onClick={() => resolve([f.path])}>
                            ✦
                          </button>
                        )}
                      </>
                    )}
                    <button className="icon" title="Open file" onClick={() => openFile(abs(f))}>↗</button>
                    {f.kind !== 'conflicted' && (
                      <button className="icon" title="Rollback" onClick={() => void confirmRollback([f], root)}>↶</button>
                    )}
                  </span>
                </div>
              ))}
          </div>
        )
      })}
    </>
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
