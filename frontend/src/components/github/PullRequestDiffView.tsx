import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { createPortal } from 'react-dom'
import { EditorState, type Extension } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { Diff, GitHub, errorMessage, type Hunk, type PullRequestDetails, type PullRequestFileDiff, type ReviewThread as Thread } from '../../api/backend'
import type { PullRequestDiffTab } from '../../store/editor'
import { detailsKey, loadDetails, openInBrowser, usePullRequests } from '../../store/pullRequests'
import { languageFor } from '../../lib/language'
import { baseExtensions } from '../../lib/editorSetup'
import { diffSide, expandFold, foldsFor, type Fold } from '../../lib/diffDecorations'
import { foldsClearOf, isCommentable, placeThreads, type Anchor, type Side } from '../../lib/prReview'
import { ThreadLayout, commentGutter, setThreadAnchors, threadDecorations } from '../../lib/prThreadWidgets'
import { CommentComposer, ReviewThread } from './ReviewThread'
import { ReviewBar } from './ReviewBar'
import { useWindowFocus } from './useWindowFocus'

const COLLAPSE = { margin: 3, minSize: 6 }

interface Sides {
  diff: PullRequestFileDiff
  base: string
  head: string
  hunks: Hunk[]
  lines: { a: number; b: number }
}

/**
 * One file of a pull request side by side (merge base vs head), like the diff tab, with
 * review threads under their lines, a reply box per thread and "+" to comment on a line.
 */
