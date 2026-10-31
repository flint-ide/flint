// The app's own commands (palette + global shortcuts). Features add theirs with
// registerCommand, see lib/commands.ts.
import { Workspace } from '../api/backend'
import { parseKeys, registerCommands, type Command } from '../lib/commands'
import { attachSelectionToAgent } from '../lib/agentContext'
import { registerFeatureCommands } from './featureCommands'
import { requestCloseTab } from '../components/editor/EditorArea'
import { useWorkspace, type SidebarView } from '../store/workspace'
import { saveTab, useEditor, viewFor, type Tab } from '../store/editor'
import { toggleTerminal, useTerminal } from '../store/terminal'
import { activeRoot } from '../store/activeRoot'
import { removeFromWorkspace } from '../store/fileOps'
import { rootLabels } from '../lib/workspaceRoots'
import { useGit } from '../store/git'
import { useScratch } from '../store/scratch'
import { useSearch } from '../store/search'
import { useDatabase } from '../store/database'
import { activeSqlConsole } from '../store/sqlConsole'
import { useAgentChat } from '../store/agentChat'
import { showToast } from '../store/dialog'

/** Focus requests that live in App state (the panels take them as focus tokens). */
export interface AppCommandHooks {
  focusSearch: () => void
  focusCommit: () => void
}

// Where a global shortcut belongs to the focused widget instead: ⌘B in an editor is
// "go to declaration" (IntelliJ), ⌘K in a terminal clears the screen.
const IN_EDITOR = '.cm-editor'
const IN_TERMINAL = '.terminal-view'

const ws = () => useWorkspace.getState()
const activeTab = (): Tab | undefined => {
  const { tabs, activeId } = useEditor.getState()
  return tabs.find((t) => t.id === activeId)
}
const isRepo = () => useGit.getState().isRepo
const hasEditor = () => {
  const id = useEditor.getState().activeId
  return !!id && !!viewFor(id)
}
const showPanel = (view: SidebarView) => () => ws().setSidebar(view)

/** Registers everything below; returns the matching unregister. */
export function registerAppCommands(hooks: AppCommandHooks): () => void {
  const offStatic = registerCommands(appCommands(hooks))
  const offAgents = registerAgentChatCommands()
  const offFeatures = registerFeatureCommands()
  const offRoots = registerWorkspaceRootCommands()
  return () => {
    offStatic()
    offAgents()
    offFeatures()
    offRoots()
  }
}

