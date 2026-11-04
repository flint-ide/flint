// External extensions (Go: internal/features/extensions, docs/design/extensions.md):
// Settings ▸ Extensions ("Show Extensions"), "Install Extension from Folder…", the "extensions"
// settings section, and — once the window is up — each installed extension's
// commands, panels, Settings page and status items, contributed under
// extension:<id> and withdrawn when it is turned off or uninstalled.
import { lazy } from 'react'
import { defineFeature } from '../../lib/features'
import { useEditor } from '../../store/editor'
import { extensionsSettings, featureToggleSettings } from './settings'
import { installFromFolder, subscribeExtensions } from './store'

const ExtensionsSection = lazy(() => import('./ExtensionsSection').then((m) => ({ default: m.ExtensionsSection })))

export default defineFeature({
  id: 'extensions',
  settings: [extensionsSettings, featureToggleSettings],
  commands: [
    {
      id: 'extensions.show',
      title: 'Show Extensions',
      category: 'Extensions',
      keywords: 'extension plugin installed manage list settings marketplace repository',
      run: () => useEditor.getState().openSettings('extensions'),
    },
    {
      id: 'extensions.install',
      title: 'Install Extension from Folder…',
      category: 'Extensions',
      keywords: 'extension plugin add',
      run: () => installFromFolder(),
    },
  ],
  settingsPages: [{ id: 'extensions', label: 'Extensions', order: 155, component: ExtensionsSection }],
  // After the first render: the list comes from Go (which reads the manifests
  // then, not before the window) and the extensions' contributions follow.
  activate: subscribeExtensions,
})
