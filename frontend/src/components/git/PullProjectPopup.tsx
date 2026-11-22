import { useCallback, useEffect, useRef, useState } from 'react'
import { closePullPicker, useGit } from '../../store/git'
import { useWorkspace } from '../../store/workspace'
import { pullHint, pullTargets } from '../../lib/pullTargets'
import { useEventCallback } from '../../lib/useEventCallback'
import { Modal } from '../layout/Modal'

const optionId = (i: number) => `pull-option-${i}`
/** The row an event came from (its data-index). */
const indexOf = (e: React.SyntheticEvent<HTMLElement>) => Number(e.currentTarget.dataset.index)

/**
 * "Update Repository" with several repositories in the workspace: which one to pull
 * (like IntelliJ's root selector), or all of them. Arrows and Enter work too.
 */
export function PullProjectPopup() {
  const repos = useGit((s) => s.repos)
  const root = useGit((s) => s.root)
  const roots = useWorkspace((s) => s.roots)
  const targets = pullTargets(repos, roots, root)
  // Entries: every repository, then "all".
  const count = targets.length + 1
  const [index, setIndex] = useState(() => Math.max(0, targets.findIndex((t) => t.current)))
  // Where focus goes back to (an editor, a terminal) when the picker closes.
  const [previousFocus] = useState(() => document.activeElement)
  const list = useRef<HTMLDivElement | null>(null)
  // The dialog mounts its content a commit after this component, so the list is
  // focused and scrolled to the current repository when it appears (a ref callback).
  const attachList = useCallback((el: HTMLDivElement | null) => {
    list.current = el
    el?.focus()
  }, [])
  useEffect(() => {
    document.getElementById(optionId(index))?.scrollIntoView({ block: 'nearest' })
  }, [index])
  // A folder was detached meanwhile: nothing is left to choose from.
  const empty = targets.length === 0
  useEffect(() => {
    if (empty) closePullPicker()
  }, [empty])

  // Handlers keep one identity across renders: rows get a reference, not a new closure each.
  const close = useEventCallback(() => {
    closePullPicker()
    if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus()
  })

  const pull = useEventCallback((i: number) => {
    const chosen = i < targets.length ? [targets[i].root] : targets.map((t) => t.root)
    close()
    void useGit.getState().pull(chosen)
  })
  const onHover = useEventCallback((e: React.MouseEvent<HTMLElement>) => setIndex(indexOf(e)))
  const onChoose = useEventCallback((e: React.MouseEvent<HTMLElement>) => pull(indexOf(e)))

  const onKeyDown = useEventCallback((e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') setIndex((i) => (i + 1) % count)
    else if (e.key === 'ArrowUp') setIndex((i) => (i + count - 1) % count)
    else if (e.key === 'Enter') pull(Math.min(index, count - 1))
    else if (e.key === 'Tab') {
      // Focus stays in the picker; Esc closes it.
    } else return
    e.preventDefault()
  })

  if (empty) return null
  return (
    <Modal title="Update Repository" onClose={close} className="pull-popup">
      <div className="popup-section" id="pull-popup-hint">Pull from the remote into</div>
      <div
        ref={attachList}
        className="quick-list"
        tabIndex={0}
        role="listbox"
        aria-labelledby="pull-popup-hint"
        aria-activedescendant={optionId(Math.min(index, count - 1))}
        onKeyDown={onKeyDown}
      >
        {targets.map((t, i) => (
          <div
            key={t.root}
            id={optionId(i)}
            data-index={i}
            role="option"
            aria-selected={i === index}
            className={`quick-item${i === index ? ' selected' : ''}${t.current ? ' current' : ''}`}
            title={t.root}
            onMouseEnter={onHover}
            onClick={onChoose}
          >
            <span className="file-name">{t.label}</span>
            <span className="muted path-hint">{pullHint(t)}</span>
          </div>
        ))}
        <div
          id={optionId(targets.length)}
          data-index={targets.length}
          role="option"
          aria-selected={index >= targets.length}
          className={`quick-item action${index >= targets.length ? ' selected' : ''}`}
          onMouseEnter={onHover}
          onClick={onChoose}
        >
          <span>All Repositories</span>
          <span className="muted path-hint">{targets.length} repositories</span>
        </div>
      </div>
    </Modal>
  )
}
