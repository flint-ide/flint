import { useEffect, useRef, useState } from 'react'
import { MergeView } from '@codemirror/merge'
import { EditorState, type Extension } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { Workspace, errorMessage } from '../../api/backend'
import { Git, useGit } from '../../store/git'
import { useEditor, type DiffTab } from '../../store/editor'
import { onFileChanged } from '../../store/fsEvents'
import { languageFor } from '../../lib/language'
import { baseExtensions } from '../../lib/editorSetup'

interface Sides {
  base: string
  current: string
  note: string | null
}

/** Side-by-side diff of HEAD vs the working tree, refreshed live as agents edit. */
export function DiffView({ tab }: { tab: DiffTab }) {
  const host = useRef<HTMLDivElement>(null)
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
      .then(([base, current]) => {
        if (cancelled) return
        const note = base.binary || current.binary ? 'Binary file — contents not shown.' : null
        setSides({ base: base.content, current: current.content, note })
        setError(null)
      })
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [tab.repoRoot, tab.path, tab.basePath, version, headVersion])

  useEffect(() => {
    if (!host.current || !sides || sides.note) return
    const readOnly = [EditorState.readOnly.of(true), EditorView.editable.of(false)]
    const view = new MergeView({
      parent: host.current,
      a: { doc: sides.base, extensions: [baseExtensions(), language, readOnly] },
      b: { doc: sides.current, extensions: [baseExtensions(), language, readOnly] },
      collapseUnchanged: { margin: 3, minSize: 6 },
      gutter: true,
    })
    return () => view.destroy()
  }, [sides, language])

  const rel = tab.path.startsWith(tab.repoRoot) ? tab.path.slice(tab.repoRoot.length + 1) : tab.path

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
      <div className="diff-host" ref={host} />
    </div>
  )
}

async function readWorking(path: string): Promise<{ content: string; binary: boolean }> {
  try {
    const f = await Workspace.ReadFile(path)
    return { content: f.content, binary: f.binary || f.tooLarge }
  } catch {
    return { content: '', binary: false } // deleted in the working tree
  }
}
