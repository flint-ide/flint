import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { createPortal } from 'react-dom'
import { EditorState, type Extension } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { Diff, Workspace, errorMessage, type Hunk } from '../../api/backend'
import { Git, headVersionOf, useGit } from '../../store/git'
import { useEditor, type DiffTab } from '../../store/editor'
import { onFileChanged } from '../../store/fsEvents'
import { languageFor } from '../../lib/language'
import { baseExtensions } from '../../lib/editorSetup'
import { diffSide, expandFold, foldsFor, type Fold } from '../../lib/diffDecorations'
import { foldsClearOf, placeThreads, type Anchor } from '../../lib/prReview'
import { ThreadLayout, setThreadAnchors, threadDecorations } from '../../lib/prThreadWidgets'
import { findingThreads, type PlacedFinding } from '../../lib/agentReview'
import { cancelReview, reviewChanges, useAgentReview } from '../../store/agentReview'
import { FindingCard } from '../review/FindingCard'

const COLLAPSE = { margin: 3, minSize: 6 }
const NO_FINDINGS: PlacedFinding[] = []

interface Sides {
  base: string
  current: string
  hunks: Hunk[]
  note: string | null
}

/**
 * Side-by-side diff of HEAD (or baseRev) vs the working tree, refreshed live as agents
 * edit, or between two revisions when the tab has rev. A working-tree diff shows the
 * findings of Review Changes with Agent under their lines.
 */
