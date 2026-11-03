// The extensions repository (ExtensionsAPI.Catalog): what it offers, read by
// Go from a git clone of flint-ide/extensions. Installing builds a Go
// extension on this machine, then goes through the same native confirmation
// as "Install from Folder…".
import { createStore } from '../../lib/store'
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import type { Entry, Listing } from '../../../bindings/flint/internal/extensions/catalog/models'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { loadExtensions } from './store'
import { sentence } from './catalogModel'

export type { Entry, Listing }

interface CatalogState {
  listing: Listing | null
  /** Fetching or cloning the repository. */
  loading: boolean
  error: string
  /** Repository folders being built and installed. */
  busy: readonly string[]
}

export const useCatalog = createStore<CatalogState>(() => ({ listing: null, loading: false, error: '', busy: [] }))

let pending: Promise<void> | null = null

/** Reads the repository (cloning it the first time); refresh fetches it again. */
export function loadCatalog(refresh = false): Promise<void> {
  if (pending) return pending
  useCatalog.setState({ loading: true })
  pending = (refresh ? API.RefreshCatalog() : API.Catalog())
    .then((listing) => useCatalog.setState({ listing, error: '' }))
    .catch((err: unknown) => useCatalog.setState({ error: errorMessage(err) }))
    .finally(() => {
      pending = null
      useCatalog.setState({ loading: false })
    })
  return pending
}

/** Builds (Go extensions) and installs the extension in the repository's folder. */
export async function installFromCatalog(folder: string): Promise<void> {
  if (useCatalog.getState().busy.includes(folder)) return
  useCatalog.setState((s) => ({ busy: [...s.busy, folder] }))
  try {
    const info = await API.InstallFromCatalog(folder)
    if (!info) return // cancelled in the confirmation
    showToast(`Installed ${info.name} ${info.version}`)
    await loadExtensions()
  } catch (err) {
    showToast(`Not installed. ${sentence(errorMessage(err))}`, 'error')
  } finally {
    useCatalog.setState((s) => ({ busy: s.busy.filter((b) => b !== folder) }))
  }
}
