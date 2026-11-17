// Commands of agent tasks (store/agentTasks.ts) and of the Agents panel
// (store/agents.ts); the "Show …" commands come from the panel registry.
import { registerCommands } from '../lib/commands'
import { useGit } from '../store/git'
import { openNewTask } from '../store/agentTasks'
import { peekAttention } from '../store/agents'
import { showToast } from '../store/dialog'

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
    {
      id: 'agent.peekAttention',
      title: 'Peek at Agent Needing Attention',
      category: 'Agent',
      keywords: 'waiting approval permission failed finished chat overlay',
      run: () => {
        if (!peekAttention()) showToast('No agent needs you')
      },
    },
  ])
}
