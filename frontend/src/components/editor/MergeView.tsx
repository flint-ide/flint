import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Compartment, EditorState, Prec, type Extension, type StateField } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { Workspace, errorMessage } from '../../api/backend'
import { Git, useGit } from '../../store/git'
import type { git } from '../../wails/models'
import { registerView, useEditor, type MergeTab } from '../../store/editor'
import { onFileChanged } from '../../store/fsEvents'
import { confirmAction, showToast } from '../../store/dialog'
import { rootFor } from '../../store/activeRoot'
import { registerMergeController } from '../../store/mergeEditor'
import { languageFor } from '../../lib/language'
import { baseExtensions } from '../../lib/editorSetup'
import { minimalChange } from '../../lib/textDiff'
import { askAgent } from '../../lib/askAgent'
import { formatKeys } from '../../lib/commands'
import { relativePath } from '../../lib/agentPrompt'
import { mergeConflicts, type MergeState } from '../../lib/mergeDecorations'
import {
  applyRedaction,
  baseFor,
  conflictAt,
  conflictPrompt,
  conflictText,
  diff3Segments,
  nextConflict,
  redactionInput,
  replaceConflict,
  resolvedLines,
  type Resolution,
} from '../../lib/mergeConflicts'

type ConflictFile = git.ConflictFile

/** Where the cursor is among the conflicts: index of the one it is in (-1 when outside). */
interface Position {
  total: number
  index: number
}

/**
 * The merge editor: the conflicted file as git left it (non-conflicting changes
 * already merged), each conflict an inline block with its actions. The result is
 * written only on Save or Mark as Resolved.
 */
