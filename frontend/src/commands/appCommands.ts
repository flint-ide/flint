// The app's own commands (palette + global shortcuts). Features add theirs with
// registerCommand, see lib/commands.ts.
import { Workspace } from '../api/backend'
import { parseKeys, registerCommands, type Command } from '../lib/commands'
import { pressInEditor } from './pressInEditor'
import { attachSelectionToAgent } from '../lib/agentContext'
import { registerFeatureCommands } from './featureCommands'
import { registerTaskCommands } from './taskCommands'
import { registerPromptCommands } from './promptCommands'
import { registerReviewCommands } from './reviewCommands'
import { requestCloseTab } from '../components/editor/EditorArea'
import { useWorkspace } from '../store/workspace'
import { PANELS } from '../panels'
import { panelCommands } from '../panels/registry'
import { saveTab, useEditor, viewFor, type Tab } from '../store/editor'
import { toggleTerminal, useTerminal } from '../store/terminal'
import { activeRoot } from '../store/activeRoot'
import { removeFromWorkspace } from '../store/fileOps'
import { rootLabels } from '../lib/workspaceRoots'
import { useGit } from '../store/git'
import { useScratch } from '../store/scratch'
import { useSearch } from '../store/search'
import { useAgentChat } from '../store/agentChat'
import { showToast } from '../store/dialog'
import { trustFolder, untrustFolder } from '../store/trust'
import { copyDiagnostics, reportIssue, showLogs } from '../store/diagnostics'
import { openToolCheck } from '../store/toolCheck'
import { openInlineEditInActiveEditor } from '../store/editorAi'
import { useSettings } from '../store/settings'
import { useLsp } from '../store/lsp'
import { openVSCodeImport } from '../store/vscodeImport'
import { fileManagerName } from '../lib/platform'

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
/** The active tab is a file in an editor (the commands below are its language server's own keys there). */
const hasLanguageServer = () => activeTab()?.kind === 'file' && hasEditor()
/** Registers everything below; returns the matching unregister. */
export function registerAppCommands(): () => void {
  const offStatic = registerCommands(appCommands())
  const offAgents = registerAgentChatCommands()
  const offFeatures = registerFeatureCommands()
  const offRoots = registerWorkspaceRootCommands()
  const offTasks = registerTaskCommands()
  const offPrompts = registerPromptCommands()
  const offReview = registerReviewCommands()
  return () => {
    offPrompts()
    offReview()
    offTasks()
    offStatic()
    offAgents()
    offFeatures()
    offRoots()
  }
}

