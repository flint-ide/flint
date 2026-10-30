import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { useTerminal } from '../../store/terminal'
import { useLsp } from '../../store/lsp'
import { showToast } from '../../store/dialog'

export function StatusBar() {
  const root = useWorkspace((s) => s.root)
  const files = useWorkspace((s) => s.fileCount)
  const active = useEditor((s) => s.tabs.find((t) => t.id === s.activeId))
  const terminals = useTerminal((s) => s.tabs.length)

  const location =
    active?.kind === 'file' && root && active.path.startsWith(root) ? active.path.slice(root.length + 1) : active?.title

  return (
    <footer className="status-bar">
      <span className="truncate">{location ?? ''}</span>
      <span className="spacer" />
      {root && <span>{files.toLocaleString()} files</span>}
      <span>{terminals} {terminals === 1 ? 'terminal' : 'terminals'}</span>
      <LspWidget path={active?.kind === 'file' ? active.path : null} />
    </footer>
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
