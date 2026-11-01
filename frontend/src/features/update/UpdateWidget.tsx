import { dismissUpdate, downloadUpdate, skipUpdate, useUpdate } from './store'
import { offerText } from './update'

/** A newer Flint release (found by the daily check or "Check for Updates…"). */
export function UpdateWidget() {
  const offer = useUpdate((s) => s.offer)
  if (!offer) return null
  const title = offer.latest.name && offer.latest.name !== offer.latest.tag ? `${offer.latest.name} — ` : ''
  return (
    <span className="status-update" role="status">
      <span className="truncate" title={`${title}${offerText(offer)}`}>Flint {offer.latest.tag} available</span>
      <button className="status-action on" title="Open the release page in your browser" onClick={downloadUpdate}>
        Download
      </button>
      <button className="status-action" title="Don't tell me about this version again" onClick={() => void skipUpdate()}>
        Skip This Version
      </button>
      <button className="status-action" title="Hide until Flint restarts" aria-label="Dismiss" onClick={dismissUpdate}>
        ✕
      </button>
    </span>
  )
}
