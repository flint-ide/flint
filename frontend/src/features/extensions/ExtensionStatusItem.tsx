import * as API from '../../../bindings/flint/internal/features/extensions/api'
import { errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { useExtensions } from './store'

/** A status bar item of an extension: its last status/update, hidden while it has no text. */
export function ExtensionStatusItem({ ext, item }: { ext: string; item: string }) {
  const status = useExtensions((s) => s.status[ext]?.[item])
  if (!status?.text) return null
  const severity = status.severity ? ` ${status.severity}` : ''
  const run = status.command
    ? () => API.Execute(ext, status.command, null).catch((err: unknown) => showToast(errorMessage(err), 'error'))
    : undefined
  return run ? (
    <button className={`status-action status-extension${severity}`} title={status.tooltip || undefined} onClick={() => void run()}>
      {status.text}
    </button>
  ) : (
    <span className={`status-extension${severity}`} title={status.tooltip || undefined}>
      {status.text}
    </span>
  )
}
