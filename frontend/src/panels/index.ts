// Flint's own sidebar panels (Databases comes from its feature module).
// Adding one is a component plus one entry here: the activity bar, the sidebar and the "Show …" command follow from it. A feature
// module contributes its panels itself (lib/features.ts); usePanels and
// currentPanels are both lists together.
import { lazyPreload } from '../lib/lazyPreload'
import { FileTree } from '../components/sidebar/FileTree'
import { SearchPanel } from '../components/sidebar/SearchPanel'
import { ScratchPanel } from '../components/sidebar/ScratchPanel'
import { definePanels, mergePanels, type PanelSpec } from './registry'
import { useContributions, type Contributions } from '../lib/contributions'

// Light panels are imported directly; the rest are not parsed at startup but
// fetched in the background once the app is idle (or when first opened).
const CommitPanel = lazyPreload(() => import('../components/sidebar/CommitPanel').then((m) => ({ default: m.CommitPanel })))
const PullRequestsPanel = lazyPreload(() => import('../components/github/PullRequestsPanel').then((m) => ({ default: m.PullRequestsPanel })))
const TasksPanel = lazyPreload(() => import('../components/tasks/TasksPanel').then((m) => ({ default: m.TasksPanel })))
const UsagePanel = lazyPreload(() => import('../components/usage/UsagePanel').then((m) => ({ default: m.UsagePanel })))

// Command ids and titles predating the registry are kept: keymap overrides are stored by id.
export const PANELS = definePanels([
  { id: 'files', title: 'Files', icon: 'files', order: 10, component: FileTree, command: { keywords: 'project tree explorer' } },
  {
    id: 'search', title: 'Search', icon: 'search', order: 20, component: SearchPanel, shortcutCommand: 'nav.findInFiles',
    command: { keywords: 'find grep' },
  },
  {
    id: 'git', title: 'Commit', icon: 'branch', order: 30, component: CommitPanel, shortcutCommand: 'git.commit',
    command: { keywords: 'git changes' },
  },
  {
    id: 'tasks', title: 'Agent Tasks', icon: 'fork', order: 35, component: TasksPanel,
    command: { id: 'view.agentTasks', category: 'Agent', keywords: 'worktree parallel agents branch claude codex' },
  },
  {
    id: 'usage', title: 'Usage', icon: 'sum', order: 37, component: UsagePanel,
    command: { id: 'view.agentUsage', title: 'Show Agent Usage', category: 'Agent', keywords: 'cost tokens budget spend dollars claude codex csv' },
  },
  {
    id: 'pulls', title: 'Pull Requests', icon: 'updown', order: 40, component: PullRequestsPanel,
    command: { id: 'github.pullRequests', title: 'Pull Requests…', category: 'GitHub', keywords: 'pr review github list' },
  },
  {
    id: 'scratch', title: 'Scratchpads', icon: 'pencil', order: 60, component: ScratchPanel,
    command: { id: 'view.scratchpads', keywords: 'notes sql consoles' },
  },
])

/** Id of a built-in panel; the open sidebar view is one of these or a contributed panel's. */
export type PanelId = (typeof PANELS)[number]['id']

let merged: { extra: Contributions['panels']; all: readonly PanelSpec[] } = { extra: [], all: PANELS }
const withContributed = (s: Contributions): readonly PanelSpec[] => {
  if (merged.extra !== s.panels) merged = { extra: s.panels, all: mergePanels(PANELS, s.panels) }
  return merged.all
}

/** Every panel: Flint's and contributed ones, in activity bar order. */
export const usePanels = () => useContributions(withContributed)
export const currentPanels = () => withContributed(useContributions.getState())
