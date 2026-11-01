// Review Changes with Agent: runs the review (Go: AgentAPI.ReviewChanges) on the
// changes the Commit panel shows, keeps the findings placed on their lines as
// the files change (lib/agentReview.ts), and hands findings to the agent chat.
import * as Agent from '../../bindings/flint/agentapi'
import { ReviewRequest } from '../../bindings/flint/models'
import { Workspace, errorMessage } from '../api/backend'
import { askAgent } from '../lib/askAgent'
import { createStore } from '../lib/store'
import {
  countSummary, findingAttachments, findingPrompt, findingsPrompt, fixAllSelection, placed, reanchorPaths, sortFindings, splitLines,
  type Finding, type PlacedFinding,
} from '../lib/agentReview'
import { showToast } from './dialog'
import { useEditor } from './editor'
import { onFileChanged } from './fsEvents'
import { useGit, type FileChange } from './git'
import { useAgent } from './agent'
import { useSettings } from './settings'

interface Running {
  root: string
  agent: string
  files: number
  startedAt: number
}

interface AgentReviewState {
  /** The review in progress, if any (one at a time). */
  running: Running | null
  /** Repository the findings belong to. */
  root: string | null
  agent: string
  /** Paths reviewed (repo-relative). */
  files: string[]
  /** Not dismissed, sorted by file and line. */
  findings: PlacedFinding[]
  /** What was left out or shortened, from Go. */
  notes: string[]
  error: string
  /** When the last review finished (0: none yet). */
  doneAt: number
  /** A diff tab should scroll to this line (abs path, 1-based line). */
  reveal: { path: string; line: number; token: number } | null
}

export const useAgentReview = createStore<AgentReviewState>(() => ({
  running: null,
  root: null,
  agent: '',
  files: [],
  findings: [],
  notes: [],
  error: '',
  doneAt: 0,
  reveal: null,
}))

/** The running call, cancelled by cancelReview (a Wails CancellablePromise). */
let inflight: { cancel: () => void; cancelled: boolean } | null = null
/** A rename's source by path, for opening its diff. */
let origPaths: Record<string, string> = {}

/** The changes a review covers: ticked ones (or all, settings), plus `extra` (repo-relative). */
function scopeFiles(extra?: string): FileChange[] {
  const { status, isIncluded } = useGit.getState()
  const all = (status?.files ?? []).filter((f) => f.kind !== 'conflicted')
  const scope = useSettings.getState().settings.agentReview.scope
  const files = scope === 'all' ? all : all.filter(isIncluded)
  const added = extra ? all.find((f) => f.path === extra) : undefined
  return added && !files.includes(added) ? [...files, added] : files
}

/**
 * Asks the review agent about the changes in the Commit panel's scope; `extraPath`
 * (absolute, from a diff tab) is reviewed too even when it isn't ticked.
 */
export async function reviewChanges(extraPath?: string): Promise<void> {
  const { root } = useGit.getState()
  if (!root) return showToast('Not a git repository', 'error')
  if (useAgentReview.getState().running) return showToast('A review is already running')
  const extra = extraPath?.startsWith(root + '/') ? extraPath.slice(root.length + 1) : undefined
  const files = scopeFiles(extra)
  if (files.length === 0) {
    const scope = useSettings.getState().settings.agentReview.scope
    return showToast(scope === 'all' ? 'No changes to review' : 'Tick the changes to review in the Commit panel first', 'error')
  }
  const agent = useSettings.getState().settings.agentReview.agent || useAgent.getState().preferred
  origPaths = Object.fromEntries(files.filter((f) => f.origPath).map((f) => [f.path, f.origPath]))
  const call = Agent.ReviewChanges(
    ReviewRequest.createFrom({ root, files: files.map((f) => ({ path: f.path, origPath: f.origPath, kind: f.kind })) }),
  )
  const run = { cancel: () => call.cancel(), cancelled: false }
  inflight = run
  useAgentReview.setState({ running: { root, agent, files: files.length, startedAt: Date.now() }, error: '' })
  try {
    const res = await call
    if (run.cancelled) return
    const findings = sortFindings((res.findings ?? []).map((f) => placed(f as Finding)))
    const reviewed = res.files ?? []
    useAgentReview.setState({ root, agent: res.agent, files: reviewed, findings, notes: res.notes ?? [], error: '', doneAt: Date.now() })
    watchFiles()
    // Files edited while the agent worked: place the findings on today's lines.
    void replace(root, [...new Set(findings.map((f) => f.path))])
    if (findings.length === 0) showToast(`${res.agent}: no problems found in ${reviewed.length} file${reviewed.length === 1 ? '' : 's'}`)
  } catch (err) {
    if (run.cancelled) return
    useAgentReview.setState({ error: errorMessage(err), doneAt: Date.now() })
  } finally {
    if (inflight === run) {
      inflight = null
      useAgentReview.setState({ running: null })
    }
  }
}

