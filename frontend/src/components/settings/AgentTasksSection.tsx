import { useSettings } from '../../store/settings'
import { MAX_PARALLEL_LIMIT, validBranchPrefix, validWorktreeDir, type AgentTaskSettings } from '../../lib/agentTasks'
import { NumberField, Row, TextField } from './fields'

/** Settings ▸ Agents ▸ Agent tasks: where worktrees go, how branches are named and how many work at once. */
export function AgentTasksSection() {
  const tasks = useSettings((s) => s.settings.agentTasks)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<AgentTaskSettings>) => update((s) => ({ agentTasks: { ...s.agentTasks, ...change } }))
  return (
    <>
      <h3>Agent tasks</h3>
      <Row
        label="Worktree folder"
        hint={'Where New Agent Task puts worktrees, as <folder>/<repository>/<branch>. An absolute path or ~/…; empty puts them in "<repository>.worktrees" next to the main checkout, like the branch widget.'}
      >
        <TextField value={tasks.worktreeDir} valid={validWorktreeDir} onCommit={(worktreeDir) => set({ worktreeDir })} />
      </Row>
      <Row label="Branch prefix" hint='Starts every task branch: "agent/" gives agent/add-rate-limiting. Letters, digits, . _ - and /; may be empty.'>
        <TextField value={tasks.branchPrefix} valid={validBranchPrefix} onCommit={(branchPrefix) => set({ branchPrefix })} />
      </Row>
      <Row
        label="Tasks working at the same time"
        hint="A task started beyond this number waits in the queue (its chat is not started and nothing is sent) until a working task finishes, fails or is stopped. 0 means no limit."
      >
        <NumberField value={tasks.maxParallel} min={0} max={MAX_PARALLEL_LIMIT} step={1} onCommit={(maxParallel) => set({ maxParallel: Math.round(maxParallel) })} />
      </Row>
    </>
  )
}
