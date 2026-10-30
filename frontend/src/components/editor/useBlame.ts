import { useEffect, useState, type RefObject } from 'react'
import type { EditorView } from '@codemirror/view'
import { errorMessage } from '../../api/backend'
import { blameAt, blameShown, openBlameAt, setBlame } from '../../lib/blameGutter'
import { Git } from '../../store/git'
import { onFileChanged } from '../../store/fsEvents'
import { showToast } from '../../store/dialog'
import { openContextMenu } from '../../store/contextMenu'
import { openBlameCommit, setBlameShown, showFileHistory, toggleBlame, useGitHistory } from '../../store/gitHistory'

/**
 * Feeds the blame column of a file editor: blames the buffer when annotations are
 * toggled on, again when HEAD moves (commit, checkout) or the file changes on disk
 * (save, agent edit), and offers Annotate / Show History on gutter right-click.
 */
export function useBlame(view: RefObject<EditorView | null>, path: string, ready: boolean, headVersion: number): void {
  const shown = useGitHistory((s) => !!s.blame[path])
  const [changed, setChanged] = useState(0)

  useEffect(() => (shown ? onFileChanged(path, () => setChanged((n) => n + 1)) : undefined), [path, shown])

  useEffect(() => {
    const v = view.current
    if (!v || !ready) return
    if (!shown) {
      if (blameShown(v.state)) v.dispatch({ effects: setBlame.of(null) })
      return
    }
    let cancelled = false
    const doc = v.state.doc
    Git.Blame(path, v.state.sliceDoc())
      .then((b) => {
        if (cancelled || view.current !== v) return
        // Typed meanwhile: line numbers moved, so blame the new text instead.
        if (v.state.doc !== doc) return setChanged((n) => n + 1)
        const open = openBlameCommit.bind(null, b.root, path)
        v.dispatch({ effects: setBlame.of({ commits: b.commits ?? [], ranges: b.ranges ?? [], untracked: b.untracked, open }) })
      })
      .catch((err) => {
        if (cancelled) return
        showToast(`Annotate: ${errorMessage(err)}`, 'error')
        setBlameShown(path, false)
      })
    return () => {
      cancelled = true
    }
  }, [view, path, ready, shown, headVersion, changed])

  useEffect(() => {
    const v = view.current
    if (!v || !ready) return
    const onContextMenu = (e: MouseEvent) => {
      if (!(e.target instanceof Element) || !e.target.closest('.cm-gutters')) return
      e.preventDefault()
      const line = v.lineBlockAtHeight(e.clientY - v.documentTop)
      const commit = blameAt(v.state, line.from)
      openContextMenu(e.clientX, e.clientY, [
        {
          label: useGitHistory.getState().blame[path] ? 'Close Annotations' : 'Annotate with Git Blame',
          onSelect: () => toggleBlame(path),
        },
        { label: 'Show History', onSelect: () => showFileHistory(path) },
        ...(commit && !commit.uncommitted
          ? [
              { type: 'separator' as const },
              { label: `Show Diff for ${commit.hash.slice(0, 7)}`, hint: commit.summary, onSelect: () => void openBlameAt(v.state, line.from) },
            ]
          : []),
      ])
    }
    v.dom.addEventListener('contextmenu', onContextMenu)
    return () => v.dom.removeEventListener('contextmenu', onContextMenu)
  }, [view, path, ready])
}