function appCommands({ focusSearch, focusCommit }: AppCommandHooks): Command[] {
  return [
    // General
    {
      id: 'app.commandPalette', title: 'Find Action…', category: 'General', keys: 'Mod+Shift+P',
      keywords: 'command palette', run: () => ws().setPaletteOpen(true),
    },
    {
      id: 'app.settings', title: 'Settings…', category: 'General', keys: 'Mod+,',
      keywords: 'preferences options theme dark light font size keymap shortcuts keyboard agent',
      run: () => useEditor.getState().openSettings(),
    },

    // Navigate
    { id: 'nav.goToFile', title: 'Go to File…', category: 'Navigate', keys: 'Mod+P', keywords: 'open quick', run: () => ws().setQuickOpen(true) },
    {
      id: 'nav.findInFiles', title: 'Find in Files…', category: 'Navigate', keys: 'Mod+Shift+F', keywords: 'search grep',
      run: () => {
        ws().setSidebar('search')
        focusSearch()
      },
    },
    {
      id: 'nav.replaceInFiles', title: 'Replace in Files…', category: 'Navigate', keys: 'Mod+Shift+R', keywords: 'search substitute rename text',
      run: () => {
        useSearch.getState().setParams({ replaceOn: true })
        ws().setSidebar('search')
        focusSearch()
      },
    },
    {
      id: 'nav.undoReplace', title: 'Undo Last Replace in Files', category: 'Navigate', keywords: 'revert',
      when: () => !!useSearch.getState().lastReplace?.undoable,
      run: () => useSearch.getState().undoReplace(ws().roots),
    },
    { id: 'nav.back', title: 'Back', category: 'Navigate', keys: 'Mod+[', keywords: 'previous history', run: () => useEditor.getState().back() },
    { id: 'nav.forward', title: 'Forward', category: 'Navigate', keys: 'Mod+]', keywords: 'next history', run: () => useEditor.getState().forward() },
    {
      id: 'nav.goToDeclaration', title: 'Go to Declaration', category: 'Navigate', hintKeys: 'Mod+B', keywords: 'definition lsp',
      when: () => activeTab()?.kind === 'file' && hasEditor(), run: () => pressInEditor('Mod+B'),
    },
    {
      id: 'nav.findUsages', title: 'Find Usages', category: 'Navigate', hintKeys: 'Alt+F7', keywords: 'references lsp',
      when: () => activeTab()?.kind === 'file' && hasEditor(), run: () => pressInEditor('Alt+F7'),
    },
    { id: 'nav.goToLine', title: 'Go to Line…', category: 'Navigate', hintKeys: 'Mod+Alt+G', when: hasEditor, run: () => pressInEditor('Mod+Alt+G') },

    // Editor
    { id: 'editor.find', title: 'Find in File…', category: 'Editor', hintKeys: 'Mod+F', keywords: 'search replace', when: hasEditor, run: () => pressInEditor('Mod+F') },
    {
      id: 'editor.save', title: 'Save', category: 'Editor', hintKeys: 'Mod+S',
      when: () => ['file', 'scratch'].includes(activeTab()?.kind ?? ''), run: () => saveTab(useEditor.getState().activeId ?? ''),
    },
    {
      id: 'editor.closeTab', title: 'Close Tab', category: 'Editor', keys: 'Mod+W',
      run: () => {
        const tab = activeTab()
        if (tab) void requestCloseTab(tab)
      },
    },

    // Agent
    {
      id: 'agent.sendSelection', title: 'Send Selection to Agent', category: 'Agent', keys: 'Mod+L',
      keywords: 'attach file context chat claude codex', run: () => attachSelectionToAgent(),
    },
    ...policyToggles(),

    // Git
    {
      id: 'git.commit', title: 'Commit…', category: 'Git', keys: 'Mod+K', keysIgnoredIn: IN_TERMINAL,
      run: () => {
        ws().setSidebar('git')
        focusCommit()
      },
    },
    { id: 'git.push', title: 'Push', category: 'Git', keys: 'Mod+Shift+K', keysIgnoredIn: IN_TERMINAL, run: () => useGit.getState().remote('push') },
    { id: 'git.pull', title: 'Update Project (Pull)', category: 'Git', when: isRepo, run: () => useGit.getState().remote('pull') },
    { id: 'git.fetch', title: 'Fetch', category: 'Git', when: isRepo, run: () => useGit.getState().remote('fetch') },
    { id: 'git.refresh', title: 'Refresh Status', category: 'Git', keywords: 'reload changes', when: isRepo, run: () => useGit.getState().refresh() },
    { id: 'git.branches', title: 'Branches & Worktrees…', category: 'Git', keywords: 'checkout switch', when: hasBranchWidget, run: () => ws().setBranchesPopup('list') },
    { id: 'git.newBranch', title: 'New Branch…', category: 'Git', when: hasBranchWidget, run: () => ws().setBranchesPopup('new-branch') },
    { id: 'git.newWorktree', title: 'New Worktree…', category: 'Git', keywords: 'agent folder', when: hasBranchWidget, run: () => ws().setBranchesPopup('new-worktree') },

    // Terminal
    // In the active tab's workspace folder (the primary one when it has none).
    { id: 'terminal.toggle', title: 'Toggle Terminal', category: 'Terminal', keys: 'Mod+J', run: () => toggleTerminal(activeRoot()) },
    { id: 'terminal.new', title: 'New Terminal', category: 'Terminal', keys: 'Mod+Shift+T', keywords: 'shell', run: () => void useTerminal.getState().create(activeRoot()) },

    // View
    { id: 'view.toggleSidebar', title: 'Toggle Sidebar', category: 'View', keys: 'Mod+B', keysIgnoredIn: IN_EDITOR, keywords: 'panel hide show', run: () => ws().toggleSidebar() },
    { id: 'view.files', title: 'Show Files', category: 'View', keywords: 'project tree explorer', run: showPanel('files') },
    { id: 'view.databases', title: 'Show Databases', category: 'View', keywords: 'sql postgres clickhouse', run: showPanel('database') },
    { id: 'view.scratchpads', title: 'Show Scratchpads', category: 'View', keywords: 'notes sql consoles', run: showPanel('scratch') },
    { id: 'view.kube', title: 'Kubernetes', category: 'View', keywords: 'k8s kubectl cluster pods context namespace', run: showPanel('kube') },
    { id: 'view.aws', title: 'AWS Profiles', category: 'View', keywords: 'amazon sso login credentials terminal', run: showPanel('aws') },
    { id: 'view.docker', title: 'Docker', category: 'View', keywords: 'containers compose logs', run: showPanel('docker') },
    { id: 'view.todo', title: 'TODO List', category: 'View', keywords: 'fixme hack xxx bug notes comments', run: showPanel('todo') },

    // Project
    { id: 'project.recent', title: 'Recent Projects…', category: 'Project', keys: 'Mod+O', keywords: 'open folder', run: () => ws().setRecentOpen(true) },
    {
      id: 'project.openFolder', title: 'Open Folder…', category: 'Project',
      run: async () => {
        const path = await Workspace.PickFolder()
        if (path) await ws().openFolder(path)
      },
    },
    {
      id: 'project.addToWorkspace', title: 'Add Project to Workspace…', category: 'Project', keys: 'Mod+Shift+O',
      keywords: 'attach folder multi root', when: () => !!ws().root, run: () => ws().pickRoot(),
    },
    { id: 'project.reloadTree', title: 'Reload File Tree', category: 'Project', keywords: 'refresh', when: () => !!ws().root, run: () => ws().reloadTree() },

    // Database & scratchpads
    { id: 'scratch.newSql', title: 'New SQL Console', category: 'Database', keys: 'Mod+N', keywords: 'scratch query', run: () => void useScratch.getState().create('sql') },
    {
      id: 'sql.runQuery', title: 'Run Query', category: 'Database', hintKeys: 'Mod+Enter', keywords: 'execute statement',
      when: () => {
        const tab = activeTab()
        return tab?.kind === 'scratch' && tab.scratchKind === 'sql' && hasEditor()
      },
      run: () => pressInEditor('Mod+Enter'),
    },
    {
      id: 'sql.explain', title: 'Explain Plan', category: 'Database', keys: 'Mod+Shift+E', keywords: 'query plan explain',
      when: () => !!activeSqlConsole(),
      run: () => activeSqlConsole()?.explain(false),
    },
    {
      id: 'sql.explainAnalyze', title: 'Explain Analyze', category: 'Database', keys: 'Mod+Shift+A',
      keywords: 'query plan explain execute timing buffers pipeline',
      when: () => !!activeSqlConsole(),
      run: () => activeSqlConsole()?.explain(true),
    },
    {
      id: 'sql.history', title: 'Query History…', category: 'Database', keys: 'Mod+Shift+H', keywords: 'recent statements log rerun',
      when: () => !!activeSqlConsole(),
      run: () => activeSqlConsole()?.showHistory(),
    },
    {
      id: 'db.newConnection', title: 'New Database Connection…', category: 'Database', keywords: 'postgres clickhouse add',
      run: () => {
        ws().setSidebar('database')
        useDatabase.getState().setEditing('new')
      },
    },
    { id: 'scratch.newMarkdown', title: 'New Markdown Note', category: 'Scratchpads', keywords: 'md scratch', run: () => void useScratch.getState().create('markdown') },
    { id: 'scratch.newText', title: 'New Text Note', category: 'Scratchpads', keywords: 'txt scratch', run: () => void useScratch.getState().create('text') },
  ]
}