export function DiffView({ tab }: { tab: DiffTab }) {
  const left = useRef<HTMLDivElement>(null)
  const right = useRef<HTMLDivElement>(null)
  const [sides, setSides] = useState<Sides | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [language, setLanguage] = useState<Extension>([])
  const [version, setVersion] = useState(0)
  // Fixed revisions never change: only a working-tree side follows saves, and only a
  // HEAD base follows commits (a commit diff must not refetch on every git event).
  const working = !tab.rev
  const headBase = !tab.baseRev
  const headVersion = useGit((s) => (headBase ? headVersionOf(s, tab.repoRoot) : 0))
  const openFile = useEditor((s) => s.openFile)
  const rel = tab.path.startsWith(tab.repoRoot) ? tab.path.slice(tab.repoRoot.length + 1) : tab.path

  // Review findings: only where the Commit panel's view applies (HEAD vs working tree).
  const reviewable = working && headBase
  const reviewRoot = useAgentReview((s) => s.root)
  const allFindings = useAgentReview((s) => s.findings)
  const agent = useAgentReview((s) => s.agent)
  const reviewing = useAgentReview((s) => !!s.running)
  const findings = useMemo(
    () => (reviewable && reviewRoot === tab.repoRoot ? allFindings.filter((f) => f.path === rel) : NO_FINDINGS),
    [reviewable, reviewRoot, tab.repoRoot, allFindings, rel],
  )
  const placement = useMemo(() => {
    if (!sides || sides.note) return { anchors: [] as Anchor[], detached: [] as string[] }
    const lines = { a: sides.base.split('\n').length, b: sides.current.split('\n').length }
    return placeThreads(findingThreads(findings), sides.hunks, lines)
  }, [findings, sides])
  const byId = useMemo(() => new Map(findings.map((f) => [f.id, f])), [findings])

  const layout = useMemo(() => new ThreadLayout(), [])
  const hosts = useSyncExternalStore(layout.subscribe, layout.getHosts)
  // Read when the editors are built; later changes go in through an effect.
  const anchorsNow = useRef(placement.anchors)
  anchorsNow.current = placement.anchors
  const folds = useRef<Fold[]>([])

  useEffect(() => (working ? onFileChanged(tab.path, () => setVersion((v) => v + 1)) : undefined), [tab.path, working])
  useEffect(() => void languageFor(tab.path).then(setLanguage), [tab.path])

  useEffect(() => {
    let cancelled = false
    const right = tab.rev ? Git.FileAt(tab.repoRoot, tab.rev, tab.path) : readWorking(tab.path)
    Promise.all([Git.FileAt(tab.repoRoot, tab.baseRev || 'HEAD', tab.basePath), right])
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
  }, [tab.repoRoot, tab.path, tab.basePath, tab.baseRev, tab.rev, version, headVersion])

  useEffect(() => {
    if (!left.current || !right.current || !sides || sides.note) return
    const { base, current, hunks } = sides
    folds.current = foldsClearOf(foldsFor(hunks, base.split('\n').length, COLLAPSE), anchorsNow.current)
    const views: EditorView[] = []
    // Unfolding must happen on both sides at once or they drift apart.
    const expand = (index: number) => views.forEach((v) => v.dispatch({ effects: expandFold.of(index) }))
    const readOnly = [EditorState.readOnly.of(true), EditorView.editable.of(false)]
    views.push(
      new EditorView({
        parent: left.current,
        doc: base,
        extensions: [baseExtensions(), language, readOnly, diffSide('a', hunks, folds.current, expand), threadDecorations('a', layout, anchorsNow.current)],
      }),
      new EditorView({
        parent: right.current,
        doc: current,
        extensions: [baseExtensions(), language, readOnly, diffSide('b', hunks, folds.current, expand), threadDecorations('b', layout, anchorsNow.current)],
      }),
    )
    layout.views = views
    return () => {
      layout.views = []
      views.forEach((v) => v.destroy())
    }
  }, [sides, language, layout])

  // Findings placed anew or dismissed: same editors, new widgets.
  useEffect(() => {
    const views = layout.views
    if (views.length === 0) return
    const anchors = placement.anchors
    const hidden = folds.current.filter((f) => foldsClearOf([f], anchors).length === 0).map((f) => folds.current.indexOf(f))
    views.forEach((v) => v.dispatch({ effects: [setThreadAnchors.of(anchors), ...hidden.map((i) => expandFold.of(i))] }))
  }, [placement, layout])

  useRevealFinding(tab.path, layout, sides)

  const showEditors = !!sides && !sides.note
  const detached = placement.detached.map((id) => byId.get(id)).filter((f): f is PlacedFinding => !!f)
  const anchorsByKey = new Map(placement.anchors.map((a) => [a.key, a]))

  return (
    <div className="diff-view">
      <div className="diff-header">
        <span className="muted">{tab.baseLabel ?? tab.baseRev ?? 'HEAD'}</span>
        <span className="truncate" title={tab.path}>{rel}</span>
        <span className="muted">{tab.label ?? (tab.rev ? tab.rev.slice(0, 7) : 'Working tree')}</span>
        <span className="spacer" />
        {reviewable && (reviewing
          ? <button className="link" onClick={cancelReview} title="Stop the agent">Reviewing… Cancel</button>
          : <button className="link" onClick={() => void reviewChanges(tab.path)} title="Let the agent review this file and the other changes in the Commit panel's scope">✦ Review</button>)}
        <button className="link" onClick={() => openFile(tab.path)}>Open file</button>
      </div>
      {error && <div className="placeholder error selectable">{error}</div>}
      {sides?.note && <div className="placeholder muted">{sides.note}</div>}
      {!sides && !error && <div className="placeholder">Loading…</div>}
      {detached.length > 0 && (
        <div className="pr-detached">
          {detached.map((f) => (
            <FindingCard key={f.id} finding={f} agent={agent} total={allFindings.length} label={f.stale ? `was line ${f.line}` : 'File'} />
          ))}
        </div>
      )}
      {/* Both editors grow to full height inside one scroller, so they scroll together. */}
      <div className="diff-host" hidden={!showEditors}>
        <div className="diff-sides">
          <div className="diff-side" ref={left} />
          <div className="diff-side" ref={right} />
        </div>
        {[...hosts].map(([key, el]) => {
          const anchor = anchorsByKey.get(key)
          if (!anchor) return null
          return createPortal(
            <div className="pr-anchor">
              {anchor.threadIds.map((id) => {
                const f = byId.get(id)
                return f ? <FindingCard key={id} finding={f} agent={agent} total={allFindings.length} /> : null
              })}
            </div>,
            el,
            key,
          )
        })}
      </div>
    </div>
  )
}

/** Scrolls the working-tree side to a finding the Commit panel asked for, once. */
function useRevealFinding(path: string, layout: ThreadLayout, sides: Sides | null) {
  const reveal = useAgentReview((s) => s.reveal)
  const done = useRef(0)
  useEffect(() => {
    const view = layout.views[1]
    if (!reveal || reveal.path !== path || reveal.token === done.current || !view) return
    done.current = reveal.token
    const line = view.state.doc.line(Math.min(Math.max(reveal.line, 1), view.state.doc.lines))
    view.dispatch({ effects: EditorView.scrollIntoView(line.from, { y: 'center' }) })
  }, [reveal, path, layout, sides])
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
