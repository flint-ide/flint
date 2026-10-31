import { useEffect, useMemo, useState } from 'react'
import { ClipboardSetText } from '../../wails/runtime'
import { formatValue, frameAt, percent, searchFrames, type FlameFrame, type TopRow } from '../../lib/flame'
import { showToast } from '../../store/dialog'
import { useEditor } from '../../store/editor'
import { revealProfile, selectProfileFile, selectSampleType, useProfile } from '../../store/profile'
import { Dropdown } from '../ui/Dropdown'
import { Select } from '../ui/Select'
import { FlameGraph } from './FlameGraph'
import { TopTable } from './TopTable'

interface Props {
  profileId: string
}

/** A profile tab: flame graph or top table of one profile file (store/profile.ts). */
export function ProfileView({ profileId }: Props) {
  const doc = useProfile((s) => s.docs[profileId])
  const [view, setView] = useState<'flame' | 'top'>('flame')
  const [query, setQuery] = useState('')
  const [focus, setFocus] = useState<number[]>([])
  const report = doc?.report ?? null
  const root = report?.root as FlameFrame | undefined

  // A new report (file or sample type) has another tree: zoom paths don't carry over.
  useEffect(() => setFocus([]), [report])
  const search = useMemo(() => searchFrames(root ?? { name: '', self: 0, total: 0 }, query), [root, query])

  if (!doc) return null
  const file = doc.files[doc.file]
  const unit = report?.unit ?? ''
  const zoomed = root ? frameAt(root, focus) : null

  const openSource = (target: Pick<FlameFrame, 'name' | 'file' | 'line'>) => {
    if (!target.file?.startsWith('/')) return showToast(`No source location for ${target.name}`)
    useEditor.getState().openFile(target.file, target.line ? { line: target.line, from: 0, to: 0 } : undefined)
  }
  const highlight = (row: TopRow) => {
    setQuery(row.name)
    setView('flame')
  }

  return (
    <div className="profile-view">
      <div className="run-toolbar profile-toolbar">
        <div className="profile-modes" role="tablist">
          <button role="tab" aria-selected={view === 'flame'} className={view === 'flame' ? 'on' : ''} onClick={() => setView('flame')}>
            Flame Graph
          </button>
          <button role="tab" aria-selected={view === 'top'} className={view === 'top' ? 'on' : ''} onClick={() => setView('top')}>
            Top
          </button>
        </div>
        {doc.files.length > 1 && (
          <Select
            size="sm"
            value={doc.file}
            options={doc.files.map((f, i) => ({ value: i, label: f.label }))}
            onChange={(i) => selectProfileFile(profileId, i)}
            title="Profile"
          />
        )}
        {report && report.sampleTypes.length > 1 && (
          <Select
            size="sm"
            value={report.sampleType}
            options={report.sampleTypes.map((t) => ({ value: t, label: t }))}
            onChange={(t) => selectSampleType(profileId, t)}
            title="Sample type"
          />
        )}
        <input
          className="profile-search"
          type="search"
          value={query}
          placeholder="Search functions"
          spellCheck={false}
          onChange={(e) => setQuery(e.target.value)}
        />
        {query && report && view === 'flame' && (
          <span className="muted" title="Share of the profile in matching frames">
            {search.matched.size ? `${percent(search.total, report.total)} matched` : 'no match'}
          </span>
        )}
        {view === 'flame' && (
          <button className="icon" disabled={focus.length === 0} title="Reset zoom (Esc)" onClick={() => setFocus([])}>
            Reset Zoom
          </button>
        )}
        <span className="spacer" />
        {report && (
          <span className="muted profile-summary" title={report.trimmed ? `${report.trimmed} frames under 0.5% are hidden` : undefined}>
            {zoomed && focus.length > 0 ? `${zoomed.name.slice(zoomed.name.lastIndexOf('/') + 1)} · ` : ''}
            {report.sampleType} {formatValue(report.total, unit)}
            {report.durationNs > 0 && ` in ${formatValue(report.durationNs, 'nanoseconds')}`}
          </span>
        )}
        {file && (
          <Dropdown
            align="end"
            items={[
              { label: 'Copy Path', hint: file.path, onSelect: () => void copyPath(file.path) },
              { label: 'Reveal in Finder', onSelect: () => void revealProfile(file.path) },
            ]}
            trigger={(t) => (
              <button {...t} className="icon" title={`Raw profile: ${file.path} (go tool pprof)`}>
                Raw .pprof ⌄
              </button>
            )}
          />
        )}
      </div>
      <div className="profile-body">
        {doc.error ? (
          <div className="profile-message error">{doc.error}</div>
        ) : !report || !root ? (
          <div className="profile-message muted">{doc.loading ? 'Loading profile…' : ''}</div>
        ) : report.total === 0 ? (
          <div className="profile-message muted">The profile has no samples (the program was idle, or ran too briefly).</div>
        ) : view === 'flame' ? (
          <FlameGraph root={root} unit={unit} focus={focus} onFocus={setFocus} search={search} onOpen={openSource} />
        ) : (
          <TopTable rows={report.top} total={report.total} unit={unit} query={query} onOpen={openSource} onHighlight={highlight} />
        )}
      </div>
    </div>
  )
}

async function copyPath(path: string): Promise<void> {
  if (await ClipboardSetText(path)) showToast(`Copied ${path}`)
}
