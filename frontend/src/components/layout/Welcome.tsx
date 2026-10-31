import { useEffect, useState } from 'react'
import { Workspace } from '../../api/backend'
import { useWorkspace } from '../../store/workspace'
import { openToolCheck } from '../../store/toolCheck'

const SHORTCUTS: [string, string][] = [
  ['⌘P', 'Go to file'],
  ['⇧⌘F', 'Search in project'],
  ['⌘K', 'Commit'],
  ['⇧⌘K', 'Push'],
  ['⌘J', 'Terminal ↔ last file'],
  ['⌘[ / ⌘]', 'Back / forward'],
  ['⌘N', 'New SQL console'],
  ['⌘↵', 'Run statement'],
  ['⌘B', 'Toggle sidebar'],
]

export function Welcome() {
  const root = useWorkspace((s) => s.root)
  const openFolder = useWorkspace((s) => s.openFolder)
  const [recent, setRecent] = useState<string[]>([])

  useEffect(() => {
    void Workspace.RecentFolders().then((r) => setRecent(r ?? []))
  }, [root])

  const pick = async () => {
    const path = await Workspace.PickFolder()
    if (path) await openFolder(path)
  }

  return (
    <div className="welcome">
      <div className="welcome-logo">flint</div>
      {!root && (
        <div className="welcome-actions">
          <button className="primary" onClick={() => void pick()}>Open folder…</button>
          {recent.length > 0 && (
            <div className="recent">
              {recent.map((p) => (
                <button key={p} className="link" onClick={() => void openFolder(p)} title={p}>
                  {p.split('/').pop()} <span className="muted">{p}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}
      <dl className="shortcuts">
        {SHORTCUTS.map(([k, label]) => (
          <div key={k}>
            <dt>{label}</dt>
            <dd><kbd>{k}</kbd></dd>
          </div>
        ))}
      </dl>
      <button className="link welcome-tools" onClick={openToolCheck}>Check installed tools…</button>
    </div>
  )
}
