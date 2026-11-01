// Commands of the prompt library (store/prompts.ts); the "/" picker lives in the chat input.
import { registerCommands } from '../lib/commands'
import { openPromptDialog } from '../store/prompts'

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
  ])
}
