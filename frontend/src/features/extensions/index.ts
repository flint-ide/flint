// The external extensions feature (Go: internal/features/extensions):
// Settings ▸ Extensions, "Install Extension from Folder…" and the
// contributions of installed extensions (color themes, contributed under
// "extension:<id>" by the store). The list is read after the first render,
// so startup does no extra I/O before the window paints.
import { lazy } from 'react'
import { defineFeature } from '../../lib/features'
import { installFromFolder, watchExtensions } from './store'

const ExtensionsSection = lazy(() => import('./ExtensionsSection').then((m) => ({ default: m.ExtensionsSection })))

export default defineFeature({
  id: 'extensions',
  commands: [
    {
      id: 'extensions.installFromFolder', title: 'Install Extension from Folder…', category: 'Extensions',
      keywords: 'extension plugin theme add', run: () => void installFromFolder(),
    },
  ],
  settingsPages: [{ id: 'extensions', label: 'Extensions', order: 155, component: ExtensionsSection }],
  activate: watchExtensions,
})
