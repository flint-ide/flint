import { useEditor } from '../../store/editor'
import { useAgentChat } from '../../store/agentChat'
import { inside } from '../../lib/sessionSnapshot'
import type { UsageTotals } from '../../lib/agentReducer'
import { formatCost, formatTokens, hasUsage, sumTotals, usageTooltip } from '../../lib/usage'
import { useWorkspace } from '../../store/workspace'
import { displayPath, rootLabels, rootOf } from '../../lib/workspaceRoots'
import { useTerminal } from '../../store/terminal'
import { useLsp } from '../../store/lsp'
import { showToast } from '../../store/dialog'
import { useGit } from '../../store/git'
import { showFileHistory, toggleBlame, useGitHistory } from '../../store/gitHistory'
import { dismissUpdate, downloadUpdate, skipUpdate, useUpdate } from '../../store/update'
import { offerText } from '../../lib/update'
import { useSettings } from '../../store/settings'
import { useCompletionStatus } from '../../store/editorAi'
import { useVimStatus } from '../../store/vimStatus'

export function StatusBar() {
  const root = useWorkspace((s) => s.root)
  const roots = useWorkspace((s) => s.roots)
  const files = useWorkspace((s) => s.fileCount)
  const active = useEditor((s) => s.tabs.find((t) => t.id === s.activeId))
  const terminals = useTerminal((s) => s.tabs.length)

  const location = active?.kind === 'file' && rootOf(roots, active.path) ? displayPath(roots, active.path) : active?.title

  return (
    <footer className="status-bar">
      <span className="truncate">{location ?? ''}</span>
      <span className="spacer" />
      {(active?.kind === 'file' || active?.kind === 'scratch') && <VimModeWidget />}
      {active?.kind === 'file' && <GitFileActions path={active.path} />}
      {root && <span>{files.toLocaleString()} files</span>}
      <span>{terminals} {terminals === 1 ? 'terminal' : 'terminals'}</span>
      <AgentUsageWidget roots={roots} />
      <CompletionsWidget />
      <LspWidget path={active?.kind === 'file' ? active.path : null} />
      <UpdateWidget />
    </footer>
  )
}

/** Vim's mode in the focused editor, while Vim mode is on. */
function VimModeWidget() {
  const on = useSettings((s) => s.settings.editor.vimMode)
  const mode = useVimStatus((s) => s.mode)
  if (!on || !mode) return null
  return <span className="status-vim" title="Vim mode (Settings ▸ Editor)">-- {mode} --</span>
}

/** A newer Flint release (found by the daily check or "Check for Updates…"). */
function UpdateWidget() {
  const offer = useUpdate((s) => s.offer)
  if (!offer) return null
  const title = offer.latest.name && offer.latest.name !== offer.latest.tag ? `${offer.latest.name} — ` : ''
  return (
    <span className="status-update" role="status">
      <span className="truncate" title={`${title}${offerText(offer)}`}>Flint {offer.latest.tag} available</span>
      <button className="status-action on" title="Open the release page in your browser" onClick={downloadUpdate}>
        Download
      </button>
      <button className="status-action" title="Don't tell me about this version again" onClick={() => void skipUpdate()}>
        Skip This Version
      </button>
      <button className="status-action" title="Hide until Flint restarts" aria-label="Dismiss" onClick={dismissUpdate}>
        ✕
      </button>
    </span>
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

/**
 * Tokens and cost of the agent chats in the open project and its attached folders
 * (hidden until something was used); the tooltip splits it per folder when there are several.
 */
function AgentUsageWidget({ roots }: { roots: string[] }) {
  // Selects a string, so streaming agent events (which replace sessions) don't
  // re-render the status bar; it changes only when a turn reports usage.
  const summary = useAgentChat((s) => {
    const chats = Object.values(s.sessions)
    const usedIn = (root: string) => chats.filter((c) => inside(c.cwd, root)).map((c) => c.chat)
    const total = sumTotals(roots.flatMap(usedIn))
    if (!hasUsage(total)) return ''
    const perRoot = roots.length > 1 ? rootLabels(roots).flatMap((label, i) => {
      const t = sumTotals(usedIn(roots[i]))
      return hasUsage(t) ? [`${label}: ${usageLabel(t)}`] : []
    }) : []
    return [usageLabel(total), usageTooltip(total), ...perRoot].join('\n')
  })
  if (!summary) return null
  const [label, ...details] = summary.split('\n')
  const scope = roots.length > 1 ? 'Agent chats in this workspace' : 'Agent chats in this project'
  return <span title={`${scope}\n${details.join('\n')}`}>{label}</span>
}

function usageLabel(t: UsageTotals): string {
  const cost = t.costUsd > 0 ? ` · ${formatCost(t.costUsd)}` : ''
  return `${formatTokens(t.inputTokens + t.outputTokens)} tokens${cost}`
}

/** Inline completions: shown while they are on, with the last error (e.g. Ollama not running). */
function CompletionsWidget() {
  const on = useSettings((s) => s.settings.completions.enabled)
  const model = useSettings((s) => s.settings.completions.model)
  const error = useCompletionStatus((s) => s.error)
  if (!on) return null
  return (
    <button
      className={`status-completions${error ? ' error' : ''}`}
      title={error ? `Completions: ${error}` : `Inline completions from ${model} (Tab accepts, Esc dismisses)`}
      onClick={() => error && showToast(`Completions: ${error}`, 'error')}
    >
      {error ? '⚠' : '✦'} Completions
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
