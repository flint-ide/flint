// The Backlog feature (Go: internal/features/backlog): work items for one or more
// repositories, shown as a sidebar panel and a board tab, handed to agents with
// Delegate. The panel, its commands and the board exist only while the feature runs.
import { lazy } from 'react'
import { lazyPreload } from '../../lib/lazyPreload'
import { defineFeature } from '../../lib/features'
import { BOARD_VIEW } from './backlog'
import { newBacklog, openBacklog, openNewItem, showBoard, useBacklog } from './store'

const BacklogPanel = lazyPreload(() => import('./BacklogPanel').then((m) => ({ default: m.BacklogPanel })))
const BoardView = lazy(() => import('./BoardView').then((m) => ({ default: m.BoardView })))

export default defineFeature({
  id: 'backlog',
  optional: true,
  name: 'Backlog',
  description: 'Plan work as items above your repositories, then Delegate them to agents. A panel and a board show the items and the agent tasks behind them.',
  panels: [
    {
      id: 'backlog', title: 'Backlog', icon: 'checkbox', order: 34, component: BacklogPanel,
      command: { keywords: 'items board plan work todo delegate agents tasks' },
    },
  ],
  commands: [
    { id: 'backlog.open', title: 'Open Backlog…', category: 'Backlog', keywords: 'folder items plan', run: () => void openBacklog() },
    { id: 'backlog.new', title: 'New Backlog…', category: 'Backlog', keywords: 'create folder items plan', run: () => void newBacklog() },
    {
      id: 'backlog.newItem', title: 'New Backlog Item…', category: 'Backlog', keywords: 'add work item task title',
      run: () => openNewItem(useBacklog.getState().selected),
    },
    { id: 'backlog.board', title: 'Show Backlog Board', category: 'Backlog', keywords: 'kanban columns status drag', run: () => void showBoard() },
  ],
  tabKinds: [{ id: BOARD_VIEW, component: BoardView, icon: 'checkbox', restore: true }],
})
