import { BadgeCounter, IconButton } from '@flint-ide/ui'
import { useWorkspace } from '../../store/workspace'
import { effectiveKeys, getCommand } from '../../lib/commands'
import { usePanels } from '../../panels'
import { isPanelAvailable, panelTooltip, type PanelSpec } from '../../panels/registry'
import { Icon } from '../ui/Icon'

/** The tooltip with the panel's current shortcut (commands register after the first render and can be rebound). */
function tooltip(spec: PanelSpec): string {
  const cmd = spec.shortcutCommand ? getCommand(spec.shortcutCommand) : undefined
  return panelTooltip(spec, cmd && effectiveKeys(cmd))
}

export function ActivityBar() {
  const sidebar = useWorkspace((s) => s.sidebar)
  const visible = useWorkspace((s) => s.sidebarVisible)
  const setSidebar = useWorkspace((s) => s.setSidebar)
  const toggleSidebar = useWorkspace((s) => s.toggleSidebar)
  const panels = usePanels()

  return (
    <nav className="activity-bar">
      {panels.filter(isPanelAvailable).map((v) => {
        const active = visible && sidebar === v.id
        const onClick = () => (active ? toggleSidebar() : setSidebar(v.id))
        return v.useBadge ? (
          <BadgedPanelButton key={v.id} spec={v} use={v.useBadge} active={active} onClick={onClick} />
        ) : (
          <PanelButton key={v.id} spec={v} active={active} onClick={onClick} />
        )
      })}
    </nav>
  )
}

interface PanelButtonProps {
  spec: PanelSpec
  active: boolean
  onClick: () => void
}

/** A panel's button with its count (useBadge), e.g. the agents that need you; the count is part of the button's name. */
function BadgedPanelButton({ use, ...props }: PanelButtonProps & { use: () => number }) {
  return <PanelButton {...props} count={use()} />
}

function PanelButton({ spec, active, onClick, count = 0 }: PanelButtonProps & { count?: number }) {
  return (
    <IconButton
      variant="ghost"
      size="sm"
      className="activity"
      data-active={active || undefined}
      aria-label={count > 0 ? `${spec.title}, ${count} need you` : spec.title}
      title={tooltip(spec)}
      // Refreshed on hover: no re-render follows registering or rebinding a command.
      onPointerEnter={(e) => {
        e.currentTarget.title = tooltip(spec)
      }}
      onClick={onClick}
    >
      <Icon name={spec.icon} />
      {count > 0 && <BadgeCounter count={count} max={99} tone="brand" overlay className="activity-badge" aria-hidden />}
    </IconButton>
  )
}
