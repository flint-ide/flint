import { StatusItem } from '@flint-ide/ui'
import { Workflow } from '@flint-ide/ui/icons'
import { useWorkspace } from '../../store/workspace'
import { needsYouText } from './workflows'
import { openRun, useWorkflows } from './store'

/** "Workflows: 2 need you" in the status bar; nothing while no run waits. One run opens its tab, several open the panel. */
export function WorkflowsStatus() {
  const waiting = useWorkflows((s) => s.runs?.filter((r) => r.needsUser) ?? NONE)
  if (waiting.length === 0) return null
  const only = waiting.length === 1 ? waiting[0] : null
  return (
    <StatusItem
      className="on"
      tip={only ? `${only.title || only.workflow}: open the run` : 'Open the Workflows panel: these runs wait for you'}
      onClick={() => (only ? openRun(only.id) : useWorkspace.getState().setSidebar('workflows'))}
    >
      <Workflow aria-hidden />
      {needsYouText(waiting.length)}
    </StatusItem>
  )
}

const NONE: never[] = []
