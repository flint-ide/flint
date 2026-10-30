import { useEditor } from '../../store/editor'
import { useAgentChat } from '../../store/agentChat'
import { inside } from '../../lib/sessionSnapshot'
import { formatCost, formatTokens, hasUsage, sumTotals, usageTooltip } from '../../lib/usage'
import { useWorkspace } from '../../store/workspace'
import { useTerminal } from '../../store/terminal'
import { useLsp } from '../../store/lsp'
import { showToast } from '../../store/dialog'
import { useGit } from '../../store/git'
import { showFileHistory, toggleBlame, useGitHistory } from '../../store/gitHistory'

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
      {active?.kind === 'file' && <GitFileActions path={active.path} />}
      {root && <span>{files.toLocaleString()} files</span>}
      <span>{terminals} {terminals === 1 ? 'terminal' : 'terminals'}</span>
      <AgentUsageWidget root={root} />
      <LspWidget path={active?.kind === 'file' ? active.path : null} />
    </footer>
  )
}

/** Annotate (blame) toggle and history for the active file. */
function GitFileActions({ path }: { path: string }) {
  const isRepo = useGit((s) => s.isRepo)
  const annotated = useGitHistory((s) => !!s.blame[path])
  if (!isRepo) return null
  return (
    <>
      <button
        className={`status-action${annotated ? ' on' : ''}`}
        title={annotated ? 'Close annotations' : 'Annotate with Git Blame'}
        onClick={() => toggleBlame(path)}
      >
        Blame
      </button>
      <button className="status-action" title="Show History of this file" onClick={() => showFileHistory(path)}>
        History
      </button>
    </>
  )
}

/** Tokens and cost of this project's agent chats (hidden until something was used). */
function AgentUsageWidget({ root }: { root: string | null }) {
  // Selects a string, so streaming agent events (which replace sessions) don't
  // re-render the status bar; it changes only when a turn reports usage.
  const summary = useAgentChat((s) => {
    const total = sumTotals(Object.values(s.sessions).filter((c) => root && inside(c.cwd, root)).map((c) => c.chat))
    if (!hasUsage(total)) return ''
    const cost = total.costUsd > 0 ? ` · ${formatCost(total.costUsd)}` : ''
    return `${formatTokens(total.inputTokens + total.outputTokens)} tokens${cost}\n${usageTooltip(total)}`
  })
  if (!summary) return null
  const [label, ...details] = summary.split('\n')
  return <span title={`Agent chats in this project\n${details.join('\n')}`}>{label}</span>
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
