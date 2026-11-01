import { useSettings } from '../../store/settings'
import { validBranchPrefix, validWorktreeDir, type AgentTaskSettings } from '../../lib/agentTasks'
import { Row, TextField } from './fields'

/** Settings ▸ Agents ▸ Agent tasks: where worktrees go and how branches are named. */
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
    </>
  )
}
