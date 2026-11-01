// AI in the file editor: Edit with Agent (⌘I, the default agent rewrites the
// selection) and inline suggestions from a code model (settings: completions).
// Wires lib/inlineEditWidget.ts and lib/inlineCompletion.ts to Go and keeps
// the completion status the status bar shows.
import { Prec, type Extension } from '@codemirror/state'
import { keymap } from '@codemirror/view'
import * as Agent from '../../bindings/flint/agentapi'
import * as Completions from '../../bindings/flint/completionsapi'
import { EditRequest } from '../../bindings/flint/internal/agent/models'
import { Request as CompletionRequest } from '../../bindings/flint/internal/completions/models'
import { errorMessage } from '../api/backend'
import { createStore } from '../lib/store'
import { closeInlineEdit, inlineEdit, openInlineEdit } from '../lib/inlineEditWidget'
import { dismissSuggestion, inlineCompletion } from '../lib/inlineCompletion'
import { escapeKey } from '../lib/editorAiKeys'
import { useEditor, viewFor } from './editor'
import { useSettings } from './settings'

interface CompletionStatus {
  /** The last request's error ('' after a success): shown in the status bar, never as a toast. */
  error: string
}

export const useCompletionStatus = createStore<CompletionStatus>(() => ({ error: '' }))

const setError = (error: string) => {
  if (useCompletionStatus.getState().error !== error) useCompletionStatus.setState({ error })
}

// Errors keep their first line: Go's messages are short already.
const firstLine = (err: unknown) => errorMessage(err).split('\n')[0]

/** The AI extensions of the editor of the file at path. */
export function editorAi(path: string): Extension {
  return [
    inlineEdit({
      path,
      contextLines: () => useSettings.getState().settings.inlineEdit.contextLines,
      run: (req) => Agent.InlineEdit(EditRequest.createFrom(req)),
      errorText: firstLine,
    }),
    inlineCompletion({
      options: () => useSettings.getState().settings.completions,
      complete: (editor, prefix, suffix) => Completions.Complete(editor, CompletionRequest.createFrom({ path, prefix, suffix })),
      onSuccess: () => setError(''),
      onError: (err) => {
        // Turned off meanwhile: not an error worth showing.
        if (useSettings.getState().settings.completions.enabled) setError(firstLine(err))
      },
    }),
    Prec.highest(keymap.of([{ key: 'Escape', run: escapeKey(dismissSuggestion, closeInlineEdit) }])),
  ]
}

/** ⌘I: opens the inline edit prompt in the active file editor. */
export function openInlineEditInActiveEditor(): void {
  const id = useEditor.getState().activeId
  const view = id ? viewFor(id) : undefined
  if (view) openInlineEdit(view)
}

// Turning completions off (or changing them) clears a stale error.
useSettings.subscribe((s, prev) => {
  if (s.settings.completions !== prev.settings.completions) setError('')
})
