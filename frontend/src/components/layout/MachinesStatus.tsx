import { StatusItem } from '@flint-ide/ui'
import { useEditor } from '../../store/editor'
import { useMachines } from '../../store/machines'
import { machinesSummary } from '../../lib/machines'

/** Status bar item while a machine is connecting, reconnecting or has failed; nothing otherwise. */
export function MachinesStatus() {
  const statuses = useMachines((s) => s.statuses)
  const summary = machinesSummary(statuses)
  if (!summary) return null
  return (
    <StatusItem
      className={summary.tone === 'error' ? 'status-error' : undefined}
      tip={`${summary.details.join('\n')}\n\nClick to open Settings ▸ Machines`}
      onClick={() => useEditor.getState().openSettings('machines')}
    >
      {summary.tone === 'error' ? '⚠' : '⟳'} {summary.label}
    </StatusItem>
  )
}
