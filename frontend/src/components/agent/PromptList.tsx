import { useEffect, useRef, useState } from 'react'
import { sourceLabel } from '../../lib/prompts'
import type { Prompt } from '../../store/prompts'

/** Rows of prompts: /name, description and where it comes from. */
export function PromptList({ prompts, cursor, onHover, onPick, empty }: {
  prompts: Prompt[]
  cursor: number
  onHover: (i: number) => void
  onPick: (p: Prompt) => void
  empty: string
}) {
  const list = useRef<HTMLDivElement>(null)
  useEffect(() => {
    list.current?.querySelector('.selected')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])
  return (
    <div className="quick-list prompt-list" ref={list} role="listbox">
      {prompts.map((p, i) => (
        <div
          key={p.name}
          role="option"
          aria-selected={i === cursor}
          className={`quick-item${i === cursor ? ' selected' : ''}`}
          onMouseMove={() => onHover(i)}
          // mousedown: keep the focus in the chat input
          onMouseDown={(e) => {
            e.preventDefault()
            onPick(p)
          }}
          title={p.path || undefined}
        >
          <span className="file-name">/{p.name}</span>
          <span className="muted path-hint">{p.description}</span>
          {p.agent && <span className="muted">{p.agent} only</span>}
          <span className={`badge${p.untrusted ? ' untrusted' : ''}`}>{sourceLabel(p)}</span>
        </div>
      ))}
      {prompts.length === 0 && <div className="placeholder muted">{empty}</div>}
    </div>
  )
}

/** Asks for a prompt's args; Enter in any field fills the draft, Esc goes back. */
export function PromptArgsForm({ prompt, onSubmit, onCancel }: {
  prompt: Prompt
  onSubmit: (args: Record<string, string>) => void
  onCancel: () => void
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  const submit = () => onSubmit(Object.fromEntries(prompt.args.map((a) => [a, values[a] ?? ''])))
  return (
    <form
      className="prompt-args"
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.preventDefault()
          e.stopPropagation()
          onCancel()
        }
      }}
    >
      <div className="prompt-args-title">
        /{prompt.name} <span className="muted">{prompt.description}</span>
      </div>
      {prompt.args.map((a, i) => (
        <label key={a}>
          <span>{a}</span>
          <input
            autoFocus={i === 0}
            value={values[a] ?? ''}
            spellCheck={false}
            onChange={(e) => setValues((v) => ({ ...v, [a]: e.target.value }))}
          />
        </label>
      ))}
      <div className="prompt-args-actions">
        <span className="muted">Fills the draft; nothing is sent until you press Send.</span>
        <span className="spacer" />
        <button type="button" onClick={onCancel}>Cancel</button>
        <button type="submit" className="primary">Fill Draft</button>
      </div>
    </form>
  )
}
