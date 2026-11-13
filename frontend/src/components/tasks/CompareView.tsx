import { attemptLabels, changeCell, formatTaskUsage, stateInfo, taskActions } from '../../lib/agentTasks'
import type { TaskView } from '../../lib/agentTasks'
import {
  closeCompare,
  keepTask,
  openComparedFileDiff,
  refreshCompare,
  selectCompareFile,
  setComparePair,
  useTasks,
  type TaskFileDiff,
} from '../../store/agentTasks'
import { Modal } from '../layout/Modal'
import { Select } from '../ui/Select'

const MARK: Record<string, string> = { add: '+', del: '−', ctx: ' ', hunk: '' }

/** One diff as Go made it: unified lines, or the note why there are none. */
function DiffLines({ d }: { d: TaskFileDiff | null | undefined }) {
  if (!d) return <div className="placeholder muted">Loading…</div>
  return (
    <div className="compare-diff scroll selectable">
      {d.note && <div className="muted compare-note">{d.note}</div>}
      {d.lines.map((l, i) => (
        <div key={i} className={`compare-line ${l.kind}`}>
          <span className="compare-no">{l.a || ''}</span>
          <span className="compare-no">{l.b || ''}</span>
          <span className="compare-mark">{MARK[l.kind] ?? ' '}</span>
          <span className="compare-text">{l.text}</span>
        </div>
      ))}
      {d.truncated && <div className="muted compare-note">The diff is longer: open it in the diff view.</div>}
    </div>
  )
}

/**
 * Compare a group of tasks: the files any of them changed with each task's
 * +/−, and the selected file's diff in every task side by side (against where
 * the task left its base), or directly between two tasks. Go computes all of it.
 */
export function CompareView() {
  const compare = useTasks((s) => s.compare)
  const busy = useTasks((s) => s.busy)
  if (!compare) return null
  const { data, path, file, pair, between, error } = compare
  const tasks = data?.tasks ?? []
  const labels = attemptLabels(tasks.map((c) => c.task as TaskView))
  const row = data?.files.find((f) => f.path === path)
  const live = tasks.filter((c) => !c.task.stale)
  const pairOptions = live.map((c) => ({ value: c.task.id, label: labels[c.task.id] }))

  return (
    <Modal title={`Compare: ${tasks[0]?.task.title ?? 'agent tasks'}`} onClose={closeCompare} className="compare-tasks">
      {error && <div className="form-status error">{error}</div>}
      {!data && !error && <div className="placeholder muted">Loading…</div>}
      {data && (
        <div className="compare-body">
          <div className="compare-files scroll">
            <table>
              <thead>
                <tr>
                  <th>
                    {data.files.length} {data.files.length === 1 ? 'file' : 'files'}
                    <button className="link" onClick={() => void refreshCompare()} title="Read the changes again">Refresh</button>
                  </th>
                  {tasks.map((c) => {
                    const t = c.task as TaskView
                    const a = taskActions(t)
                    const s = stateInfo(t.state)
                    return (
                      <th key={t.id} title={`${t.branch}\n${t.worktree}`}>
                        <div className="compare-agent">
                          {labels[t.id]} <span className={`task-state ${t.stale ? 'stale' : s.state}`}>{t.stale ? 'stale' : s.label}</span>
                        </div>
                        <div className="muted">
                          {c.error || `${c.count} ${c.count === 1 ? 'file' : 'files'} +${c.added} −${c.deleted} · ↑${t.ahead}`}
                        </div>
                        {formatTaskUsage(t) && <div className="muted">{formatTaskUsage(t)}</div>}
                        <div className="compare-keep">
                          {a.merge && (
                            <button className="link" disabled={!!busy[t.id]} onClick={() => void keepTask(t, 'merge')} title={`Keep this one: merge it into ${t.base}, then choose what happens to the others`}>
                              Keep: Merge
                            </button>
                          )}
                          {a.pr && (
                            <button className="link" disabled={!!busy[t.id]} onClick={() => void keepTask(t, 'pr')} title="Keep this one: create its pull request, then choose what happens to the others">
                              Keep: PR
                            </button>
                          )}
                        </div>
                      </th>
                    )
                  })}
                </tr>
              </thead>
              <tbody>
                {data.files.map((f) => (
                  <tr key={f.path} className={f.path === path ? 'selected' : ''} onClick={() => void selectCompareFile(f.path)}>
                    <td className="compare-path" title={f.path}>{f.path}</td>
                    {f.changes.map((c, i) => (
                      <td key={tasks[i].task.id} className={c ? (c.untracked ? 'change-untracked' : '') : 'muted'}>{changeCell(c)}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
            {data.files.length === 0 && <div className="placeholder muted">No task changed a file yet.</div>}
            {data.truncated && <div className="muted compare-note">More files were changed than are listed.</div>}
          </div>
          {path && (
            <>
              <div className="compare-columns">
                {tasks.map((c, i) => {
                  const change = row?.changes[i]
                  return (
                    <div key={c.task.id} className="compare-column">
                      <div className="compare-column-head">
                        <span className="truncate">{labels[c.task.id]} · {path}</span>
                        <span className="muted">{changeCell(change)}</span>
                        <span className="spacer" />
                        {!c.task.stale && (
                          <button className="link" onClick={() => openComparedFileDiff(c, path, change?.origPath ?? '')} title="Open the side-by-side diff against the base in a tab">
                            Open diff
                          </button>
                        )}
                      </div>
                      <DiffLines d={file?.tasks[i]} />
                    </div>
                  )
                })}
              </div>
              {pair && (
                <div className="compare-column compare-between">
                  <div className="compare-column-head">
                    <span>Direct diff</span>
                    <Select size="sm" value={pair[0]} options={pairOptions} onChange={(id) => void setComparePair([id, pair[1]])} />
                    <span>→</span>
                    <Select size="sm" value={pair[1]} options={pairOptions} onChange={(id) => void setComparePair([pair[0], id])} />
                    <span className="muted">{between ? `+${between.added} −${between.deleted}` : ''}</span>
                  </div>
                  <DiffLines d={between} />
                </div>
              )}
            </>
          )}
        </div>
      )}
      <div className="form-actions">
        <span className="spacer" />
        <button type="button" onClick={closeCompare}>Close</button>
      </div>
    </Modal>
  )
}
