import { useWorkspace } from '../../store/workspace'
import { useEditor } from '../../store/editor'
import { effectiveKeys, getCommand, formatKeys } from '../../lib/commands'
import { PANELS } from '../../panels'
import { isPanelAvailable, panelTooltip, type PanelSpec } from '../../panels/registry'

/** The tooltip with the panel's current shortcut (commands register after the first render and can be rebound). */
function tooltip(spec: PanelSpec): string {
  const cmd = spec.shortcutCommand ? getCommand(spec.shortcutCommand) : undefined
  return panelTooltip(spec, cmd && effectiveKeys(cmd))
}

export function ActivityBar({ onTerminal }: { onTerminal: () => void }) {
  const terminalActive = useEditor((s) => s.activeId?.startsWith('term:') ?? false)
  const sidebar = useWorkspace((s) => s.sidebar)
  const visible = useWorkspace((s) => s.sidebarVisible)
  const setSidebar = useWorkspace((s) => s.setSidebar)
  const toggleSidebar = useWorkspace((s) => s.toggleSidebar)

  return (
    <nav className="activity-bar">
      {PANELS.filter(isPanelAvailable).map((v) => (
        <button
          key={v.id}
          className={`activity${visible && sidebar === v.id ? ' active' : ''}`}
          title={tooltip(v)}
          // Refreshed on hover: no re-render follows registering or rebinding a command.
          onPointerEnter={(e) => {
            e.currentTarget.title = tooltip(v)
          }}
          onClick={() => (visible && sidebar === v.id ? toggleSidebar() : setSidebar(v.id))}
        >
          {v.icon}
        </button>
      ))}
      <span className="spacer" />
      <button className={`activity${terminalActive ? ' active' : ''}`} title={`Terminal (${formatKeys('Mod+J')})`} onClick={onTerminal}>
        ❯_
      </button>
    </nav>
  )
}
