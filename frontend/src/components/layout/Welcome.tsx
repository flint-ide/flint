import { useEffect, useState } from 'react'
import { Button, Kbd, ListGroup, ListItem } from '@flint-ide/ui'
import { GitFork, LayoutDashboard, Workflow } from '@flint-ide/ui/icons'
import { Workspace } from '../../api/backend'
import { useWorkspace } from '../../store/workspace'
import { openToolCheck } from '../../store/toolCheck'
import { formatKeys, runCommandById } from '../../lib/commands'
import { useActiveFeatures } from '../../lib/activeFeatures'
import { openNewTask } from '../../store/agentTasks'

const SHORTCUTS: [string, string][] = [
  [formatKeys('Mod+P'), 'Go to file'],
  [formatKeys('Mod+Shift+F'), 'Find in files'],
  [formatKeys('Mod+K'), 'Commit'],
  [formatKeys('Mod+Shift+K'), 'Push'],
  [formatKeys('Mod+J'), 'Terminal ↔ last file'],
  [`${formatKeys('Mod+[')} / ${formatKeys('Mod+]')}`, 'Back / forward'],
  [formatKeys('Mod+N'), 'New SQL console'],
  [formatKeys('Mod+Enter'), 'Run statement'],
  [formatKeys('Mod+B'), 'Toggle sidebar'],
]

/** The AI-native starting points; the ones of a feature show only while the feature runs. */
function WorkWithAgents() {
  const active = useActiveFeatures((s) => s.ids)
  return (
    <ListGroup label="Work with agents" className="welcome-agents">
      {active.has('workflows') && (
        <ListItem
          leading={<Workflow aria-hidden />}
          label="Run a workflow…"
          meta={['Plan, build, test, review and approve a change, the same way every time']}
          onClick={() => runCommandById('workflows.run')}
        />
      )}
      {active.has('projects') && (
        <ListItem
          leading={<LayoutDashboard aria-hidden />}
          label="Open the project board"
          meta={['Plan work as items, then hand them to agents']}
          onClick={() => runCommandById('projects.board')}
        />
      )}
      <ListItem
        leading={<GitFork aria-hidden />}
        label="New agent task…"
        meta={['Give an agent its own branch and worktree to work in']}
        onClick={openNewTask}
      />
    </ListGroup>
  )
}

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
          <Button variant="primary" onClick={() => void pick()}>Open folder…</Button>
          {recent.length > 0 && (
            <div className="recent">
              {recent.map((p) => (
                <Button key={p} variant="ghost" size="sm" onClick={() => void openFolder(p)} title={p}>
                  {p.split('/').pop()} <span className="muted">{p}</span>
                </Button>
              ))}
            </div>
          )}
        </div>
      )}
      {root && <WorkWithAgents />}
      <dl className="shortcuts">
        {SHORTCUTS.map(([k, label]) => (
          <div key={k}>
            <dt>{label}</dt>
            <dd><Kbd>{k}</Kbd></dd>
          </div>
        ))}
      </dl>
      <Button variant="link" size="sm" onClick={openToolCheck}>Check installed tools…</Button>
    </div>
  )
}
