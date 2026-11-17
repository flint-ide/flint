import { useEditor } from '../../store/editor'
import { useMachines } from '../../store/machines'
import { machinesSummary } from '../../lib/machines'

/** Status bar item while a machine is connecting, reconnecting or has failed; nothing otherwise. */
export function MachinesStatus() {
  const statuses = useMachines((s) => s.statuses)
  const summary = machinesSummary(statuses)
  if (!summary) return null
  return (
    <button
      className={`status-action status-machines ${summary.tone}`}
      title={`${summary.details.join('\n')}\n\nClick to open Settings ▸ Machines`}
      onClick={() => useEditor.getState().openSettings('machines')}
    >
      {summary.tone === 'error' ? '⚠' : '⟳'} {summary.label}
    </button>
  )
}
