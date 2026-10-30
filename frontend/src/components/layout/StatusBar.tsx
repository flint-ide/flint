import { Suspense, lazy, useState } from 'react'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { useTerminal } from '../../store/terminal'
import { useGit } from '../../store/git'
import { useLsp } from '../../store/lsp'
import { showToast } from '../../store/dialog'

const BranchesPopup = lazy(() => import('../git/BranchesPopup').then((m) => ({ default: m.BranchesPopup })))

export function StatusBar() {
  const root = useWorkspace((s) => s.root)
  const files = useWorkspace((s) => s.files.length)
  const active = useEditor((s) => s.tabs.find((t) => t.id === s.activeId))
  const terminals = useTerminal((s) => s.tabs.length)
  const [branchesOpen, setBranchesOpen] = useState(false)

  const location =
    active?.kind === 'file' && root && active.path.startsWith(root) ? active.path.slice(root.length + 1) : active?.title

  return (
    <footer className="status-bar">
      <span className="truncate">{location ?? ''}</span>
      <span className="spacer" />
      {root && <span>{files.toLocaleString()} files</span>}
      <span>{terminals} {terminals === 1 ? 'terminal' : 'terminals'}</span>
      <LspWidget path={active?.kind === 'file' ? active.path : null} />
      <BranchWidget onClick={() => setBranchesOpen(true)} />
      {branchesOpen && (
        <Suspense fallback={null}>
          <BranchesPopup onClose={() => setBranchesOpen(false)} />
        </Suspense>
      )}
    </footer>
  )
}

/** Current branch with ahead/behind and change count; opens the branches popup. */
function BranchWidget({ onClick }: { onClick: () => void }) {
  const isRepo = useGit((s) => s.isRepo)
  const status = useGit((s) => s.status)
  const busy = useGit((s) => s.busy)
  if (!isRepo || !status) return null
  const name = status.branch || `detached ${status.head.slice(0, 7)}`
  return (
    <button className="status-branch" onClick={onClick} title="Branches & worktrees">
      ⎇ {name}
      {status.ahead > 0 && <span className="ahead"> ↑{status.ahead}</span>}
      {status.behind > 0 && <span className="behind"> ↓{status.behind}</span>}
      {status.operation && <span className="warn"> · {status.operation}</span>}
      {busy && <span className="muted"> · {busy}…</span>}
    </button>
  )
}

const LANG_BY_EXT: Record<string, string> = {
  go: 'go', ts: 'typescript', tsx: 'typescript', mts: 'typescript', cts: 'typescript', js: 'typescript',
  jsx: 'typescript', mjs: 'typescript', cjs: 'typescript', py: 'python', pyi: 'python', rs: 'rust',
}

/** Language server state for the active file; click shows details / install hint. */
function LspWidget({ path }: { path: string | null }) {
  const statuses = useLsp((s) => s.statuses)
  const lang = path ? LANG_BY_EXT[path.split('.').pop()?.toLowerCase() ?? ''] : undefined
  const st = lang ? statuses[lang] : undefined
  if (!st) return null
  const icon = st.state === 'ready' ? '●' : st.state === 'starting' ? '◌' : '⚠'
  return (
    <button
      className={`status-lsp ${st.state}`}
      title={st.message || `${st.language} language server: ${st.state}`}
      onClick={() => st.message && showToast(st.message, st.state === 'ready' ? 'info' : 'error')}
    >
      {icon} {st.language}
    </button>
  )
}
