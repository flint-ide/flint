// The TODO list feature (Go: internal/features/todo): the TODO panel, its
// Settings page and the "todoExclude" settings key.
import { lazy } from 'react'
import { defineFeature } from '../../lib/features'
import { todoSettings } from './settings'

// Fetched when first opened, so their code is not parsed at startup.
const TodoPanel = lazy(() => import('./TodoPanel').then((m) => ({ default: m.TodoPanel })))
const TodoSection = lazy(() => import('./TodoSection').then((m) => ({ default: m.TodoSection })))

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
