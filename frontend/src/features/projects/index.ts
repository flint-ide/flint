// The Projects feature (Go: internal/features/projects): projects, folders of work
// items (a project's backlog) for one or more repositories, shown as a sidebar panel
// and a board tab, handed to agents with Delegate. "Project" here means only such a
// folder; the folders open in the window are workspace folders (the active folder).
// The panel, its commands and the board exist only while the feature runs.
import { lazy } from 'react'
import { lazyPreload } from '../../lib/lazyPreload'
import { defineFeature } from '../../lib/features'
import { BOARD_VIEW } from './projects'
import { newProject, openProject, openNewItem, openPlan, showBoard, subscribeAutoStart, useProjects } from './store'

const ProjectsPanel = lazyPreload(() => import('./ProjectsPanel').then((m) => ({ default: m.ProjectsPanel })))
const BoardView = lazy(() => import('./BoardView').then((m) => ({ default: m.BoardView })))

export default defineFeature({
  id: 'projects',
  optional: true,
  name: 'Projects',
  description: 'Plan work as items above your repositories, then Delegate them to agents. A panel and a board show the items and the agent tasks behind them.',
  panels: [
    {
      id: 'projects', title: 'Projects', icon: 'board', order: 18, component: ProjectsPanel,
      command: { keywords: 'backlog items board plan work todo delegate agents tasks' },
    },
  ],
  commands: [
    { id: 'projects.open', title: 'Open Project…', category: 'Projects', keywords: 'backlog items plan', run: () => void openProject() },
    { id: 'projects.new', title: 'New Project…', category: 'Projects', keywords: 'create backlog items plan', run: () => void newProject() },
    {
      id: 'projects.newItem', title: 'New Project Item…', category: 'Projects', keywords: 'add work item task title',
      run: () => openNewItem(useProjects.getState().selected),
    },
    {
      id: 'projects.plan', title: 'Plan with Agent…', category: 'Projects', keywords: 'goal split items agent planner ai',
      run: () => openPlan(useProjects.getState().selected),
    },
    { id: 'projects.board', title: 'Show Project Board', category: 'Projects', keywords: 'kanban columns status drag', run: () => void showBoard() },
  ],
  activate: subscribeAutoStart,
  tabKinds: [{ id: BOARD_VIEW, component: BoardView, icon: 'board', restore: true }],
})
