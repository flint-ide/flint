// New Flint releases: the notice in the status bar and "Check for Updates…".
// Go decides when to ask GitHub (once a day, never when turned off in
// Settings) and remembers a skipped release; dismissing only hides the notice
// until the next launch.
import * as UpdateAPI from '../../../bindings/flint/internal/features/update/api'
import { EventsOn } from '../../wails/runtime'
import { createStore } from '../../lib/store'
import { coerceUpdate, offerable, offerText, upToDateText, type UpdateResult } from './update'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { coerceNotes, notesTitle, RELEASE_NOTES_VIEW, type ReleaseNotes } from './notes'

interface UpdateState {
  /** The release the notice offers; null = no notice. */
  offer: UpdateResult | null
  /** A manual check is running. */
  checking: boolean
}

export const useUpdate = createStore<UpdateState>(() => ({ offer: null, checking: false }))

const offer = (v: unknown) => {
  const r = coerceUpdate(v)
  if (r && offerable(r)) useUpdate.setState({ offer: r })
}

let subscribed = false
export function subscribeUpdates(): void {
  if (subscribed) return
  subscribed = true
  EventsOn('update:available', offer)
  // The background check may have finished before this subscription.
  UpdateAPI.Pending().then(offer, () => {})
}

/** "Check for Updates…": asks now and always answers, up to date or not. */
export async function checkForUpdates(): Promise<void> {
  if (useUpdate.getState().checking) return
  useUpdate.setState({ checking: true })
  try {
    const r = coerceUpdate(await UpdateAPI.Check())
    if (!r) throw new Error('unexpected answer')
    if (offerable(r)) {
      useUpdate.setState({ offer: r })
      showToast(offerText(r)) // the status bar notice alone is easy to miss right after asking
    } else {
      showToast(upToDateText(r))
    }
  } catch (err) {
    showToast(`Update check failed: ${errorMessage(err)}`, 'error')
  } finally {
    useUpdate.setState({ checking: false })
  }
}

/** Opens the release page (Go picks and checks the link) and hides the notice. */
export function downloadUpdate(): void {
  UpdateAPI.OpenRelease().catch((err) => showToast(errorMessage(err), 'error'))
  dismissUpdate()
}

/** No notice for this release again; a newer one is offered as usual. */
export async function skipUpdate(): Promise<void> {
  const tag = useUpdate.getState().offer?.latest.tag
  if (!tag) return
  try {
    await UpdateAPI.Skip(tag)
    dismissUpdate()
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

const showNotes = (notes: ReleaseNotes) =>
  useEditor.getState().openContributed(RELEASE_NOTES_VIEW, notes.tag, notesTitle(notes), notes)

/** Resolves once the startup folder is open and its session restored. */
function workspaceReady(): Promise<void> {
  if (useWorkspace.getState().ready) return Promise.resolve()
  return new Promise((resolve) => {
    const off = useWorkspace.subscribe((s) => {
      if (!s.ready) return
      off()
      resolve()
    })
  })
}

/** On the first launch after an update: opens what changed in this version (Go decides whether it is that launch). */
export async function showWhatsNew(): Promise<void> {
  try {
    const notes = coerceNotes(await UpdateAPI.WhatsNew())
    if (!notes) return
    // After the session's tabs came back, so the notes end up in front of them.
    await workspaceReady()
    showNotes(notes)
  } catch {
    // Offline at startup is not worth an error; Go tries again at the next launch.
  }
}

/** "View Release Notes": what changed in the running version, whenever asked. */
export async function openReleaseNotes(): Promise<void> {
  try {
    const notes = coerceNotes(await UpdateAPI.ReleaseNotes())
    if (!notes) throw new Error('unexpected answer')
    showNotes(notes)
  } catch (err) {
    showToast(`Release notes are not available: ${errorMessage(err)}`, 'error')
  }
}

/** Opens a link of the notes in the browser (Go lets only Flint's repository through). */
export function openNotesLink(url: string): void {
  UpdateAPI.OpenLink(url).catch((err) => showToast(errorMessage(err), 'error'))
}

/** Hides the notice until the next launch. */
export function dismissUpdate(): void {
  useUpdate.setState({ offer: null })
}
