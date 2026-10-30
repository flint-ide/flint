import { useEffect, useRef } from 'react'
import { Compartment, EditorSelection, EditorState, type Extension } from '@codemirror/state'
import { EditorView, keymap, type KeyBinding } from '@codemirror/view'
import { baseExtensions } from '../../lib/editorSetup'
import { focusSoon } from '../../lib/focusSoon'
import type { Reveal } from '../../store/editor'

interface Props {
  /** Initial document; the editor owns the text afterwards. */
  initialDoc: string
  language: Extension
  keys?: KeyBinding[]
  reveal?: Reveal
  active: boolean
  onDocChange?: () => void
  onReady?: (view: EditorView) => void
}

/** Thin CodeMirror 6 wrapper. Language and keymap are swappable without recreating the view. */
export function CodeEditor({ initialDoc, language, keys = [], reveal, active, onDocChange, onReady }: Props) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const languageConf = useRef(new Compartment())
  const keysConf = useRef(new Compartment())
  const onChangeRef = useRef(onDocChange)
  onChangeRef.current = onDocChange

  useEffect(() => {
    if (!host.current) return
    const v = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: initialDoc,
        extensions: [
          keysConf.current.of(keymap.of(keys)),
          baseExtensions(),
          languageConf.current.of(language),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) onChangeRef.current?.()
          }),
        ],
      }),
    })
    view.current = v
    onReady?.(v)
    return () => {
      v.destroy()
      view.current = null
    }
    // The view is created once; later prop changes are applied through compartments.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    view.current?.dispatch({ effects: languageConf.current.reconfigure(language) })
  }, [language])

  useEffect(() => {
    view.current?.dispatch({ effects: keysConf.current.reconfigure(keymap.of(keys)) })
  }, [keys])

  useEffect(() => {
    if (active) return focusSoon(() => view.current?.focus())
  }, [active])

  useEffect(() => {
    const v = view.current
    if (!v || !reveal) return
    const lineNo = Math.min(Math.max(reveal.line, 1), v.state.doc.lines)
    const line = v.state.doc.line(lineNo)
    const from = Math.min(line.from + reveal.from, line.to)
    const to = Math.min(line.from + reveal.to, line.to)
    v.dispatch({
      selection: EditorSelection.range(from, to),
      effects: EditorView.scrollIntoView(from, { y: 'center' }),
    })
    if (!reveal.quiet) v.focus()
  }, [reveal])

  return <div className="code-editor" ref={host} />
}
