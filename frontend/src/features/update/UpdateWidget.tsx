import { StatusItem } from '@flint-ide/ui'
import { X } from '@flint-ide/ui/icons'
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
      <StatusItem className="on" tip="Open the release page in your browser" onClick={downloadUpdate}>
        Download
      </StatusItem>
      <StatusItem tip="Don't tell me about this version again" onClick={() => void skipUpdate()}>
        Skip This Version
      </StatusItem>
      <StatusItem tip="Hide until Flint restarts" onClick={dismissUpdate}>
        <X aria-label="Dismiss" />
      </StatusItem>
    </span>
  )
}
