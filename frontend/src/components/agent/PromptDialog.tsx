import { useEffect, useMemo, useState } from 'react'
import { matchPrompts, validPromptName } from '../../lib/prompts'
import { activeRoot } from '../../store/activeRoot'
import {
  closePromptDialog, createPrompt, loadPrompts, runPromptInChat, usePrompts, type Prompt, type PromptLibrary,
} from '../../store/prompts'
import { Modal } from '../layout/Modal'
import { PromptArgsForm, PromptList } from './PromptList'

/** The Run Prompt… and New Prompt… dialogs (palette commands). */
export function PromptDialog() {
  const dialog = usePrompts((s) => s.dialog)
  const [root] = useState(activeRoot)
  if (dialog === 'run') return <RunPrompt root={root} />
  if (dialog === 'new') return <NewPrompt root={root} />
  return null
}

/** Picks a prompt and puts it, filled in, into the current or default agent chat's draft. */
function RunPrompt({ root }: { root: string }) {
  const [library, setLibrary] = useState<PromptLibrary | null>(null)
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const [asking, setAsking] = useState<Prompt | null>(null)
  useEffect(() => {
    void loadPrompts(root).then(setLibrary)
  }, [root])
  // Every agent's prompts: one limited to an agent opens a chat with it.
  const rows = useMemo(() => (library ? matchPrompts(library.prompts, query.replace(/^\//, ''), '') : []), [library, query])
  useEffect(() => setCursor(0), [query])

  const run = (p: Prompt, args: Record<string, string>) => {
    closePromptDialog()
    void runPromptInChat(p, args)
  }
  const choose = (p: Prompt | undefined) => {
    if (!p) return
    if (p.args.length > 0) setAsking(p)
    else run(p, {})
  }

  return (
    <Modal onClose={asking ? () => setAsking(null) : closePromptDialog} className="quick-open command-palette">
      {asking ? (
        <PromptArgsForm prompt={asking} onSubmit={(args) => run(asking, args)} onCancel={() => setAsking(null)} />
      ) : (
        <>
          <input
            autoFocus
            className="quick-input"
            placeholder="Run prompt in the agent chat…"
            value={query}
            spellCheck={false}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') setCursor((c) => Math.min(c + 1, rows.length - 1))
              else if (e.key === 'ArrowUp') setCursor((c) => Math.max(c - 1, 0))
              else if (e.key === 'Enter') choose(rows[cursor])
              else return
              e.preventDefault()
            }}
          />
          <PromptList
            prompts={rows}
            cursor={cursor}
            onHover={setCursor}
            onPick={choose}
            empty={library ? 'No matching prompts' : 'Loading…'}
          />
        </>
      )}
    </Modal>
  )
}

/** Names a new prompt and creates it from a template, in the project or the personal folder. */
function NewPrompt({ root }: { root: string }) {
  const [name, setName] = useState('')
  const [personal, setPersonal] = useState(!root)
  const valid = validPromptName(name)
  const create = async () => {
    if (valid && (await createPrompt(name, personal))) closePromptDialog()
  }
  return (
    <Modal title="New Prompt" onClose={closePromptDialog} className="new-prompt">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <label>
          Name (typed after /)
          <input autoFocus value={name} spellCheck={false} placeholder="review-api" onChange={(e) => setName(e.target.value.trim())} />
        </label>
        {name && !valid && <span className="form-status error">Letters, digits, - and _ only, starting with a letter or digit.</span>}
        <label className="check">
          <input type="radio" name="scope" checked={!personal} disabled={!root} onChange={() => setPersonal(false)} />
          Project: .flint/prompts, shared with the team through the repository
        </label>
        <label className="check">
          <input type="radio" name="scope" checked={personal} onChange={() => setPersonal(true)} />
          Personal: only you, in every project
        </label>
        <div className="form-actions">
          <button type="button" onClick={closePromptDialog}>Cancel</button>
          <button type="submit" className="primary" disabled={!valid}>Create and Open</button>
        </div>
      </form>
    </Modal>
  )
}
