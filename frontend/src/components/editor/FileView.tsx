import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { EditorState, type Extension } from '@codemirror/state'
import type { EditorView, KeyBinding } from '@codemirror/view'
import { Workspace, errorMessage, type FileContent } from '../../api/backend'
import { languageFor } from '../../lib/language'
import { registerSaver, registerView, useEditor, type FileTab } from '../../store/editor'
import { confirmAction, showToast } from '../../store/dialog'
import { onFileChanged } from '../../store/fsEvents'
import { minimalChange } from '../../lib/textDiff'
import { gitGutter, setGitBaseline } from '../../lib/gitGutter'
import { indentationFor } from '../../lib/indent'
import { Git, useGit } from '../../store/git'
import { lspExtension, lspSupported, type LspHooks } from '../../lib/lsp'
import { useLsp } from '../../store/lsp'
import { useWorkspace } from '../../store/workspace'
import * as LSP from '../../../wailsjs/go/main/LSPAPI'
import { CodeEditor } from './CodeEditor'

interface Props {
  tab: FileTab
  active: boolean
}

export function FileView({ tab, active }: Props) {
  const [file, setFile] = useState<FileContent | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [language, setLanguage] = useState<Extension>([])
  const view = useRef<EditorView | null>(null)
  const saved = useRef('') // disk content, with its original line endings
  const saving = useRef<Promise<void>>(Promise.resolve())
  const reloadPending = useRef(false)
  const unregisterView = useRef<(() => void) | null>(null)
  useEffect(() => () => unregisterView.current?.(), [])
  const setDirty = useEditor((s) => s.setDirty)

  useEffect(() => {
    let cancelled = false
    Promise.all([Workspace.ReadFile(tab.path), languageFor(tab.path)])
      .then(([content, lang]) => {
        if (cancelled) return
        saved.current = content.content
        setFile(content)
        setLanguage(lang)
      })
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [tab.path])

  // CRLF files keep CRLF: CodeMirror otherwise joins lines with \n on save.
  const crlf = !!file?.content.includes('\r\n')
  // Language server features; the server boots in the background on first use.
  const root = useWorkspace((s) => s.root)
  const lsp = useMemo(() => {
    if (!root || !lspSupported(tab.path)) return []
    const hooks: LspHooks = {
      goTo: (loc) =>
        useEditor.getState().openFile(loc.path, {
          line: loc.range.start.line + 1,
          from: loc.range.start.character,
          to: loc.range.end.line === loc.range.start.line ? loc.range.end.character : loc.range.start.character,
        }),
      showUsages: (locs, symbol) => useLsp.getState().showUsages(symbol, locs),
      notify: (message) => showToast(message),
    }
    return lspExtension(root, tab.path, hooks)
  }, [root, tab.path])

  // Enter / Tab follow the file's own indentation (tabs for Go, detected otherwise).
  const indentation = useMemo(() => (file ? indentationFor(file.content, tab.path) : []), [file, tab.path])

  const extensions = useMemo(
    () => [language, indentation, gitGutter(), lsp, crlf ? EditorState.lineSeparator.of('\r\n') : []],
    [crlf, language, indentation, lsp],
  )

  // Gutter baseline: the file at HEAD of the work tree that contains it (which may be
  // an agent's worktree nested in the project), refetched when HEAD moves.
  const isRepo = useGit((s) => s.isRepo)
  const headVersion = useGit((s) => s.headVersion)
  const [editorReady, setEditorReady] = useState(false)
  useEffect(() => {
    const v = view.current
    if (!v || !editorReady) return
    let cancelled = false
    Git.HeadFile(tab.path)
      .then((rev) => {
        if (cancelled || view.current !== v) return
        // Files not in HEAD (new/untracked) get no markers, like IntelliJ.
        v.dispatch({ effects: setGitBaseline.of(rev.exists && !rev.binary ? rev.content : null) })
      })
      .catch(() => undefined) // gutter is best-effort
    return () => {
      cancelled = true
    }
  }, [isRepo, headVersion, tab.path, editorReady])

  // Saves are chained: Wails runs each call on its own goroutine, so two in-flight
  // writes could otherwise land out of order.
  const save = useCallback(() => {
    saving.current = saving.current.then(async () => {
      const v = view.current
      if (!v) return
      const text = v.state.sliceDoc() // joined with the file's own line separator
      try {
        // Never silently overwrite what an agent (or anything else) wrote meanwhile.
        if (!(await confirmOverwriteIfChanged())) return
        await Workspace.WriteFile(tab.path, text)
        if (lspSupported(tab.path)) void LSP.Saved(tab.path)
        saved.current = text
        if (v.state.sliceDoc() === text) setDirty(tab.id, false)
      } catch (err) {
        showToast(`Save failed: ${errorMessage(err)}`, 'error')
      }
    })
    return saving.current
  }, [tab.path, tab.id, setDirty])

  useEffect(() => registerSaver(tab.id, save), [tab.id, save])

  /** True when it's fine to write: the disk still matches what we loaded, or the user said so. */
  async function confirmOverwriteIfChanged(): Promise<boolean> {
    let disk: FileContent
    try {
      disk = await Workspace.ReadFile(tab.path)
    } catch {
      return true // deleted meanwhile: saving recreates it
    }
    if (disk.binary || disk.tooLarge || disk.content === saved.current) return true
    const ok = await confirmAction(
      `${tab.title} was changed on disk (probably by the agent) after you started editing. Overwrite those changes with your version?`,
      'Overwrite',
    )
    if (!ok) showToast('Not saved — your edits stay in the editor. Compare with the disk version in the Commit panel (⌘K).')
    return ok
  }

  // Agents edit files from the terminal: reload clean buffers in place, warn on conflicts.
  const reloadFromDisk = useCallback(async () => {
    const v = view.current
    if (!v) {
      reloadPending.current = true // editor still loading; re-check once it's ready
      return
    }
    let fresh: FileContent
    try {
      fresh = await Workspace.ReadFile(tab.path)
    } catch {
      return // deleted or unreadable; keep the buffer
    }
    if (fresh.binary || fresh.tooLarge || fresh.content === saved.current) return
    if (v.state.sliceDoc() !== saved.current) {
      showToast(`${tab.title} changed on disk — you have unsaved edits`, 'error')
      return
    }
    saved.current = fresh.content
    // Diff in \n space: document offsets count a line break as one character.
    const change = minimalChange(v.state.doc.toString(), fresh.content.replace(/\r\n/g, '\n'))
    if (change) {
      // ...but inserted text must use the document's separator to split into lines.
      const insert = change.insert.replace(/\n/g, v.state.lineBreak)
      v.dispatch({ changes: { ...change, insert }, userEvent: 'external' })
    }
  }, [tab.path, tab.title])

  useEffect(() => onFileChanged(tab.path, () => void reloadFromDisk()), [tab.path, reloadFromDisk])

  const onReady = useCallback(
    (v: EditorView) => {
      view.current = v
      unregisterView.current?.()
      unregisterView.current = registerView(tab.id, v)
      setEditorReady(true)
      if (reloadPending.current) {
        reloadPending.current = false
        void reloadFromDisk()
      }
    },
    [reloadFromDisk],
  )

  const keys = useMemo<KeyBinding[]>(
    () => [{ key: 'Mod-s', preventDefault: true, run: () => (void save(), true) }],
    [save],
  )

  const onDocChange = useCallback(() => {
    const state = view.current?.state
    if (!state) return
    // Cheap length check first so typing in large files stays fast.
    const length = state.doc.length + (state.doc.lines - 1) * (state.lineBreak.length - 1)
    const clean = length === saved.current.length && state.sliceDoc() === saved.current
    setDirty(tab.id, !clean)
  }, [tab.id, setDirty])

  if (error) return <div className="placeholder error">{error}</div>
  if (!file) return <div className="placeholder">Loading…</div>
  if (file.binary) return <div className="placeholder">Binary or non-UTF-8 file ({formatSize(file.size)}) — not shown.</div>
  if (file.tooLarge) return <div className="placeholder">File too large to edit ({formatSize(file.size)}).</div>

  return (
    <CodeEditor
      initialDoc={file.content}
      language={extensions}
      keys={keys}
      reveal={tab.reveal}
      active={active}
      onDocChange={onDocChange}
      onReady={onReady}
    />
  )
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}