export function MergeView({ tab, active }: { tab: MergeTab; active: boolean }) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const field = useRef<StateField<MergeState> | null>(null)
  /** The file's content as last loaded or written, to tell edits apart. */
  const saved = useRef('')
  const [data, setData] = useState<ConflictFile | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [language, setLanguage] = useState<Extension>([])
  // Swapped in place when it loads: rebuilding the editor would drop the user's edits.
  const languageSlot = useRef(new Compartment())
  const languageNow = useRef<Extension>([])
  languageNow.current = language
  const [pos, setPos] = useState<Position>({ total: 0, index: -1 })
  const [busy, setBusy] = useState(false)
  const setDirty = useEditor((s) => s.setDirty)
  const rel = relativePath(tab.path, tab.repoRoot)

  useEffect(() => void languageFor(tab.path).then(setLanguage), [tab.path])

  useEffect(() => {
    let cancelled = false
    Git.Conflict(tab.path)
      .then((c) => !cancelled && (setData(c), setError(null)))
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [tab.path])

  const conflicts = () => (view.current && field.current ? view.current.state.field(field.current).conflicts : [])

  const updatePos = useCallback(() => {
    const v = view.current
    if (!v || !field.current) return
    const list = v.state.field(field.current).conflicts
    const line = v.state.doc.lineAt(v.state.selection.main.head).number
    setPos((p) => {
      const next = { total: list.length, index: conflictAt(list, line) }
      return p.total === next.total && p.index === next.index ? p : next
    })
  }, [])

  /**
   * Runs a write that Go refuses when the file changed on disk since the editor
   * loaded it (an agent, another editor); the user may then overwrite. Returns
   * whether it wrote. Other errors propagate.
   */
  const writeChecked = useCallback(
    async (write: (overwrite: boolean) => Promise<void>): Promise<boolean> => {
      try {
        await write(false)
        return true
      } catch (err) {
        if (!errorMessage(err).includes('changed on disk')) throw err
      }
      const overwrite = await confirmAction(
        `${rel} changed on disk since the merge editor loaded it (an agent or another editor?). Overwrite it with the merge editor's version? Cancel keeps the file on disk; reopen the merge editor to load it.`,
        'Overwrite',
      )
      if (!overwrite) return false
      await write(true)
      return true
    },
    [rel],
  )

  const save = useCallback(async () => {
    const v = view.current
    if (!v) return
    const text = v.state.sliceDoc()
    try {
      if (!(await writeChecked((overwrite) => Git.SaveMergeResult(tab.path, text, saved.current, overwrite)))) return
      saved.current = text
      setDirty(tab.id, false)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  }, [tab.path, tab.id, setDirty, writeChecked])

  const resolveAt = useCallback((index: number, how: Resolution) => {
    const v = view.current
    const c = conflicts()[index]
    if (!v || !c) return
    const doc = v.state.doc
    const change = replaceConflict(doc, c, resolvedLines(conflictText(doc, c), how), v.state.lineBreak)
    v.dispatch({ changes: change, selection: { anchor: change.from }, scrollIntoView: true, userEvent: 'input.merge' })
    v.focus()
  }, [])

  const askAgentAt = useCallback(
    async (index: number) => {
      const v = view.current
      const list = conflicts()
      const c = list[index]
      if (!v || !c || !data) return
      if (v.state.sliceDoc() !== saved.current && (await confirmAction('The agent edits the file on disk. Save your changes first?', 'Save'))) {
        await save()
      }
      const raw = conflictText(v.state.doc, c)
      const rawBase = baseFor(raw, diff3Segments(data.diff3))
      // Credentials are redacted in Go, and a secret file's contents stay out entirely.
      const redacted = await Git.RedactForAgent(tab.path, redactionInput(raw, rawBase))
      const { text, base, secretFile } = applyRedaction(raw, rawBase, redacted)
      const root = rootFor(tab.path)
      const prompt = conflictPrompt({
        rel: relativePath(tab.path, root),
        index,
        total: list.length,
        range: c,
        text,
        base,
        secretFile,
        operation: data.sides.operation,
        current: data.sides.current,
        incoming: data.sides.incoming,
      })
      await askAgent({ prompt, root })
    },
    [data, save, tab.path],
  )

  const go = useCallback((dir: 1 | -1) => {
    const v = view.current
    const list = conflicts()
    if (!v) return
    const i = nextConflict(list, v.state.doc.lineAt(v.state.selection.main.head).number, dir)
    if (i < 0) return showToast('No conflicts left in this file')
    const from = v.state.doc.line(list[i].open).from
    v.dispatch({ selection: { anchor: from }, effects: EditorView.scrollIntoView(from, { y: 'center' }) })
    v.focus()
  }, [])

  const markResolved = useCallback(async () => {
    const v = view.current
    if (!v || busy) return
    const text = v.state.sliceDoc()
    const left = conflicts().length
    if (left > 0) {
      const ok = await confirmAction(
        `${left} conflict${left === 1 ? '' : 's'} in ${rel} still ${left === 1 ? 'has' : 'have'} markers. Save and stage the file with the markers anyway?`,
        'Stage Anyway',
      )
      if (!ok) return
    }
    setBusy(true)
    try {
      const expected = saved.current
      if (!(await writeChecked((overwrite) => Git.MarkResolved(tab.path, text, expected, left > 0, overwrite)))) return
      saved.current = text
      setDirty(tab.id, false)
      showToast(`${rel} marked as resolved`)
      void useGit.getState().refresh()
      useEditor.getState().closeTab(tab.id)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    } finally {
      setBusy(false)
    }
  }, [busy, rel, tab.path, tab.id, setDirty, writeChecked])

  // The commands (palette, shortcuts) act on the cursor's conflict, else the next one.
  const resolveAtCursor = useCallback(
    (how: Resolution) => {
      const v = view.current
      const list = conflicts()
      if (!v) return
      const line = v.state.doc.lineAt(v.state.selection.main.head).number
      const i = conflictAt(list, line) >= 0 ? conflictAt(list, line) : nextConflict(list, line, 1)
      if (i < 0) return showToast('No conflicts left in this file')
      resolveAt(i, how)
    },
    [resolveAt],
  )
  const askAgentAtCursor = useCallback(() => {
    const v = view.current
    const list = conflicts()
    if (!v) return
    const line = v.state.doc.lineAt(v.state.selection.main.head).number
    const i = conflictAt(list, line) >= 0 ? conflictAt(list, line) : nextConflict(list, line, 1)
    if (i < 0) return showToast('No conflicts left in this file')
    void askAgentAt(i)
  }, [askAgentAt])

  useEffect(
    () => registerMergeController(tab.id, { go, resolve: resolveAtCursor, askAgent: askAgentAtCursor, save, markResolved }),
    [tab.id, go, resolveAtCursor, askAgentAtCursor, save, markResolved],
  )

  // Callbacks change with data; the editor reads them through a ref.
  const actions = useRef({ resolveAt, askAgentAt, save })
  actions.current = { resolveAt, askAgentAt, save }

  const labels = useMemo(
    () => (data ? { current: data.sides.current.label, incoming: data.sides.incoming.label } : null),
    [data],
  )
  const editable = !!data && data.working.exists && !data.working.binary

  useEffect(() => {
    if (!host.current || !data || !labels || !editable) return
    const content = data.working.content
    const merge = mergeConflicts(labels, {
      resolve: (i, how) => actions.current.resolveAt(i, how),
      askAgent: (i) => void actions.current.askAgentAt(i),
    })
    field.current = merge.field
    saved.current = content
    const v = new EditorView({
      parent: host.current,
      doc: content,
      extensions: [
        Prec.high(keymap.of([{ key: 'Mod-s', preventDefault: true, run: () => (void actions.current.save(), true) }])),
        baseExtensions(),
        languageSlot.current.of(languageNow.current),
        content.includes('\r\n') ? EditorState.lineSeparator.of('\r\n') : [],
        merge.extension,
        EditorView.updateListener.of((u) => {
          if (u.docChanged) setDirty(tab.id, u.state.sliceDoc() !== saved.current)
          if (u.docChanged || u.selectionSet) updatePos()
        }),
      ],
    })
    view.current = v
    const unregister = registerView(tab.id, v)
    updatePos()
    return () => {
      unregister()
      v.destroy()
      view.current = null
    }
  }, [data, labels, editable, tab.id, setDirty, updatePos])

  useEffect(() => view.current?.dispatch({ effects: languageSlot.current.reconfigure(language) }), [language])

  useEffect(() => {
    if (active) view.current?.focus()
  }, [active, data])

  // An agent (or the terminal) may edit the file: follow it while there are no
  // unsaved edits here; otherwise say so and keep the user's version.
  useEffect(
    () =>
      onFileChanged(tab.path, async () => {
        const v = view.current
        if (!v) return
        const fresh = await Workspace.ReadFile(tab.path).catch(() => null)
        if (!fresh || fresh.binary || fresh.tooLarge || fresh.content === saved.current) return
        if (v.state.sliceDoc() !== saved.current) {
          showToast(`${rel} changed on disk — you have unsaved edits in the merge editor`, 'error')
          return
        }
        saved.current = fresh.content
        const change = minimalChange(v.state.doc.toString(), fresh.content.replace(/\r\n/g, '\n'))
        if (change) v.dispatch({ changes: { ...change, insert: change.insert.replace(/\n/g, v.state.lineBreak) }, userEvent: 'external' })
      }),
    [tab.path, rel],
  )

  const compare = (rev: ':2' | ':3', side: git.ConflictSide) =>
    useEditor.getState().openRevisionDiff({
      repoRoot: tab.repoRoot,
      path: tab.path,
      basePath: tab.path,
      baseRev: ':1',
      rev,
      baseLabel: 'Base',
      label: `${rev === ':2' ? 'Current' : 'Incoming'} (${side.label})`,
    })

  if (error) {
    return (
      <div className="diff-view merge-view">
        <div className="placeholder error selectable">{error}</div>
      </div>
    )
  }
  if (!data) return <div className="placeholder">Loading…</div>

  const { sides } = data
  const canCompare = data.base.exists && !data.base.binary
  return (
    <div className="diff-view merge-view">
      <div className="diff-header merge-header">
        <span className="truncate" title={tab.path}>{rel}</span>
        <span className="merge-side current" title={sides.current.detail}>Current: {sides.current.label}</span>
        <span className="merge-side incoming" title={sides.incoming.detail}>Incoming: {sides.incoming.label}</span>
        <span className="spacer" />
        {editable && (
          <span className="merge-nav">
            <button className="icon" title="Previous conflict" disabled={pos.total === 0} onClick={() => go(-1)}>‹</button>
            <span className="muted">{conflictCount(pos)}</span>
            <button className="icon" title="Next conflict" disabled={pos.total === 0} onClick={() => go(1)}>›</button>
          </span>
        )}
        {canCompare && data.current.exists && (
          <button className="link" title="What the current side changed since the base" onClick={() => compare(':2', sides.current)}>Δ Current</button>
        )}
        {canCompare && data.incoming.exists && (
          <button className="link" title="What the incoming side changed since the base" onClick={() => compare(':3', sides.incoming)}>Δ Incoming</button>
        )}
        {editable && (
          <>
            <button title={`Save the result without staging it (${formatKeys('Mod+S')})`} onClick={() => void save()}>Save</button>
            <button className="primary" disabled={busy} title="Save the result and stage it (git add)" onClick={() => void markResolved()}>
              Mark as Resolved
            </button>
          </>
        )}
      </div>
      <div className="merge-legend muted">
        <span><b>Current</b> = {sides.current.detail}</span>
        <span><b>Incoming</b> = {sides.incoming.detail}</span>
      </div>
      {editable && (!data.current.exists || !data.incoming.exists) && (
        <div className="merge-legend">
          Deleted in {data.current.exists ? `incoming (${sides.incoming.label})` : `current (${sides.current.label})`}: Mark as Resolved keeps the
          file as shown here; to delete it instead, run git rm in a terminal.
        </div>
      )}
      {!editable && <div className="placeholder muted">{notEditable(data, sides)}</div>}
      <div className="merge-host" hidden={!editable} ref={host} />
    </div>
  )
}

function conflictCount({ total, index }: Position): string {
  if (total === 0) return 'No conflicts left'
  if (index >= 0) return `Conflict ${index + 1} of ${total}`
  return `${total} conflict${total === 1 ? '' : 's'}`
}

/** Why the file can't be merged here: a side deleted it, or it isn't text. */
function notEditable(c: ConflictFile, sides: git.ConflictSides): string {
  if (!c.working.exists) {
    const state = (r: git.Revision) => (r.exists ? 'changed' : 'deleted')
    return `This conflict is about the file itself, not its lines: ${state(c.current)} in current (${sides.current.label}), ${state(c.incoming)} in incoming (${sides.incoming.label}). Keep it with git add, or remove it with git rm, in a terminal or with the agent.`
  }
  return 'Binary or very large file: pick a side in a terminal (git checkout --ours or --theirs, then git add) or with the agent.'
}
