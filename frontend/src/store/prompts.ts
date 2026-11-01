// The prompt library (Go: PromptsAPI): the agent chat's "/" picker, the
// "Run Prompt…" and "New Prompt…" dialogs. A chosen prompt only ever fills the
// chat draft; the user reviews it and presses Send.
import { createStore } from '../lib/store'
import * as Prompts from '../../bindings/flint/promptsapi'
import { PromptInput, type PromptLibrary } from '../../bindings/flint/models'
import type { Prompt } from '../../bindings/flint/internal/prompts/models'
import { errorMessage } from '../api/backend'
import { askAgent } from '../lib/askAgent'
import { lastFileTab } from '../lib/prompts'
import { activeRoot } from './activeRoot'
import { showToast } from './dialog'
import { useEditor, viewFor } from './editor'

export type { Prompt, PromptLibrary }

interface PromptsState {
  /** The Run Prompt… or New Prompt… dialog, when open. */
  dialog: 'run' | 'new' | null
}

export const usePrompts = createStore<PromptsState>(() => ({ dialog: null }))

export function openPromptDialog(dialog: 'run' | 'new'): void {
  usePrompts.setState({ dialog })
}

export function closePromptDialog(): void {
  usePrompts.setState({ dialog: null })
}

/** The prompts for a workspace folder ('' = none), read fresh from disk; null after an error. */
export async function loadPrompts(root: string): Promise<PromptLibrary | null> {
  try {
    return await Prompts.List(root)
  } catch (err) {
    showToast(`Couldn't read the prompts: ${errorMessage(err)}`, 'error')
    return null
  }
}

/**
 * The prompt's text with its placeholders filled from the most recent editor
 * (selection, file, problems) and git (diff, branch); null after an error.
 */
export async function expandPrompt(root: string, prompt: Prompt, args: Record<string, string>): Promise<string | null> {
  try {
    return await Prompts.Expand(root, prompt.name, await editorInput(args))
  } catch (err) {
    showToast(`Couldn't fill in /${prompt.name}: ${errorMessage(err)}`, 'error')
    return null
  }
}

/** Run Prompt…: the expanded prompt goes into the current (or a new) chat's draft. */
export async function runPromptInChat(prompt: Prompt, args: Record<string, string>): Promise<void> {
  const root = activeRoot()
  const text = await expandPrompt(root, prompt, args)
  if (text !== null) await askAgent({ prompt: text, root, agent: prompt.agent || undefined })
}

/** New Prompt…: writes a prompt file from a template and opens it. */
export async function createPrompt(name: string, personal: boolean): Promise<boolean> {
  try {
    const path = await Prompts.Create(activeRoot(), personal, name)
    useEditor.getState().openFile(path)
    return true
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return false
  }
}

async function editorInput(args: Record<string, string>): Promise<PromptInput> {
  const { tabs, activeId, backStack } = useEditor.getState()
  const tab = lastFileTab(tabs, activeId, backStack)
  if (!tab || tab.kind !== 'file') return PromptInput.createFrom({ args })
  const view = viewFor(tab.id)
  let selection = ''
  let problems = ''
  if (view) {
    const { from, to } = view.state.selection.main
    selection = view.state.doc.sliceString(from, to)
    // The editor chunk is loaded whenever an editor exists.
    problems = (await import('../lib/lspAgent')).problemsText(view)
  }
  return PromptInput.createFrom({ file: tab.path, selection, problems, args })
}
