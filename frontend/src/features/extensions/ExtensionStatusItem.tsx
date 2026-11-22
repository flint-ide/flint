import { StatusItem } from '@flint-ide/ui'
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
    <StatusItem className={`status-extension${severity}`} tip={status.tooltip || status.text} onClick={() => void run()}>
      {status.text}
    </StatusItem>
  ) : (
    <span className={`status-extension${severity}`} title={status.tooltip || undefined}>
      {status.text}
    </span>
  )
}
