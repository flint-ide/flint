// The installed extensions (Go: internal/features/extensions) and their
// contributions: each usable extension contributes under the source
// "extension:<id>", withdrawn as a whole when it is uninstalled or the list
// is read again.
import * as ExtensionsAPI from '../../../bindings/flint/internal/features/extensions/api'
import { EventsOn } from '../../wails/runtime'
import { createStore } from '../../lib/store'
import { contribute } from '../../lib/contributions'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { coerceExtensions, themeContributions, type InstalledExtension } from './extensions'

interface ExtensionsState {
  extensions: readonly InstalledExtension[]
  /** The list has been read once. */
  loaded: boolean
  /** An install or uninstall is running (its native dialog may be open). */
  busy: boolean
  /** Why the list couldn't be read; '' when it could. */
  error: string
}

export const useExtensions = createStore<ExtensionsState>(() => ({ extensions: [], loaded: false, busy: false, error: '' }))

let withdraw: (() => void)[] = []

/** Replaces every extension's contributions with those of list. */
export function syncContributions(list: readonly InstalledExtension[]): void {
  withdraw.forEach((off) => off())
  withdraw = []
  for (const e of list) {
    const themes = themeContributions(e)
    if (themes.length === 0) continue
    try {
      withdraw.push(contribute(`extension:${e.id}`, { themes }))
    } catch (err) {
      showToast(`Extension ${e.name}: ${errorMessage(err)}`, 'error')
    }
  }
}

/** Reads the installed extensions again. */
export async function loadExtensions(): Promise<void> {
  try {
    const list = coerceExtensions(await ExtensionsAPI.List())
    syncContributions(list)
    useExtensions.setState({ extensions: list, loaded: true, error: '' })
  } catch (err) {
    useExtensions.setState({ loaded: true, error: errorMessage(err) })
  }
}

async function whileBusy(run: () => Promise<void>): Promise<void> {
  if (useExtensions.getState().busy) return
  useExtensions.setState({ busy: true })
  try {
    await run()
  } catch (err) {
    showToast(errorMessage(err), 'error')
  } finally {
    useExtensions.setState({ busy: false })
  }
}

// Install and uninstall make Go emit extensions:changed, which reloads the list.

/** Settings ▸ Extensions ▸ Install from Folder…: Go asks for the folder and the user's approval. */
export function installFromFolder(): Promise<void> {
  return whileBusy(async () => {
    const ext = await ExtensionsAPI.InstallFromFolder()
    if (ext) showToast(`Installed ${ext.name} ${ext.version}`)
  })
}

/** Uninstalls after Go's native confirmation. */
export function uninstallExtension(id: string): Promise<void> {
  return whileBusy(async () => {
    await ExtensionsAPI.Uninstall(id)
  })
}

/** Reads the list now and again whenever Go says it changed; the result unsubscribes. */
export function watchExtensions(): () => void {
  void loadExtensions()
  return EventsOn('extensions:changed', () => void loadExtensions())
}
