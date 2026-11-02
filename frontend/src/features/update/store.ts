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

/** Hides the notice until the next launch. */
export function dismissUpdate(): void {
  useUpdate.setState({ offer: null })
}
