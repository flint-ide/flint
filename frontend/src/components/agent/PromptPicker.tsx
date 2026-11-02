import { useEffect, useMemo, useState, type KeyboardEvent, type ReactNode, type RefObject } from 'react'
import { matchPrompts, slashQuery } from '../../lib/prompts'
import { rootOf } from '../../lib/workspaceRoots'
import { useAgentChat, type ChatSession } from '../../store/agentChat'
import { expandPrompt, loadPrompts, type Prompt, type PromptLibrary } from '../../store/prompts'
import { useWorkspace } from '../../store/workspace'
import { PromptArgsForm, PromptList } from './PromptList'
import { useListCursor } from '../../lib/useListCursor'

export interface PromptPicker {
  /** Handles the chat input's key first; true when the picker used it. */
  onKeyDown: (e: KeyboardEvent<HTMLTextAreaElement>) => boolean
  element: ReactNode
}

/**
 * The "/" picker of a chat input: typing "/" (then a filter) lists the prompt
 * library; choosing one replaces the draft with the filled-in prompt for the
 * user to review. When nothing matches, or after Esc, Enter sends the text as
 * typed, so the agent's own commands (/compact…) still reach it.
 */
export function usePromptPicker(session: ChatSession, input: RefObject<HTMLTextAreaElement | null>): PromptPicker {
  const setDraft = useAgentChat((s) => s.setDraft)
  // Prompts come from the workspace folder the chat runs in (none for a folder outside it).
  const root = useWorkspace((s) => rootOf(s.roots, session.cwd)) ?? ''
  const query = slashQuery(session.draft)
  const open = query !== null
  const [library, setLibrary] = useState<PromptLibrary | null>(null)
  const [dismissed, setDismissed] = useState<string | null>(null)
  const { listRef, cursor, setCursor, hover } = useListCursor()
  const [asking, setAsking] = useState<Prompt | null>(null)

  // Read fresh from disk each time the picker opens, so edited files show up.
  useEffect(() => {
    if (!open) return setLibrary(null)
    let live = true
    void loadPrompts(root).then((lib) => live && setLibrary(lib))
    return () => {
      live = false
    }
  }, [open, root])
  const matches = useMemo(
    () => (library && query !== null ? matchPrompts(library.prompts, query, session.agent) : []),
    [library, query, session.agent],
  )
  useEffect(() => setCursor(0), [query, setCursor])

  const shown = open && library !== null && dismissed !== session.draft

  const fill = async (p: Prompt, args: Record<string, string>) => {
    setAsking(null)
    const text = await expandPrompt(root, p, args)
    if (text !== null) setDraft(session.id, text)
    input.current?.focus()
  }
  const choose = (p: Prompt | undefined) => {
    if (!p) return
    if (p.args.length > 0) setAsking(p)
    else void fill(p, {})
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    if (!shown || matches.length === 0 || e.nativeEvent.isComposing) return false
    if (e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n')) setCursor((c) => Math.min(c + 1, matches.length - 1))
    else if (e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p')) setCursor((c) => Math.max(c - 1, 0))
    else if ((e.key === 'Enter' && !e.shiftKey) || e.key === 'Tab') choose(matches[cursor])
    else if (e.key === 'Escape') setDismissed(session.draft)
    else return false
    e.preventDefault()
    return true
  }

  let element: ReactNode = null
  if (asking) {
    element = (
      <div className="prompt-picker">
        <PromptArgsForm
          prompt={asking}
          onSubmit={(args) => void fill(asking, args)}
          onCancel={() => {
            setAsking(null)
            input.current?.focus()
          }}
        />
      </div>
    )
  } else if (shown) {
    const untrusted = matches.some((p) => p.untrusted)
    const skipped = library?.problems ?? []
    element = (
      <div className="prompt-picker">
        {matches.length > 0 ? (
          <PromptList listRef={listRef} prompts={matches} cursor={cursor} onHover={hover} onPick={choose} empty="" />
        ) : (
          <div className="prompt-picker-note muted">
            {query
              ? `No Flint prompt named /${query}: Enter sends it to ${session.agent} as typed.`
              : 'No prompts yet: run "New Prompt…" from the command palette (⇧⌘P).'}
          </div>
        )}
        {matches.length > 0 && (
          <div className="prompt-picker-note muted">
            {untrusted && <span className="untrusted">Untrusted repository: read its prompts before sending. </span>}
            ↵ fills the draft · Esc sends “{session.draft}” as typed
            {skipped.length > 0 && <span title={skipped.join('\n')}> · {skipped.length} prompt file(s) skipped</span>}
          </div>
        )}
      </div>
    )
  }
  return { onKeyDown, element }
}
