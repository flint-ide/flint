// Commands of the prompt library (store/prompts.ts); the "/" picker lives in the chat input.
// New Agent Definition… sits here too: a definition is a Markdown file like a prompt (store/agentDefs.ts).
import { registerCommands } from '../lib/commands'
import { openPromptDialog } from '../store/prompts'
import { openAgentDefDialog } from '../store/agentDefs'

export function registerPromptCommands(): () => void {
  return registerCommands([
    {
      id: 'agent.runPrompt',
      title: 'Run Prompt…',
      category: 'Agent',
      keywords: 'prompt library slash command template review test explain fix claude codex',
      run: () => openPromptDialog('run'),
    },
    {
      id: 'agent.newPrompt',
      title: 'New Prompt…',
      category: 'Agent',
      keywords: 'prompt library slash command template create',
      run: () => openPromptDialog('new'),
    },
    {
      id: 'agent.newDefinition',
      title: 'New Agent Definition…',
      category: 'Agent',
      keywords: 'agent definition role persona instructions subagent markdown create claude codex',
      run: openAgentDefDialog,
    },
  ])
}
