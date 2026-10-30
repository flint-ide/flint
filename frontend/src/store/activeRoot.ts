import { pickRoot } from '../lib/workspaceRoots'
import { useAgentChat } from './agentChat'
import { useEditor } from './editor'
import { useTerminal } from './terminal'
import { useWorkspace } from './workspace'

/** The path the active tab is about: a file, or a terminal's project / a chat's folder. */
export function activeContextPath(): string | null {
  const { tabs, activeId } = useEditor.getState()
  const tab = tabs.find((t) => t.id === activeId)
  switch (tab?.kind) {
    case 'file':
    case 'diff':
    case 'history':
    case 'http':
      return tab.path
    case 'terminal': {
      const term = useTerminal.getState().tabs.find((t) => t.id === tab.termId)
      return term?.project ?? term?.cwd ?? null
    }
    case 'chat':
      return useAgentChat.getState().sessions[tab.sessionId]?.cwd ?? null
    default:
      return null
  }
}

/**
 * The folder a view following the active tab shows (Commit panel, run configurations):
 * the active tab's folder. A tab without one (scratch, run output) keeps current while
 * it is still in the workspace.
 */
export function followedRoot(current: string | null): string | null {
  const { roots } = useWorkspace.getState()
  const context = activeContextPath()
  if (!context && current && roots.includes(current)) return current
  return pickRoot(roots, context)
}

/** Workspace folder new terminals and chats open in: the active tab's, else the primary one. */
export function activeRoot(): string {
  return pickRoot(useWorkspace.getState().roots, activeContextPath()) ?? ''
}

/** Workspace folder of path (a file's project), else the primary one; '' when nothing is open. */
export function rootFor(path: string | null | undefined): string {
  return pickRoot(useWorkspace.getState().roots, path) ?? ''
}
