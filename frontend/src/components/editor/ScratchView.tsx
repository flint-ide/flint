import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Extension } from '@codemirror/state'
import type { EditorView, KeyBinding } from '@codemirror/view'
import { Scratch, errorMessage, type Driver } from '../../api/backend'
import { languageFor } from '../../lib/language'
import { sqlLanguage } from '../../lib/sqlDialects'
import { registerSaver, registerView, useEditor, type ScratchTab } from '../../store/editor'
import { pendingContent, useScratch } from '../../store/scratch'
import { useDatabase } from '../../store/database'
import { showToast } from '../../store/dialog'
import { CodeEditor } from './CodeEditor'
import { QueryPanel, useQueryRunner } from './QueryPanel'

const AUTOSAVE_MS = 600

interface Props {
  tab: ScratchTab
  active: boolean
}

/** A persistent scratchpad. SQL scratchpads get a connection picker and a results panel. */
export function ScratchView({ tab, active }: Props) {
  const [initial, setInitial] = useState<string | null>(null)
  const [plainLanguage, setPlainLanguage] = useState<Extension>([])
  const view = useRef<EditorView | null>(null)
  // Registered so palette commands (Run Query, Find…) can reach this editor.
  const unregisterView = useRef<(() => void) | null>(null)
  useEffect(() => () => unregisterView.current?.(), [])
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const dirty = useRef(false)
  const setDirty = useEditor((s) => s.setDirty)
  const isSQL = tab.scratchKind === 'sql'

  const scratch = useScratch((s) => s.items.find((i) => i.id === tab.scratchId))
  const connId = scratch?.connId ?? ''
  const conn = useDatabase((s) => s.connections.find((c) => c.id === connId))
  const schema = useDatabase((s) => s.schemas[connId])
  const loadSchema = useDatabase((s) => s.loadSchema)
  const runner = useQueryRunner(connId)
  const runAtCursor = runner.runAtCursor

  useEffect(() => {
    let cancelled = false
    Scratch.Content(tab.scratchId)
      .then((text) => {
        if (cancelled) return
        const pending = pendingContent.get(tab.scratchId)
        pendingContent.delete(tab.scratchId)
        setInitial(pending ?? text)
        if (pending) void Scratch.Save(tab.scratchId, pending)
      })
      .catch((err) => !cancelled && showToast(errorMessage(err), 'error'))
    if (!isSQL) void languageFor(tab.scratchKind === 'markdown' ? 'x.md' : 'x.txt').then(setPlainLanguage)
    return () => {
      cancelled = true
    }
  }, [tab.scratchId, tab.scratchKind, isSQL])

  useEffect(() => {
    if (isSQL && connId) void loadSchema(connId)
  }, [isSQL, connId, loadSchema])

  const language = useMemo(
    () => (isSQL ? sqlLanguage(conn?.driver as Driver | undefined, conn?.database ?? '', schema?.tables ?? []) : plainLanguage),
    [isSQL, conn, schema?.tables, plainLanguage],
  )

  // Chained so overlapping autosaves can't land out of order (Wails calls run concurrently).
  const saving = useRef<Promise<void>>(Promise.resolve())
  const save = useCallback(() => {
    clearTimeout(timer.current)
    saving.current = saving.current.then(async () => {
      const v = view.current
      if (!v) return
      const text = v.state.doc.toString()
      try {
        dirty.current = false
        await Scratch.Save(tab.scratchId, text)
        if (v.state.doc.toString() === text) setDirty(tab.id, false)
      } catch (err) {
        dirty.current = true
        showToast(`Save failed: ${errorMessage(err)}`, 'error')
      }
    })
    return saving.current
  }, [tab.scratchId, tab.id, setDirty])

  useEffect(() => registerSaver(tab.id, save), [tab.id, save])
  // Flush pending edits when the tab closes.
  useEffect(() => () => void (dirty.current && save()), [save])

  const onDocChange = useCallback(() => {
    dirty.current = true
    setDirty(tab.id, true)
    clearTimeout(timer.current)
    timer.current = setTimeout(() => void save(), AUTOSAVE_MS)
  }, [tab.id, setDirty, save])

  const keys = useMemo<KeyBinding[]>(() => {
    const bindings: KeyBinding[] = [{ key: 'Mod-s', preventDefault: true, run: () => (void save(), true) }]
    if (isSQL) {
      bindings.push({
        key: 'Mod-Enter',
        preventDefault: true,
        run: (v) => (runAtCursor(v), true),
      })
    }
    return bindings
  }, [save, isSQL, runAtCursor])

  if (initial === null) return <div className="placeholder">Loading…</div>

  const editor = (
    <CodeEditor
      initialDoc={initial}
      language={language}
      keys={keys}
      active={active}
      onDocChange={onDocChange}
      onReady={(v) => {
        view.current = v
        unregisterView.current?.()
        unregisterView.current = registerView(tab.id, v)
      }}
    />
  )

  if (!isSQL) return editor
  return <QueryPanel scratchId={tab.scratchId} connId={connId} runner={runner} editor={editor} view={view} />
}
