import { useWorkspace, type SidebarView } from '../../store/workspace'

const VIEWS: { id: SidebarView; icon: string; title: string }[] = [
  { id: 'files', icon: '▤', title: 'Files' },
  { id: 'search', icon: '⌕', title: 'Search (⇧⌘F)' },
  { id: 'git', icon: '⎇', title: 'Commit (⌘K)' },
  { id: 'database', icon: '⛁', title: 'Databases' },
  { id: 'scratch', icon: '✎', title: 'Scratchpads' },
]

export function ActivityBar({ onTerminal, terminalOpen }: { onTerminal: () => void; terminalOpen: boolean }) {
  const sidebar = useWorkspace((s) => s.sidebar)
  const visible = useWorkspace((s) => s.sidebarVisible)
  const setSidebar = useWorkspace((s) => s.setSidebar)
  const toggleSidebar = useWorkspace((s) => s.toggleSidebar)

  return (
    <nav className="activity-bar">
      {VIEWS.map((v) => (
        <button
          key={v.id}
          className={`activity${visible && sidebar === v.id ? ' active' : ''}`}
          title={v.title}
          onClick={() => (visible && sidebar === v.id ? toggleSidebar() : setSidebar(v.id))}
        >
          {v.icon}
        </button>
      ))}
      <span className="spacer" />
      <button className={`activity${terminalOpen ? ' active' : ''}`} title="Terminal (⌘J)" onClick={onTerminal}>
        ❯_
      </button>
    </nav>
  )
}
