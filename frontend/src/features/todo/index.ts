// The TODO list feature (Go: internal/features/todo): the TODO panel, its
// Settings page and the "todoExclude" settings key.
import { lazyPreload } from '../../lib/lazyPreload'
import { defineFeature } from '../../lib/features'
import { todoSettings } from './settings'

// Not parsed at startup; fetched in the background once the app is idle (or when first opened).
const TodoPanel = lazyPreload(() => import('./TodoPanel').then((m) => ({ default: m.TodoPanel })))
const TodoSection = lazyPreload(() => import('./TodoSection').then((m) => ({ default: m.TodoSection })))

export default defineFeature({
  id: 'todo',
  settings: [todoSettings],
  panels: [
    {
      id: 'todo', title: 'TODO', icon: 'checkbox', order: 70, component: TodoPanel,
      command: { title: 'TODO List', keywords: 'fixme hack xxx bug notes comments' },
    },
  ],
  settingsPages: [{ id: 'todo', label: 'TODO List', order: 130, component: TodoSection }],
})
