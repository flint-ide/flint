import { useSettings } from '../../store/settings'
import { Row } from '../../components/settings/fields'
import { checkForUpdates, useUpdate } from './store'

/** Settings ▸ Updates: the daily release check. */
export function UpdatesSection() {
  const enabled = useSettings((s) => s.settings.checkForUpdates)
  const checking = useUpdate((s) => s.checking)
  const update = useSettings((s) => s.update)
  return (
    <section>
      <h2>Updates</h2>
      <Row label="Check for new releases" hint="Once a day Flint asks GitHub which release is the latest and shows a notice in the status bar when there is a newer one.">
        <input type="checkbox" checked={enabled} onChange={(e) => update({ checkForUpdates: e.target.checked })} />
      </Row>
      <div className="settings-actions">
        <button disabled={checking} onClick={() => void checkForUpdates()}>{checking ? 'Checking…' : 'Check Now'}</button>
      </div>
      <p className="muted settings-note">
        The check is one anonymous request to api.github.com for the latest release of flint-ide/flint, with Flint's version in the User-Agent and no
        account, token, cookie or identifier. Flint never downloads or installs anything itself: Download opens the release page in your browser.
        With the check off, GitHub is asked only when you choose Check Now or Help ▸ Check for Updates….
      </p>
    </section>
  )
}