function appCommands(): Command[] {
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
    {
      id: 'app.importVSCode', title: 'Import from VS Code…', category: 'General',
      keywords: 'migrate switch cursor vscodium settings keybindings shortcuts theme colors',
      run: () => void openVSCodeImport(),
    },
    {
      id: 'editor.toggleVimMode', title: 'Toggle Vim Mode', category: 'Editor', keywords: 'vi keybindings modal normal insert',
      run: () => {
        const { settings, update } = useSettings.getState()
        const vimMode = !settings.editor.vimMode
        update({ editor: { ...settings.editor, vimMode } })
        showToast(vimMode ? 'Vim mode on' : 'Vim mode off')
      },
    },
    {
      id: 'workspace.trust', title: 'Trust Folder', category: 'Workspace', keywords: 'restricted mode security trust',
      run: () => void trustFolder(activeRoot()),
    },
    {
      id: 'workspace.untrust', title: 'Revoke Folder Trust (Restricted Mode)', category: 'Workspace', keywords: 'restricted mode security untrust',
      run: () => void untrustFolder(activeRoot()),
    },
    { id: 'help.reportIssue', title: 'Report an Issue…', category: 'Help', keywords: 'bug feedback github diagnostics logs', run: () => void reportIssue() },
    { id: 'help.copyDiagnostics', title: 'Copy Diagnostics', category: 'Help', keywords: 'logs versions support', run: () => void copyDiagnostics() },
    { id: 'help.showLogs', title: `Show Logs in ${fileManagerName()}`, category: 'Help', keywords: 'log file debug', run: showLogs },
    {
      id: 'help.checkTools', title: 'Check Tools…', category: 'Help',
      keywords: 'install setup missing path version git ripgrep claude codex delve language server',
      run: openToolCheck,
    },

    // Navigate
    { id: 'nav.goToFile', title: 'Go to File…', category: 'Navigate', keys: 'Mod+P', keywords: 'open quick', run: () => ws().setQuickOpen(true) },
    {
      id: 'nav.findInFiles', title: 'Find in Files…', category: 'Navigate', keys: 'Mod+Shift+F', keywords: 'search grep',
      run: () => ws().focusPanel('search'),
    },
    {
      id: 'nav.replaceInFiles', title: 'Replace in Files…', category: 'Navigate', keys: 'Mod+Shift+R', keywords: 'search substitute rename text',
      run: () => {
        useSearch.getState().setParams({ replaceOn: true })
        ws().focusPanel('search')
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
    {
      id: 'nav.goToImplementation', title: 'Go to Implementation', category: 'Navigate', hintKeys: 'Mod+Alt+B', keywords: 'implementations lsp',
      when: hasLanguageServer, run: () => pressInEditor('Mod+Alt+B'),
    },
    {
      id: 'nav.goToTypeDefinition', title: 'Go to Type Declaration', category: 'Navigate', hintKeys: 'Mod+Shift+B', keywords: 'type definition lsp',
      when: hasLanguageServer, run: () => pressInEditor('Mod+Shift+B'),
    },
    {
      id: 'nav.fileStructure', title: 'Go to Symbol in File…', category: 'Navigate', hintKeys: 'Mod+F12', keywords: 'outline file structure symbols lsp',
      when: hasLanguageServer, run: () => pressInEditor('Mod+F12'),
    },
    {
      id: 'nav.goToSymbol', title: 'Go to Symbol in Workspace…', category: 'Navigate', keys: 'Mod+Alt+O', keywords: 'class function type lsp',
      when: () => !!ws().root, run: () => useLsp.getState().showWorkspaceSymbols(),
    },
    { id: 'nav.goToLine', title: 'Go to Line…', category: 'Navigate', hintKeys: 'Mod+Alt+G', when: hasEditor, run: () => pressInEditor('Mod+Alt+G') },

    // Editor
    {
      id: 'editor.rename', title: 'Rename Symbol…', category: 'Editor', hintKeys: 'Shift+F6', keywords: 'refactor lsp F2',
      when: hasLanguageServer, run: () => pressInEditor('Shift+F6'),
    },
    {
      id: 'editor.quickFix', title: 'Show Quick Fixes and Refactorings…', category: 'Editor', hintKeys: 'Alt+Enter', keywords: 'code actions intentions lightbulb lsp',
      when: hasLanguageServer, run: () => pressInEditor('Alt+Enter'),
    },
    {
      id: 'editor.format', title: 'Reformat File', category: 'Editor', hintKeys: 'Mod+Alt+L', keywords: 'format document gofmt prettier lsp',
      when: hasLanguageServer, run: () => pressInEditor('Mod+Alt+L'),
    },
    {
      id: 'editor.organizeImports', title: 'Organize Imports', category: 'Editor', hintKeys: 'Shift+Alt+O', keywords: 'optimize imports sort lsp',
      when: hasLanguageServer, run: () => pressInEditor('Shift+Alt+O'),
    },
    {
      id: 'editor.parameterInfo', title: 'Parameter Info', category: 'Editor', hintKeys: 'Mod+Shift+Space', keywords: 'signature help arguments lsp',
      when: hasLanguageServer, run: () => pressInEditor('Mod+Shift+Space'),
    },
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
    {
      id: 'agent.inlineEdit', title: 'Edit Selection with Agent…', category: 'Agent', keys: 'Mod+I', keysIgnoredIn: IN_TERMINAL,
      keywords: 'inline edit rewrite refactor ai prompt claude codex', when: () => activeTab()?.kind === 'file' && hasEditor(),
      run: openInlineEditInActiveEditor,
    },
    {
      id: 'editor.toggleCompletions', title: 'Toggle Inline Completions', category: 'Editor',
      keywords: 'ai suggestions ghost text ollama local model autocomplete copilot',
      run: () => useSettings.getState().update((s) => ({ completions: { ...s.completions, enabled: !s.completions.enabled } })),
    },
    ...policyToggles(),

    // Git
    {
      id: 'git.commit', title: 'Commit…', category: 'Git', keys: 'Mod+K', keysIgnoredIn: IN_TERMINAL,
      run: () => ws().focusPanel('git'),
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
    // One "Show <panel>" per built-in sidebar panel (panels/index.ts); features register their own.
    ...panelCommands(PANELS, (id) => ws().setSidebar(id)),

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

    // Scratchpads (SQL consoles and their commands come from the database feature)
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

