import { useEffect, useRef, useState } from 'react'
import { EditorState, type Extension } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { Diff, Workspace, errorMessage, type Hunk } from '../../api/backend'
import { Git, useGit } from '../../store/git'
import { useEditor, type DiffTab } from '../../store/editor'
import { onFileChanged } from '../../store/fsEvents'
import { languageFor } from '../../lib/language'
import { baseExtensions } from '../../lib/editorSetup'
import { diffSide, expandFold, foldsFor } from '../../lib/diffDecorations'

const COLLAPSE = { margin: 3, minSize: 6 }

interface Sides {
  base: string
  current: string
  hunks: Hunk[]
  note: string | null
}

/** Side-by-side diff of HEAD vs the working tree, refreshed live as agents edit. */
export function DiffView({ tab }: { tab: DiffTab }) {
  const left = useRef<HTMLDivElement>(null)
  const right = useRef<HTMLDivElement>(null)
  const [sides, setSides] = useState<Sides | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [language, setLanguage] = useState<Extension>([])
  const [version, setVersion] = useState(0)
  const headVersion = useGit((s) => s.headVersion)
  const openFile = useEditor((s) => s.openFile)

  useEffect(() => onFileChanged(tab.path, () => setVersion((v) => v + 1)), [tab.path])
  useEffect(() => void languageFor(tab.path).then(setLanguage), [tab.path])

  useEffect(() => {
    let cancelled = false
    Promise.all([Git.FileAt(tab.repoRoot, 'HEAD', tab.basePath), readWorking(tab.path)])
      .then(async ([baseFile, currentFile]) => {
        const note = baseFile.binary || currentFile.binary ? 'Binary file — contents not shown.' : null
        // One line-break convention, so Go and CodeMirror count the same lines.
        const base = normalize(baseFile.content)
        const current = normalize(currentFile.content)
        const hunks = note ? [] : await Diff.Compare(base, current)
        if (cancelled) return
        setSides({ base, current, hunks: hunks ?? [], note })
        setError(null)
      })
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [tab.repoRoot, tab.path, tab.basePath, version, headVersion])

  useEffect(() => {
    if (!left.current || !right.current || !sides || sides.note) return
    const { base, current, hunks } = sides
    const folds = foldsFor(hunks, base.split('\n').length, COLLAPSE)
    const views: EditorView[] = []
    // Unfolding must happen on both sides at once or they drift apart.
    const expand = (index: number) => views.forEach((v) => v.dispatch({ effects: expandFold.of(index) }))
    const readOnly = [EditorState.readOnly.of(true), EditorView.editable.of(false)]
    views.push(
      new EditorView({
        parent: left.current,
        doc: base,
        extensions: [baseExtensions(), language, readOnly, diffSide('a', hunks, folds, expand)],
      }),
      new EditorView({
        parent: right.current,
        doc: current,
        extensions: [baseExtensions(), language, readOnly, diffSide('b', hunks, folds, expand)],
      }),
    )
    return () => views.forEach((v) => v.destroy())
  }, [sides, language])

  const rel = tab.path.startsWith(tab.repoRoot) ? tab.path.slice(tab.repoRoot.length + 1) : tab.path
  const showEditors = !!sides && !sides.note

  return (
    <div className="diff-view">
      <div className="diff-header">
        <span className="muted">HEAD</span>
        <span className="truncate" title={tab.path}>{rel}</span>
        <span className="muted">Working tree</span>
        <span className="spacer" />
        <button className="link" onClick={() => openFile(tab.path)}>Open file</button>
      </div>
      {error && <div className="placeholder error selectable">{error}</div>}
      {sides?.note && <div className="placeholder muted">{sides.note}</div>}
      {!sides && !error && <div className="placeholder">Loading…</div>}
      {/* Both editors grow to full height inside one scroller, so they scroll together. */}
      <div className="diff-host" hidden={!showEditors}>
        <div className="diff-sides">
          <div className="diff-side" ref={left} />
          <div className="diff-side" ref={right} />
        </div>
      </div>
    </div>
  )
}

function normalize(text: string): string {
  return text.replace(/\r\n?/g, '\n')
}

async function readWorking(path: string): Promise<{ content: string; binary: boolean }> {
  try {
    const f = await Workspace.ReadFile(path)
    return { content: f.content, binary: f.binary || f.tooLarge }
  } catch {
    return { content: '', binary: false } // deleted in the working tree
  }
}
