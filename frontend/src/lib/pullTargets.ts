// "Update Repository" in a workspace of several repositories: which ones the
// picker offers and what the toast says afterwards. Pure, so it is testable
// with node:test; store/git.ts wires it.
import { rootLabels } from './workspaceRoots'

/** What the picker needs of a repository (store/git.ts RepoView). */
interface Repo {
  folder: string
  root: string
  status: { branch: string; head: string; upstream: string; behind: number }
}

/** A repository the picker offers. */
export interface PullTarget {
  /** Work-tree root, what Pull runs in. */
  root: string
  /** The workspace folder's display name. */
  label: string
  /** "main", or "detached 0123abc". */
  branch: string
  /** Commits known to be behind the upstream (as of the last fetch). */
  behind: number
  /** No upstream branch: pull would fail. */
  noUpstream: boolean
  /** The repository the Commit panel shows; preselected. */
  current: boolean
}

/** The repositories to choose from, in folder order. Fewer than two: there is nothing to ask. */
export function pullTargets(repos: readonly Repo[], roots: string[], current: string | null): PullTarget[] {
  if (repos.length < 2) return []
  const labels = rootLabels(roots)
  return repos.map((r) => ({
    root: r.root,
    label: labels[roots.indexOf(r.folder)] ?? r.folder.slice(r.folder.lastIndexOf('/') + 1),
    branch: r.status.branch || `detached ${r.status.head.slice(0, 7)}`,
    behind: r.status.behind,
    noUpstream: !r.status.upstream,
    current: r.root === current,
  }))
}

/** Next to a target's name: "main · ↓3", "main · no upstream". */
export function pullHint(t: PullTarget): string {
  if (t.noUpstream) return `${t.branch} · no upstream`
  return t.behind > 0 ? `${t.branch} · ↓${t.behind}` : t.branch
}

/** Git's errors run over many lines (hints, file lists); a toast names the cause only. */
function firstLine(error: string): string {
  const line = (error.split('\n').find((l) => l.trim()) ?? '').trim()
  return line.length > 200 ? `${line.slice(0, 199)}…` : line
}

/** A repository whose pull failed. */
export interface PullFailure {
  label: string
  error: string
}

/** The toast after pulling: what was updated, and every failure with its repository. */
export function pullSummary(updated: string[], failed: PullFailure[]): { message: string; kind: 'info' | 'error' } {
  const done = updated.length === 1 ? `Updated ${updated[0]}` : `Updated ${updated.length} repositories`
  if (failed.length === 0) return { message: updated.length ? done : 'Nothing to update', kind: 'info' }
  const errors = failed.map((f) => `${f.label}: ${firstLine(f.error)}`).join('\n')
  return { message: updated.length ? `${done}. Failed:\n${errors}` : `Update failed.\n${errors}`, kind: 'error' }
}