export function PullRequestDiffView({ tab, active }: { tab: PullRequestDiffTab; active: boolean }) {
  const key = detailsKey(tab.root, tab.number)
  const entry = usePullRequests((s) => s.details[key])
  const details = entry?.data ?? null
  const [sides, setSides] = useState<Sides | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [language, setLanguage] = useState<Extension>([])
  const [draft, setDraft] = useState<{ side: Side; line: number } | null>(null)

  useEffect(() => {
    if (!entry) void loadDetails(tab.root, tab.number)
  }, [entry, tab.root, tab.number])
  useWindowFocus(() => void loadDetails(tab.root, tab.number), active)
  useEffect(() => void languageFor(tab.path).then(setLanguage), [tab.path])

  // Both sides change only with the head commit (a new push), not with new comments.
  const headOid = details?.headOid
  useEffect(() => {
    if (!headOid) return
    let cancelled = false
    GitHub.FileDiff(tab.root, tab.number, tab.path)
      .then(async (diff) => {
        const base = normalize(diff.base)
        const head = normalize(diff.head)
        const shown = !diff.binary && !diff.tooLarge
        const hunks = shown ? await Diff.Compare(base, head) : []
        if (cancelled) return
        setSides({ diff, base, head, hunks: hunks ?? [], lines: { a: base.split('\n').length, b: head.split('\n').length } })
        setError(null)
      })
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [tab.root, tab.number, tab.path, headOid])

  const threads = useMemo(() => details?.threads.filter((t) => t.path === tab.path) ?? [], [details, tab.path])
  const placement = useMemo(
    () => (sides ? placeThreads(threads, sides.hunks, sides.lines, draft) : { anchors: [], detached: [] }),
    [threads, sides, draft],
  )

  const note = sides?.diff.binary ? 'Binary file — contents not shown.' : sides?.diff.tooLarge ? 'File too large to show here.' : null
  const editors = sides && !note && details ? sides : null
  const byId = useMemo(() => new Map(threads.map((t) => [t.id, t])), [threads])
  const detached = placement.detached.map((id) => byId.get(id)).filter((t): t is Thread => !!t)

  return (
    <div className="diff-view pr-diff">
      <div className="diff-header">
        <span className="muted" title="GitHub diffs against the merge base">{details ? `${details.baseRef} (merge base)` : 'base'}</span>
        <span className="truncate" title={tab.path}>
          {sides?.diff.previousPath ? `${sides.diff.previousPath} → ${tab.path}` : tab.path}
        </span>
        <span className="muted">{details ? `${details.headRef} @ ${details.headOid.slice(0, 7)}` : 'head'}</span>
        <span className="spacer" />
        {entry?.loading && <span className="muted">refreshing…</span>}
        <button className="icon" title="Refresh comments" onClick={() => void loadDetails(tab.root, tab.number, true)}>↻</button>
        {details && <button className="link" onClick={() => openInBrowser(`${details.url}/files`)}>Open on GitHub ↗</button>}
      </div>
      {(error || entry?.error) && <div className="placeholder error selectable">{error ?? entry?.error}</div>}
      {note && <div className="placeholder muted">{note}</div>}
      {!sides && !error && !entry?.error && <div className="placeholder">Loading…</div>}
      {details && detached.length > 0 && <DetachedThreads root={tab.root} number={tab.number} threads={detached} />}
      {editors && details && (
        <DiffEditors
          tab={tab}
          sides={editors}
          details={details}
          language={language}
          anchors={placement.anchors}
          byId={byId}
          onAdd={(side, line) => setDraft({ side, line })}
          onDraftDone={() => setDraft(null)}
        />
      )}
      {details && <ReviewBar root={tab.root} details={details} />}
    </div>
  )
}

interface EditorsProps {
  tab: PullRequestDiffTab
  sides: Sides
  details: PullRequestDetails
  language: Extension
  anchors: Anchor[]
  byId: Map<string, Thread>
  onAdd: (side: Side, line: number) => void
  onDraftDone: () => void
}

function DiffEditors({ tab, sides, details, language, anchors, byId, onAdd, onDraftDone }: EditorsProps) {
  const left = useRef<HTMLDivElement>(null)
  const right = useRef<HTMLDivElement>(null)
  const layout = useMemo(() => new ThreadLayout(), [])
  const hosts = useSyncExternalStore(layout.subscribe, layout.getHosts)
  // Read when the editors are built; later changes go in through effects.
  const latest = useRef({ anchors, onAdd })
  latest.current = { anchors, onAdd }
  const folds = useRef<Fold[]>([])

  useEffect(() => {
    if (!left.current || !right.current) return
    const { base, head, hunks, lines, diff } = sides
    const initial = latest.current.anchors
    folds.current = foldsClearOf(foldsFor(hunks, lines.a, COLLAPSE), initial)
    const views: EditorView[] = []
    const expand = (index: number) => views.forEach((v) => v.dispatch({ effects: expandFold.of(index) }))
    const add = (side: Side, line: number) => latest.current.onAdd(side, line)
    const readOnly = [EditorState.readOnly.of(true), EditorView.editable.of(false)]
    const side = (s: Side, parent: HTMLElement, doc: string) =>
      new EditorView({
        parent,
        doc,
        extensions: [
          baseExtensions(),
          language,
          readOnly,
          commentGutter(s, (n) => isCommentable(s === 'a' ? diff.left : diff.right, n), add),
          diffSide(s, hunks, folds.current, expand),
          threadDecorations(s, layout, initial),
        ],
      })
    views.push(side('a', left.current, base), side('b', right.current, head))
    layout.views = views
    return () => {
      layout.views = []
      views.forEach((v) => v.destroy())
    }
  }, [sides, language, layout])

  // New, answered or resolved threads and the comment box: same editors, new widgets.
  useEffect(() => {
    const views = layout.views
    if (views.length === 0) return
    // A thread inside a folded stretch (e.g. just posted by someone else) unfolds it.
    const hidden = folds.current.filter((f) => foldsClearOf([f], anchors).length === 0).map((f) => folds.current.indexOf(f))
    views.forEach((v) => v.dispatch({ effects: [setThreadAnchors.of(anchors), ...hidden.map((i) => expandFold.of(i))] }))
  }, [anchors, layout])

  const anchorsByKey = new Map(anchors.map((a) => [a.key, a]))
  return (
    <div className="diff-host">
      <div className="diff-sides">
        <div className="diff-side" ref={left} />
        <div className="diff-side" ref={right} />
      </div>
      {[...hosts].map(([key, el]) => {
        const anchor = anchorsByKey.get(key)
        if (!anchor) return null
        return createPortal(
          <AnchorContent tab={tab} anchor={anchor} byId={byId} pendingReview={!!details.pendingReviewId} onDraftDone={onDraftDone} />,
          el,
          key,
        )
      })}
    </div>
  )
}

function AnchorContent({ tab, anchor, byId, pendingReview, onDraftDone }: {
  tab: PullRequestDiffTab
  anchor: Anchor
  byId: Map<string, Thread>
  pendingReview: boolean
  onDraftDone: () => void
}) {
  return (
    <div className="pr-anchor">
      {anchor.threadIds.map((id) => {
        const t = byId.get(id)
        return t ? <ReviewThread key={id} root={tab.root} number={tab.number} thread={t} /> : null
      })}
      {anchor.draft && (
        <CommentComposer
          root={tab.root}
          number={tab.number}
          path={tab.path}
          line={anchor.line + 1}
          side={anchor.side === 'a' ? 'LEFT' : 'RIGHT'}
          pendingReview={pendingReview}
          onDone={onDraftDone}
        />
      )}
    </div>
  )
}

/** Threads that aren't on the current diff: outdated ones and comments on the whole file. */
function DetachedThreads({ root, number, threads }: { root: string; number: number; threads: Thread[] }) {
  const [open, setOpen] = useState(threads.some((t) => !t.resolved))
  return (
    <div className="pr-detached">
      <div className="pr-section-title clickable" onClick={() => setOpen(!open)}>
        <span className="twisty">{open ? '▾' : '▸'}</span>
        <span>
          {threads.length} comment thread{threads.length === 1 ? '' : 's'} not on this diff
        </span>
      </div>
      {open &&
        threads.map((t) => (
          <ReviewThread
            key={t.id}
            root={root}
            number={number}
            thread={t}
            label={t.fileLevel ? 'File' : `Outdated · ${t.side === 'LEFT' ? 'base' : 'head'} line ${t.originalLine || '?'}`}
          />
        ))}
    </div>
  )
}

function normalize(text: string): string {
  return text.replace(/\r\n?/g, '\n')
}
