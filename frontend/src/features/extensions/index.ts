// External extensions (Go: internal/features/extensions, docs/design/extensions.md):
// Settings ▸ Extensions ("Show Extensions"), "Install Extension from Folder…", the "extensions"
// settings section, and — once the window is up — each installed extension's
// commands, panels, Settings page and status items, contributed under
// extension:<id> and withdrawn when it is turned off or uninstalled.
import { lazy } from 'react'
import { defineFeature } from '../../lib/features'
import { useEditor, viewFor } from '../../store/editor'
import { showToast } from '../../store/dialog'
import { showExtensionQuickFixes, toggleDecorationsHidden } from '../../lib/editorDecorations'
import { extensionsSettings, featureToggleSettings } from './settings'
import { installFromFolder, subscribeExtensions } from './store'

/** The editor of the active tab, if it is a file. */
const activeView = () => viewFor(useEditor.getState().activeId ?? '')

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
    {
      id: 'extensions.quickFix',
      title: 'Show Extension Quick Fixes',
      category: 'Extensions',
      keys: 'Alt+Shift+Enter',
      keywords: 'extension decoration fix action lightbulb intention',
      when: () => !!activeView(),
      run: () => {
        const view = activeView()
        if (view && !showExtensionQuickFixes(view)) showToast('No extension quick fixes on this line')
      },
    },
    {
      id: 'extensions.toggleDecorations',
      title: 'Toggle Extension Decorations in This Editor',
      category: 'Extensions',
      keywords: 'extension decoration marks gutter hints hide show',
      when: () => !!activeView(),
      run: () => {
        const view = activeView()
        if (view) showToast(toggleDecorationsHidden(view) ? 'Extension decorations hidden in this editor' : 'Extension decorations shown')
      },
    },
  ],
  settingsPages: [{ id: 'extensions', label: 'Extensions', order: 155, component: ExtensionsSection }],
  // After the first render: the list comes from Go (which reads the manifests
  // then, not before the window) and the extensions' contributions follow.
  activate: subscribeExtensions,
})
