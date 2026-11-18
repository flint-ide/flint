import { useEffect, useState } from 'react'
import * as AlertsAPI from '../../../bindings/flint/alertsapi'
import { useSettings } from '../../store/settings'
import { errorMessage } from '../../api/backend'
import { notificationStatusLabel } from '../../lib/attention'
import { isMac } from '../../lib/platform'
import { Row } from './fields'

/** Settings ▸ Agents ▸ Notifications: what notifies, whether the system lets it, and a test. */
export function NotificationsSection() {
  const notify = useSettings((s) => s.settings.notifyInBackground)
  const notifyApproval = useSettings((s) => s.settings.notifyOnApproval)
  const update = useSettings((s) => s.update)
  const [status, setStatus] = useState<{ state: string; detail: string } | null>(null)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState('')
  const read = () => AlertsAPI.Status().then(setStatus, (err) => setStatus({ state: 'unavailable', detail: errorMessage(err) }))
  useEffect(() => {
    void read()
  }, [])
  const test = async () => {
    setTesting(true)
    setTestResult('')
    try {
      await AlertsAPI.Test()
      setTestResult('Sent: it shows like this when an agent needs you.')
    } catch (err) {
      setTestResult(`Not delivered: ${errorMessage(err)}`)
    } finally {
      setTesting(false)
      void read()
    }
  }
  const badge = isMac() ? ' and a count on the Dock icon' : ''
  return (
    <>
      <h3>Notifications</h3>
      <Row
        label="Tell me when work finishes in the background"
        hint={`A desktop notification${badge} when an agent's turn, a run, tests, CI or a project's automatic start finish or fail while Flint isn't the active app. Off: nothing is sent and there is no badge; a budget warning waits until you are back.`}
      >
        <input type="checkbox" checked={notify} onChange={(e) => update({ notifyInBackground: e.target.checked })} />
      </Row>
      <Row
        label="Tell me when an agent waits for my approval"
        hint={`Also a desktop notification when an agent asks for approval (in a chat, or an agent CLI in a terminal) or Flint asks you a question in a dialog (an agent's action, an agent directing others, a project's start): at once in the background, or when you leave Flint while it still waits. It counts on the badge until you answer.${notify ? '' : ' Needs "Tell me when work finishes in the background", which is off.'}`}
      >
        <input type="checkbox" checked={notifyApproval} onChange={(e) => update({ notifyOnApproval: e.target.checked })} />
      </Row>
      <Row label="Desktop notifications" hint={status?.detail || (isMac() ? '' : 'Linux has no Dock badge: the count shows on the Agents button in the activity bar.')}>
        <span className={`notify-status ${status?.state ?? ''}`}>{status ? notificationStatusLabel(status.state) : 'Checking…'}</span>
      </Row>
      <div className="settings-actions">
        <button disabled={testing} onClick={() => void test()}>Send Test Notification</button>
        {testResult && <span className="muted">{testResult}</span>}
      </div>
    </>
  )
}
