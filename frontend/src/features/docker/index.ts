// The Docker feature (Go: internal/features/docker): the Docker panel, its
// Settings page and the "docker" settings section. Container log and shell tabs
// are terminal tabs (store/terminal.ts).
import { lazyPreload } from '../../lib/lazyPreload'
import { defineFeature } from '../../lib/features'
import { dockerSettings } from './settings'

// Not parsed at startup; fetched in the background once the app is idle (or when first opened).
const DockerPanel = lazyPreload(() => import('./DockerPanel').then((m) => ({ default: m.DockerPanel })))
const DockerSection = lazyPreload(() => import('./DockerSection').then((m) => ({ default: m.DockerSection })))

export default defineFeature({
  id: 'docker',
  optional: true,
  name: 'Docker',
  description: 'Containers and Compose projects through the Docker Engine API: start, stop, logs, shells and compose up, down and pull.',
  settings: [dockerSettings],
  panels: [
    {
      id: 'docker', title: 'Docker', icon: 'ship', order: 80, component: DockerPanel,
      command: { title: 'Docker', keywords: 'containers compose logs' },
    },
  ],
  settingsPages: [{ id: 'docker', label: 'Docker', order: 110, component: DockerSection }],
})
