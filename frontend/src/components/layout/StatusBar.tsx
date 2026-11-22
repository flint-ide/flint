import { StatusBar as KitStatusBar, StatusItem, StatusSpacer } from '@flint-ide/ui'
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
import { useContributions } from '../../lib/contributions'
import { useSettings } from '../../store/settings'
import { useCompletionStatus } from '../../store/editorAi'
import { useVimStatus } from '../../store/vimStatus'
import { CIWidget } from './CIWidget'
import { MachinesStatus } from './MachinesStatus'

export function StatusBar() {
  const root = useWorkspace((s) => s.root)
  const roots = useWorkspace((s) => s.roots)
  const files = useWorkspace((s) => s.fileCount)
  const active = useEditor((s) => s.tabs.find((t) => t.id === s.activeId))
  const terminals = useTerminal((s) => s.tabs.length)

  const location = active?.kind === 'file' && rootOf(roots, active.path) ? displayPath(roots, active.path) : active?.title

  return (
    <KitStatusBar className="status-bar">
      <span className="truncate">{location ?? ''}</span>
      <StatusSpacer />
      {(active?.kind === 'file' || active?.kind === 'scratch') && <VimModeWidget />}
      {active?.kind === 'file' && <GitFileActions path={active.path} />}
      {root && <span>{files.toLocaleString()} files</span>}
      <span>{terminals} {terminals === 1 ? 'terminal' : 'terminals'}</span>
      <MachinesStatus />
      <CIWidget />
      <AgentUsageWidget roots={roots} />
      <CompletionsWidget />
      <LspWidget path={active?.kind === 'file' ? active.path : null} />
      <ContributedItems />
    </KitStatusBar>
  )
}

/** Vim's mode in the focused editor, while Vim mode is on. */
function VimModeWidget() {
  const on = useSettings((s) => s.settings.editor.vimMode)
  const mode = useVimStatus((s) => s.mode)
  if (!on || !mode) return null
  return <span className="status-vim" title="Vim mode (Settings ▸ Editor)">-- {mode} --</span>
}

/** Items of feature modules (lib/features.ts), after Flint's own; each renders nothing when it has nothing to show. */
function ContributedItems() {
  const items = useContributions((s) => s.statusItems)
  return (
    <>
      {items.map((item) => (
        <item.component key={item.id} />
      ))}
    </>
  )
}

/** Annotate (blame) toggle and history for the active file. */
function GitFileActions({ path }: { path: string }) {
  const isRepo = useGit((s) => s.isRepo)
  const annotated = useGitHistory((s) => !!s.blame[path])
  if (!isRepo) return null
  return (
    <>
      <StatusItem pressed={annotated} tip={annotated ? 'Close annotations' : 'Annotate with Git Blame'} onClick={() => toggleBlame(path)}>
        Blame
      </StatusItem>
      <StatusItem tip="Show History of this file" onClick={() => showFileHistory(path)}>
        History
      </StatusItem>
    </>
  )
}

/**
 * Tokens and cost of the agent chats in the open folder and its attached folders
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
  const scope = roots.length > 1 ? 'Agent chats in this workspace' : 'Agent chats in this folder'
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
    <StatusItem
      className={error ? 'status-error' : undefined}
      tip={error ? `Completions: ${error}` : `Inline completions from ${model} (Tab accepts, Esc dismisses)`}
      onClick={() => error && showToast(`Completions: ${error}`, 'error')}
    >
      {error ? '⚠' : '✦'} Completions
    </StatusItem>
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
    <StatusItem
      className={`status-lsp ${st.state}`}
      tip={st.message || `${st.language} language server: ${st.state}`}
      onClick={() => st.message && showToast(st.message, st.state === 'ready' ? 'info' : 'error')}
    >
      {icon} {st.language}
    </StatusItem>
  )
}
