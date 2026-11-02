// The release check feature (Go: internal/features/update): "Check for
// Updates…" (palette and Help menu), the status bar notice, the Settings page
// and the "checkForUpdates" key. Go decides when to ask GitHub.
import { lazy } from 'react'
import { EventsOn } from '../../wails/runtime'
import { defineFeature } from '../../lib/features'
import { updateSettings } from './settings'
import { checkForUpdates, subscribeUpdates } from './store'
import { UpdateWidget } from './UpdateWidget'

const UpdatesSection = lazy(() => import('./UpdatesSection').then((m) => ({ default: m.UpdatesSection })))

export default defineFeature({
  id: 'update',
  settings: [updateSettings],
  commands: [
    { id: 'help.checkForUpdates', title: 'Check for Updates…', category: 'Help', keywords: 'update upgrade release version new download', run: () => void checkForUpdates() },
  ],
  settingsPages: [{ id: 'updates', label: 'Updates', order: 150, component: UpdatesSection }],
  statusItems: [{ id: 'update', order: 100, component: UpdateWidget }],
  activate: () => {
    subscribeUpdates()
    return EventsOn('menu:check-updates', () => void checkForUpdates()) // Help ▸ Check for Updates…
  },
})