/** The branch widget (and so the branches popup) only exists for a repo with a known status. */
function hasBranchWidget(): boolean {
  const g = useGit.getState()
  return g.isRepo && !!g.status
}

/** "What the agent may do without asking", as in the chat's policy menu. */
function policyToggles(): Command[] {
  type Flag = 'autoApproveEdits' | 'autoApproveReads' | 'autoApproveAll'
  const toggle = (flag: Flag, what: string): Command => ({
    id: `agent.policy.${flag}`,
    title: `Toggle Auto-Approve: ${what}`,
    category: 'Agent',
    keywords: 'permissions policy settings ask',
    when: () => !!useAgentChat.getState().policy,
    run: async () => {
      const { policy, setPolicy } = useAgentChat.getState()
      if (!policy) return
      await setPolicy({ ...policy, [flag]: !policy[flag] })
      const on = useAgentChat.getState().policy?.[flag]
      showToast(`Agent ${on ? 'may now' : 'now asks before it can'} ${what.toLowerCase()}`)
    },
  })
  return [
    toggle('autoApproveEdits', 'Edit Files'),
    toggle('autoApproveReads', 'Read and Search'),
    toggle('autoApproveAll', 'Do Everything (Commands Too)'),
  ]
}

/** One "New Chat with <agent>" per installed agent; follows the list as it loads. */
function registerAgentChatCommands(): () => void {
  let agents: string[] | null = null
  let off: () => void = () => undefined
  const sync = () => {
    const next = useAgentChat.getState().agents
    if (next === agents) return
    agents = next
    off()
    off = registerCommands(
      next.map((agent) => ({
        id: `agent.newChat.${agent}`,
        title: `New Chat with ${agent}`,
        category: 'Agent',
        keywords: 'conversation',
        when: () => !useAgentChat.getState().starting,
        run: () => useAgentChat.getState().start(agent, activeRoot()),
      })),
    )
  }
  sync()
  const unsubscribe = useAgentChat.subscribe(sync)
  return () => {
    unsubscribe()
    off()
  }
}

