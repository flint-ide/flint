import { useWorkspace, type SidebarView } from '../../store/workspace'
import { useEditor } from '../../store/editor'

const VIEWS: { id: SidebarView; icon: string; title: string }[] = [
  { id: 'files', icon: '▤', title: 'Files' },
  { id: 'search', icon: '⌕', title: 'Search (⇧⌘F)' },
  { id: 'git', icon: '⎇', title: 'Commit (⌘K)' },
  { id: 'pulls', icon: '⇅', title: 'Pull Requests' },
  { id: 'database', icon: '⛁', title: 'Databases' },
  { id: 'scratch', icon: '✎', title: 'Scratchpads' },
  { id: 'todo', icon: '☑', title: 'TODO' },
  { id: 'docker', icon: '⛴', title: 'Docker' },
  { id: 'aws', icon: '☁', title: 'AWS Profiles' },
  { id: 'kube', icon: '☸', title: 'Kubernetes' },
]

export function ActivityBar({ onTerminal }: { onTerminal: () => void }) {
  const terminalActive = useEditor((s) => s.activeId?.startsWith('term:') ?? false)
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
      <button className={`activity${terminalActive ? ' active' : ''}`} title="Terminal (⌘J)" onClick={onTerminal}>
        ❯_
      </button>
    </nav>
  )
}
