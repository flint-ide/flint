import * as ImportAPI from '../../bindings/flint/importapi'
import { createStore } from '../lib/store'
import type { ImportItem, ImportSkipped } from '../lib/vscodeImport'
import { errorMessage } from '../api/backend'
import { flushSettings, replaceSettings } from './settings'
import { showToast } from './dialog'

// "Import from VS Code…": pick an editor (VS Code, Cursor, VSCodium), preview
// what Go would change, and save only when the user confirms. Go keeps the
// previewed plan; Apply names it by id, so what is saved is what was shown.

export interface ImportSource {
  id: string
  name: string
  userDir: string
}

export interface ImportPlan {
  id: string
  source: ImportSource
  items: ImportItem[]
  skipped: ImportSkipped[]
}

interface ImportState {
  open: boolean
  /** null until the editors were looked for. */
  sources: ImportSource[] | null
  plan: ImportPlan | null
  busy: boolean
  error: string
}

const closed: ImportState = { open: false, sources: null, plan: null, busy: false, error: '' }

export const useVSCodeImport = createStore<ImportState>(() => closed)

/** Opens the dialog and previews the first editor found. */
export async function openVSCodeImport(): Promise<void> {
  useVSCodeImport.setState({ ...closed, open: true, busy: true })
  try {
    const sources = (await ImportAPI.Sources()) ?? []
    useVSCodeImport.setState({ sources, busy: false })
    if (sources.length > 0) await previewImport(sources[0].id)
  } catch (err) {
    useVSCodeImport.setState({ busy: false, error: errorMessage(err) })
  }
}

/** Reads the chosen editor's files; nothing is written. */
export async function previewImport(sourceId: string): Promise<void> {
  useVSCodeImport.setState({ busy: true, error: '', plan: null })
  try {
    const plan = (await ImportAPI.Preview(sourceId)) as ImportPlan
    if (useVSCodeImport.getState().open) useVSCodeImport.setState({ plan, busy: false })
  } catch (err) {
    useVSCodeImport.setState({ busy: false, error: errorMessage(err) })
  }
}

/** Saves the previewed plan through Go's settings validation and applies it live. */
export async function applyImport(): Promise<void> {
  const { plan, busy } = useVSCodeImport.getState()
  if (!plan || busy) return
  useVSCodeImport.setState({ busy: true, error: '' })
  try {
    await flushSettings() // pending edits first, so the import builds on them
    replaceSettings(await ImportAPI.Apply(plan.id))
    useVSCodeImport.setState(closed)
    showToast(`Imported ${plan.items.length} ${plan.items.length === 1 ? 'setting' : 'settings'} from ${plan.source.name}`)
  } catch (err) {
    useVSCodeImport.setState({ busy: false, error: errorMessage(err) })
  }
}

export function closeVSCodeImport(): void {
  useVSCodeImport.setState(closed)
}
