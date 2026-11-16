// The signed extension index (ExtensionsAPI.Index): one JSON document that
// lists extensions with prebuilt archives, verified by Go against a public
// key before anything in it is shown. Go downloads it only from fetchIndex,
// which runs when the user clicks Browse Extensions / Check for Updates or
// runs the palette commands — never on its own. Installing goes through the
// same native confirmation as "Install from Folder…".
import { createStore } from '../../lib/store'
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import type { IndexEntry, IndexStatus } from '../../../bindings/flint/internal/features/extensions/models'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { loadExtensions, useExtensions } from './store'
import { sentence } from './catalogModel'
import { availableUpdates, updatesSummary } from './indexModel'

export type { IndexEntry, IndexStatus }

interface IndexState {
  status: IndexStatus | null
  /** Downloading the index. */
  loading: boolean
  error: string
  /** Ids being downloaded and installed. */
  busy: readonly string[]
}

export const useIndex = createStore<IndexState>(() => ({ status: null, loading: false, error: '', busy: [] }))

/** Reads what Go has (the last verified index, or its cache); makes no request. */
export async function loadIndex(): Promise<void> {
  try {
    useIndex.setState({ status: await API.Index() })
  } catch (err) {
    useIndex.setState({ error: errorMessage(err) })
  }
}

let pending: Promise<boolean> | null = null

/** Downloads and verifies the index (a user action); false when it failed. */
export function fetchIndex(): Promise<boolean> {
  if (pending) return pending
  useIndex.setState({ loading: true })
  pending = API.FetchIndex()
    .then((status) => {
      useIndex.setState({ status, error: '' })
      return true
    })
    .catch((err: unknown) => {
      useIndex.setState({ error: errorMessage(err) })
      return false
    })
    .finally(() => {
      pending = null
      useIndex.setState({ loading: false })
    })
  return pending
}

/** "Check for Extension Updates": fetches the index and says what it found. */
export async function checkForExtensionUpdates(): Promise<void> {
  if (!(await fetchIndex())) {
    showToast(`Extension updates not checked. ${sentence(useIndex.getState().error)}`, 'error')
    return
  }
  const installed = useExtensions.getState().list
  const updates = availableUpdates(useIndex.getState().status?.entries ?? [], installed)
  showToast(updatesSummary(updates, (id) => installed.find((i) => i.id === id)?.name ?? id))
}

/** Downloads, verifies and installs (or updates) an extension of the index. */
export async function installFromIndex(id: string): Promise<void> {
  if (useIndex.getState().busy.includes(id)) return
  useIndex.setState((s) => ({ busy: [...s.busy, id] }))
  try {
    const info = await API.InstallFromIndex(id)
    if (!info) return // cancelled in the confirmation
    showToast(`Installed ${info.name} ${info.version}`)
    await loadExtensions()
  } catch (err) {
    showToast(`Not installed. ${sentence(errorMessage(err))}`, 'error')
  } finally {
    useIndex.setState((s) => ({ busy: s.busy.filter((b) => b !== id) }))
  }
}
