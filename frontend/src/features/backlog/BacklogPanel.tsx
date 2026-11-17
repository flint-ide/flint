import { useEffect } from 'react'
import { Dropdown } from '../../components/ui/Dropdown'
import { Select } from '../../components/ui/Select'
import { BacklogDialogs } from './Dialogs'
import { ItemActions, ItemMeta, ItemTasks, ItemTitle } from './ItemParts'
import { canClose, groupByStatus, PANEL_STATUSES, statusLabel, type BacklogData, type ProblemInfo } from './backlog'
import {
  askAboutProblem,
  closeBacklog,
  followBacklogs,
  loadBacklog,
  newBacklog,
  openBacklog,
  openBoard,
  openNewItem,
  selectBacklog,
  toggleGroup,
  useBacklog,
} from './store'

/** The Backlog panel: the picker, the items by status and the problems of the selected backlog. */
export function BacklogPanel() {
  const { infos, selected, data, collapsed } = useBacklog()
  useEffect(() => followBacklogs(() => [useBacklog.getState().selected]), [])
  const info = infos?.find((i) => i.path === selected)
  const loaded = data[selected]
  const backlog = loaded?.backlog ?? null

  return (
    <div className="panel backlog-panel">
      <div className="panel-header">
        <span className="panel-title">Backlog</span>
        <button className="icon" title="New Item…" disabled={!info} onClick={() => openNewItem(selected)}>＋</button>
        <button className="icon" title="Refresh" disabled={!info} onClick={() => void loadBacklog(selected)}>↻</button>
        <Dropdown
          align="end"
          minWidth={190}
          items={[
            { label: 'Open Backlog…', onSelect: () => void openBacklog() },
            { label: 'New Backlog…', onSelect: () => void newBacklog() },
            { label: 'Show Board', disabled: !info, onSelect: () => info && openBoard(info) },
            { type: 'separator' },
            { label: 'Close Backlog', disabled: !canClose(info), hint: info?.workspace ? 'workspace folder' : undefined, onSelect: () => info && void closeBacklog(info) },
          ]}
          trigger={(t) => (
            <button {...t} className="icon" title="Open, create or close a backlog">⋯</button>
          )}
        />
      </div>
      {infos && infos.length > 0 && (
        <div className="backlog-picker">
          <Select
            block
            size="sm"
            value={selected}
            title={selected}
            options={infos.map((i) => ({ value: i.path, label: i.name, hint: i.error || (i.workspace ? 'workspace folder' : i.path) }))}
            onChange={selectBacklog}
          />
        </div>
      )}
      <div className="tree scroll">
        {info?.error && <div className="placeholder error">{info.error}</div>}
        {loaded?.error && !info?.error && <div className="placeholder error">{loaded.error}</div>}
        {backlog && <Groups backlog={backlog} collapsed={collapsed} />}
        {infos?.length === 0 && (
          <div className="placeholder muted">
            <span>
              No backlog yet. A backlog is a folder of work items (Markdown files) for one or more repositories. <b>Delegate</b> turns an item
              into agent tasks, one per repository, and the item&apos;s status follows them.
            </span>
            <span>
              <button className="link" onClick={() => void openBacklog()}>Open Backlog…</button> ·{' '}
              <button className="link" onClick={() => void newBacklog()}>New Backlog…</button>
            </span>
          </div>
        )}
        {backlog && backlog.items.length === 0 && backlog.problems.length === 0 && (
          <div className="placeholder muted">
            No items. <button className="link" onClick={() => openNewItem(selected)}>New Item…</button> writes one to items/.
          </div>
        )}
        {backlog && backlog.problems.length > 0 && <Problems backlog={backlog} />}
      </div>
      <BacklogDialogs host="panel" />
    </div>
  )
}

function Groups({ backlog, collapsed }: { backlog: BacklogData; collapsed: Record<string, boolean> }) {
  const groups = groupByStatus(backlog.items)
  return (
    <>
      {PANEL_STATUSES.map((status) => {
        const items = groups[status]
        if (items.length === 0) return null
        const open = !collapsed[status]
        return (
          <div key={status} className="backlog-group">
            <div className="tree-row static root-row" onClick={() => toggleGroup(status, !open)} role="button" aria-expanded={open}>
              <span className="twisty">{open ? '▾' : '▸'}</span>
              <span className="dir-name">{statusLabel(status)}</span>
              <span className="badge">{items.length}</span>
            </div>
            {open &&
              items.map((item) => (
                <div key={item.id} className="task backlog-item">
                  <div className="tree-row static task-row" title={item.path}>
                    <ItemTitle item={item} />
                  </div>
                  <ItemMeta item={item} backlog={backlog} />
                  <ItemTasks tasks={item.tasks} />
                  <ItemActions backlog={backlog} item={item} />
                </div>
              ))}
          </div>
        )
      })}
    </>
  )
}

/** Files Flint could not use, each with the hand-off to the agent. */
function Problems({ backlog }: { backlog: BacklogData }) {
  return (
    <div className="backlog-problems">
      <div className="tree-row static root-row">
        <span className="dir-name">Problems</span>
        <span className="badge">{backlog.problems.length}</span>
      </div>
      {backlog.problems.map((p: ProblemInfo, i) => (
        <div key={`${p.file}:${i}`} className="backlog-problem">
          <span className="backlog-problem-file" title={p.file}>{p.file}</span>
          <span className="task-error">{p.reason}</span>
          <div className="task-actions">
            <button className="link" onClick={() => askAboutProblem(backlog, p)} title="Put the problem in an agent chat">✦ Ask the agent</button>
          </div>
        </div>
      ))}
    </div>
  )
}
