// The sidebar panels. Adding one is a component plus one entry here: the activity
// bar, the sidebar, the "Show …" command and the SidebarView type follow from it.
import { lazy } from 'react'
import { FileTree } from '../components/sidebar/FileTree'
import { SearchPanel } from '../components/sidebar/SearchPanel'
import { ScratchPanel } from '../components/sidebar/ScratchPanel'
import { definePanels } from './registry'

// Light panels are imported directly; the rest are fetched when first opened, so
// their code (and their dependencies) is not parsed at startup.
const CommitPanel = lazy(() => import('../components/sidebar/CommitPanel').then((m) => ({ default: m.CommitPanel })))
const PullRequestsPanel = lazy(() => import('../components/github/PullRequestsPanel').then((m) => ({ default: m.PullRequestsPanel })))
const DatabasePanel = lazy(() => import('../components/sidebar/DatabasePanel').then((m) => ({ default: m.DatabasePanel })))
const TodoPanel = lazy(() => import('../components/sidebar/TodoPanel').then((m) => ({ default: m.TodoPanel })))
const DockerPanel = lazy(() => import('../components/sidebar/DockerPanel').then((m) => ({ default: m.DockerPanel })))
const AwsPanel = lazy(() => import('../components/sidebar/AwsPanel').then((m) => ({ default: m.AwsPanel })))
const TasksPanel = lazy(() => import('../components/tasks/TasksPanel').then((m) => ({ default: m.TasksPanel })))
const KubePanel = lazy(() => import('../components/sidebar/KubePanel').then((m) => ({ default: m.KubePanel })))
const UsagePanel = lazy(() => import('../components/usage/UsagePanel').then((m) => ({ default: m.UsagePanel })))

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
    command: { id: 'github.pullRequests', title: 'Pull Requests…', category: 'GitHub', keywords: 'pr review gh github list' },
  },
  {
    id: 'database', title: 'Databases', icon: 'database', order: 50, component: DatabasePanel,
    command: { id: 'view.databases', keywords: 'sql postgres clickhouse' },
  },
  {
    id: 'scratch', title: 'Scratchpads', icon: 'pencil', order: 60, component: ScratchPanel,
    command: { id: 'view.scratchpads', keywords: 'notes sql consoles' },
  },
  {
    id: 'todo', title: 'TODO', icon: 'checkbox', order: 70, component: TodoPanel,
    command: { title: 'TODO List', keywords: 'fixme hack xxx bug notes comments' },
  },
  {
    id: 'docker', title: 'Docker', icon: 'ship', order: 80, component: DockerPanel,
    command: { title: 'Docker', keywords: 'containers compose logs' },
  },
  {
    id: 'aws', title: 'AWS Profiles', icon: 'cloud', order: 90, component: AwsPanel,
    command: { title: 'AWS Profiles', keywords: 'amazon sso login credentials terminal' },
  },
  {
    id: 'kube', title: 'Kubernetes', icon: 'helm', order: 100, component: KubePanel,
    command: { title: 'Kubernetes', keywords: 'k8s kubectl cluster pods context namespace' },
  },
])

/** Id of a built-in panel; the open sidebar view is one of these. */
export type PanelId = (typeof PANELS)[number]['id']