/** Stops the running review; Go kills the agent and what it started. */
export function cancelReview(): void {
  const run = inflight
  if (!run) return
  run.cancelled = true
  inflight = null
  run.cancel()
  useAgentReview.setState({ running: null })
  showToast('Review cancelled')
}

export function dismissFinding(id: string): void {
  useAgentReview.setState((s) => ({ findings: s.findings.filter((f) => f.id !== id) }))
  watchFiles()
}

/** Forgets the findings, notes and error. */
export function clearReview(): void {
  useAgentReview.setState({ root: null, files: [], findings: [], notes: [], error: '', doneAt: 0 })
  watchFiles()
}

const absPath = (root: string, rel: string) => `${root}/${rel}`

/** Opens the finding's file in a diff tab, scrolled to its line. */
export function revealFinding(id: string): void {
  const { findings, root } = useAgentReview.getState()
  const f = findings.find((x) => x.id === id)
  if (!f || !root) return
  const path = absPath(root, f.path)
  useEditor.getState().openDiff(root, path, origPaths[f.path] ? absPath(root, origPaths[f.path]) : path)
  if (f.at > 0) useAgentReview.setState({ reveal: { path, line: f.at, token: Date.now() } })
}

/** Puts one finding in the agent chat's draft, with the code around it; the user sends it. */
export async function fixFinding(id: string): Promise<void> {
  const f = useAgentReview.getState().findings.find((x) => x.id === id)
  if (f) await handToChat([f], findingPrompt(f))
}

/** Puts the findings (the worst MAX_FIX_ALL) in one draft. */
export async function fixAllFindings(): Promise<void> {
  const { findings } = useAgentReview.getState()
  if (findings.length === 0) return
  await handToChat(fixAllSelection(findings), findingsPrompt(findings))
}

async function handToChat(findings: PlacedFinding[], prompt: string): Promise<void> {
  const root = useAgentReview.getState().root
  if (!root) return
  // No Map.groupBy: macOS 12's WebKit lacks it.
  const byPath = new Map<string, PlacedFinding[]>()
  for (const f of findings) byPath.set(f.path, [...(byPath.get(f.path) ?? []), f])
  const attachments = (
    await Promise.all(
      [...byPath].map(async ([rel, fs]) => {
        const path = absPath(root, rel)
        return findingAttachments(path, await readLines(path) ?? [], fs)
      }),
    )
  ).flat()
  await askAgent({ prompt, attachments, root })
}

async function readLines(path: string): Promise<string[] | null> {
  try {
    const f = await Workspace.ReadFile(path)
    return f.binary || f.tooLarge ? null : splitLines(f.content)
  } catch {
    return null // deleted
  }
}

// ---------- keeping findings on their lines ----------

const watchers = new Map<string, () => void>()

/** Watches the files that have findings; a change re-places their findings. */
function watchFiles(): void {
  const { root, findings } = useAgentReview.getState()
  const wanted = new Set(root ? findings.map((f) => absPath(root, f.path)) : [])
  for (const [path, off] of watchers) {
    if (!wanted.has(path)) {
      off()
      watchers.delete(path)
    }
  }
  for (const path of wanted) {
    if (!watchers.has(path) && root) {
      const rel = path.slice(root.length + 1)
      watchers.set(path, onFileChanged(path, () => void replace(root, [rel])))
    }
  }
}

/** Reads the files rels (repo-relative) and places their findings anew. */
async function replace(root: string, rels: string[]): Promise<void> {
  const read = await Promise.all(rels.map(async (rel) => [rel, await readLines(absPath(root, rel))] as const))
  const s = useAgentReview.getState()
  if (s.root !== root) return // cleared or reviewed again meanwhile
  const findings = reanchorPaths(s.findings, new Map(read))
  if (findings.some((f, i) => f !== s.findings[i])) useAgentReview.setState({ findings: sortFindings(findings) })
}

/** "Reviewing 3 files with claude…" for status lines. */
export function runningLabel(r: Running): string {
  return `Reviewing ${r.files} file${r.files === 1 ? '' : 's'} with ${r.agent || 'the agent'}…`
}

export { countSummary }
