import { Suspense, lazy } from 'react'
import { useEditor, type Tab } from '../../store/editor'
import { confirmAction } from '../../store/dialog'
import { Welcome } from '../layout/Welcome'
import { ErrorBoundary } from '../layout/ErrorBoundary'

// CodeMirror and the SQL console load on first use, keeping startup to layout + terminal.
const FileView = lazy(() => import('./FileView').then((m) => ({ default: m.FileView })))
const ScratchView = lazy(() => import('./ScratchView').then((m) => ({ default: m.ScratchView })))
const DiffView = lazy(() => import('./DiffView').then((m) => ({ default: m.DiffView })))

/** Closes a tab, asking first when a file tab has unsaved changes. */
export async function requestCloseTab(tab: Tab): Promise<void> {
  // Scratchpads autosave, so only file tabs can lose work.
  if (tab.kind === 'file' && tab.dirty) {
    const ok = await confirmAction(`Discard unsaved changes in ${tab.title}?`, 'Discard')
    if (!ok) return
  }
  useEditor.getState().closeTab(tab.id)
}

export function EditorArea() {
  const tabs = useEditor((s) => s.tabs)
  const activeId = useEditor((s) => s.activeId)
  const setActive = useEditor((s) => s.setActive)

  if (tabs.length === 0) return <Welcome />

  return (
    <div className="editor-area">
      <div className="tab-bar" role="tablist">
        {tabs.map((tab) => (
          <div
            key={tab.id}
            role="tab"
            aria-selected={tab.id === activeId}
            className={`tab${tab.id === activeId ? ' active' : ''}`}
            title={tab.kind === 'file' ? tab.path : tab.title}
            onClick={() => setActive(tab.id)}
            onMouseDown={(e) => e.button === 1 && void requestCloseTab(tab)}
          >
            <span className={`tab-icon ${tab.kind === 'scratch' ? `scratch-${tab.scratchKind}` : tab.kind}`} />
            <span className="tab-title">{tab.title}</span>
            <button
              className={`tab-close${tab.dirty ? ' dirty' : ''}`}
              onClick={(e) => {
                e.stopPropagation()
                void requestCloseTab(tab)
              }}
              aria-label="Close tab"
            />
          </div>
        ))}
      </div>
      <div className="tab-panels">
        {/* All tabs stay mounted so undo history, scroll and results survive tab switches. */}
        {tabs.map((tab) => (
          <div key={tab.id} className="tab-panel" hidden={tab.id !== activeId}>
            <ErrorBoundary label={tab.title}>
              <Suspense fallback={null}>
                {tab.kind === 'file' && <FileView tab={tab} active={tab.id === activeId} />}
                {tab.kind === 'scratch' && <ScratchView tab={tab} active={tab.id === activeId} />}
                {tab.kind === 'diff' && <DiffView tab={tab} />}
              </Suspense>
            </ErrorBoundary>
          </div>
        ))}
      </div>
    </div>
  )
}
