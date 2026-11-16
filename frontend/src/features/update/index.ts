// The release check feature (Go: internal/features/update): "Check for
// Updates…" (palette and Help menu), the status bar notice, the Settings page,
// the "checkForUpdates" key, and the "What's New" tab that opens on the first
// launch after an update. Go decides when to ask GitHub.
import { lazy } from 'react'
import { EventsOn } from '../../wails/runtime'
import { defineFeature } from '../../lib/features'
import { updateSettings } from './settings'
import { RELEASE_NOTES_VIEW } from './notes'
import { checkForUpdates, openReleaseNotes, showWhatsNew, subscribeUpdates } from './store'
import { UpdateWidget } from './UpdateWidget'

const ReleaseNotesView = lazy(() => import('./ReleaseNotesView').then((m) => ({ default: m.ReleaseNotesView })))
const UpdatesSection = lazy(() => import('./UpdatesSection').then((m) => ({ default: m.UpdatesSection })))

export default defineFeature({
  id: 'update',
  settings: [updateSettings],
  commands: [
    { id: 'help.checkForUpdates', title: 'Check for Updates…', category: 'Help', keywords: 'update upgrade release version new download', run: () => void checkForUpdates() },
    { id: 'help.releaseNotes', title: 'View Release Notes', category: 'Help', keywords: "changelog what's new whats changes features fixes version", run: () => void openReleaseNotes() },
  ],
  tabKinds: [{ id: RELEASE_NOTES_VIEW, component: ReleaseNotesView, restore: true }],
  settingsPages: [{ id: 'updates', label: 'Updates', order: 150, component: UpdatesSection }],
  statusItems: [{ id: 'update', order: 100, component: UpdateWidget }],
  activate: () => {
    subscribeUpdates()
    void showWhatsNew() // the first launch of a new version
    return EventsOn('menu:check-updates', () => void checkForUpdates()) // Help ▸ Check for Updates…
  },
})
