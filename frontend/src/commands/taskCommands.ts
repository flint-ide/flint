// Commands of agent tasks (store/agentTasks.ts); the "Show Agent Tasks" command
// comes from the panel registry.
import { registerCommands } from '../lib/commands'
import { useGit } from '../store/git'
import { openNewTask } from '../store/agentTasks'

export function registerTaskCommands(): () => void {
  return registerCommands([
    {
      id: 'agent.newTask',
      title: 'New Agent Task…',
      category: 'Agent',
      keywords: 'worktree branch parallel claude codex background',
      when: () => useGit.getState().isRepo,
      run: openNewTask,
    },
  ])
}
