// The Docker feature (Go: internal/features/docker): the Docker panel, its
// Settings page and the "docker" settings section. Container log and shell tabs
// are terminal tabs (store/terminal.ts).
import { lazy } from 'react'
import { defineFeature } from '../../lib/features'
import { dockerSettings } from './settings'

// Fetched when first opened, so their code is not parsed at startup.
const DockerPanel = lazy(() => import('./DockerPanel').then((m) => ({ default: m.DockerPanel })))
const DockerSection = lazy(() => import('./DockerSection').then((m) => ({ default: m.DockerSection })))

export default defineFeature({
  id: 'docker',
  settings: [dockerSettings],
  panels: [
    {
      id: 'docker', title: 'Docker', icon: 'ship', order: 80, component: DockerPanel,
      command: { title: 'Docker', keywords: 'containers compose logs' },
    },
  ],
  settingsPages: [{ id: 'docker', label: 'Docker', order: 110, component: DockerSection }],
})