/** "Remove <project> from Workspace" per attached folder; follows the workspace. */
function registerWorkspaceRootCommands(): () => void {
  let roots: string[] | null = null
  let off: () => void = () => undefined
  const sync = () => {
    const next = useWorkspace.getState().roots
    if (next === roots) return
    roots = next
    off()
    const labels = rootLabels(next)
    off = registerCommands(
      next.slice(1).map((r, i) => ({
        id: `project.remove.${r}`,
        title: `Remove ${labels[i + 1]} from Workspace`,
        category: 'Project',
        keywords: 'detach close folder multi root',
        run: () => removeFromWorkspace(r),
      })),
    )
  }
  sync()
  const unsubscribe = useWorkspace.subscribe(sync)
  return () => {
    unsubscribe()
    off()
  }
}

/**
 * Runs the active editor's own key binding (LSP, search, ⌘↵ in SQL consoles), so a
 * palette entry behaves exactly like its shortcut. CodeMirror is imported lazily:
 * it's already loaded whenever an editor exists.
 */
async function pressInEditor(keys: string): Promise<void> {
  const id = useEditor.getState().activeId
  const view = id ? viewFor(id) : undefined
  if (!view) return
  const { runScopeHandlers } = await import('@codemirror/view')
  const chord = parseKeys(keys)
  const raw = keys.split('+').pop() ?? ''
  const event = new KeyboardEvent('keydown', {
    key: raw.length === 1 ? raw.toLowerCase() : raw, // CodeMirror names: 'b', 'F7', 'Enter'
    metaKey: chord.meta,
    ctrlKey: chord.ctrl,
    altKey: chord.alt,
    shiftKey: chord.shift,
    bubbles: true,
    cancelable: true,
  })
  view.focus()
  runScopeHandlers(view, event, 'editor')
}
