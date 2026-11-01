import { useEffect, useState } from 'react'
import * as UsageAPI from '../../../bindings/flint/usageapi'
import * as models from '../../../bindings/flint/internal/usage/models'
import { errorMessage } from '../../api/backend'
import {
  barHeights, chartMetric, costLabel, metricLabel, metricValue, projectName, shortDay, spentOf, tokensOf, totalsLabel, totalsTooltip,
  type ChartMetric, type UsageTotals,
} from '../../lib/usageView'
import { formatCost, formatTokens } from '../../lib/usage'
import { chatTabId, useAgentChat } from '../../store/agentChat'
import { showToast } from '../../store/dialog'
import { useEditor } from '../../store/editor'
import { useUsage } from '../../store/usage'

interface Group {
  key: string
  totals: UsageTotals
}

interface ChatRow {
  id: string
  label: string
  project: string
  agent: string
  model?: string
  task?: string
  totals: UsageTotals
}

interface View {
  summary: {
    today: UsageTotals
    week: UsageTotals
    month: UsageTotals
    days: { day: string; totals: UsageTotals }[]
    projects: Group[]
    agents: Group[]
    models: Group[]
    tasks: Group[]
    chats: ChatRow[]
    estimated: boolean
    unpriced: boolean
    retentionDays: number
  }
  taskTitles: Record<string, string>
}

interface Filter {
  project: string
  task: string
  days: number
}

const RANGES = [7, 30] as const

/** Agent usage from usage.json: totals, a daily chart, breakdowns and top chats. */
export function UsagePanel() {
  const version = useUsage((s) => s.version)
  const [filter, setFilter] = useState<Filter>({ project: '', task: '', days: 30 })
  const [view, setView] = useState<View | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let live = true
    UsageAPI.Summary(models.Query.createFrom(filter))
      .then((v) => {
        if (!live) return
        setView(v as unknown as View)
        setError('')
      })
      .catch((err) => live && setError(errorMessage(err)))
    return () => {
      live = false
    }
  }, [filter, version])

  const sum = view?.summary
  const titles = view?.taskTitles ?? {}
  const taskName = (id: string) => titles[id] || `task ${id.slice(0, 7)}`
  const metric: ChartMetric = sum ? chartMetric(sum.days) : 'tokens'

  return (
    <div className="panel usage-panel">
      <div className="panel-header">
        <span className="panel-title">Usage</span>
        <span className="spacer" />
        <div className="usage-range" role="group" aria-label="Range">
          {RANGES.map((d) => (
            <button key={d} className={filter.days === d ? 'active' : ''} onClick={() => setFilter({ ...filter, days: d })}>{d}d</button>
          ))}
        </div>
        <button className="icon" title="Export the daily aggregates (with the current filter) as CSV" onClick={() => void exportCSV(filter)}>⇩</button>
      </div>
      <div className="scroll usage-body">
        {error && <div className="placeholder error">{error}</div>}
        {(filter.project || filter.task) && (
          <div className="usage-filters">
            {filter.project && (
              <span className="chip" title={filter.project}>
                {projectName(filter.project)}
                <button className="icon" aria-label="Clear project filter" onClick={() => setFilter({ ...filter, project: '' })}>✕</button>
              </span>
            )}
            {filter.task && (
              <span className="chip">
                {taskName(filter.task)}
                <button className="icon" aria-label="Clear task filter" onClick={() => setFilter({ ...filter, task: '' })}>✕</button>
              </span>
            )}
          </div>
        )}
        {sum && (
          <>
            <div className="usage-tiles">
              <Tile label="Today" t={sum.today} />
              <Tile label="7 days" t={sum.week} />
              <Tile label="30 days" t={sum.month} />
            </div>
            <Chart days={sum.days} metric={metric} />
            {(sum.estimated || sum.unpriced) && (
              <p className="muted usage-note">
                {sum.estimated && '~ marks costs estimated from your prices in Settings ▸ Usage. '}
                {sum.unpriced && 'Some agents (Codex) report no cost: their turns count as tokens only until you set a price.'}
              </p>
            )}
            {sum.month.turns === 0 && sum.days.every((d) => d.totals.turns === 0) && (
              <div className="placeholder muted">No agent usage recorded in this range. Turns of agent chats are counted here as they finish.</div>
            )}
            <Breakdown title="Projects" groups={sum.projects} name={projectName} hint={(k) => k} onPick={(project) => setFilter({ ...filter, project })} />
            <Breakdown title="Agent tasks" groups={sum.tasks} name={taskName} hint={(k) => k} onPick={(task) => setFilter({ ...filter, task })} />
            <Breakdown title="Agents" groups={sum.agents} name={(k) => k} />
            <Breakdown title="Models" groups={sum.models} name={(k) => k || 'unknown'} />
            <TopChats chats={sum.chats} taskName={taskName} />
            <p className="muted usage-note">Kept {sum.retentionDays} days on this Mac only (usage.json). Budgets and prices: Settings ▸ Usage.</p>
          </>
        )}
      </div>
    </div>
  )
}

/** A total in a narrow tile: the money spent ("~" when part is estimated), else tokens. */
function Tile({ label, t }: { label: string; t: UsageTotals }) {
  const spent = spentOf(t)
  const estimated = (t.estimatedUsd ?? 0) > 0
  return (
    <div className="usage-tile" title={totalsTooltip(t)}>
      <span className="muted">{label}</span>
      <strong>{spent > 0 ? `${estimated ? '~' : ''}${formatCost(spent)}` : formatTokens(tokensOf(t))}</strong>
      <span className="muted">{spent > 0 ? `${formatTokens(tokensOf(t))} tokens${estimated ? ' · est.' : ''}` : 'tokens'}</span>
    </div>
  )
}

const CHART_HEIGHT = 64

function Chart({ days, metric }: { days: { day: string; totals: UsageTotals }[]; metric: ChartMetric }) {
  const values = days.map((d) => metricValue(d.totals, metric))
  const heights = barHeights(values)
  const width = 100 / Math.max(1, days.length)
  const top = Math.max(0, ...values)
  return (
    <figure className="usage-chart" aria-label={`${metric === 'cost' ? 'Cost' : 'Tokens'} per day`}>
      <svg viewBox={`0 0 100 ${CHART_HEIGHT}`} preserveAspectRatio="none" role="img">
        {days.map((d, i) => {
          const h = (heights[i] / 100) * CHART_HEIGHT
          return (
            <rect
              key={d.day}
              className={i === days.length - 1 ? 'today' : ''}
              x={i * width + width * 0.15}
              y={CHART_HEIGHT - h}
              width={width * 0.7}
              height={h}
            >
              <title>{`${shortDay(d.day)}: ${totalsLabel(d.totals)}`}</title>
            </rect>
          )
        })}
      </svg>
      <figcaption className="muted">
        <span>{days.length ? shortDay(days[0].day) : ''}</span>
        <span>max {metricLabel(top, metric)}/day</span>
        <span>today</span>
      </figcaption>
    </figure>
  )
}

function Breakdown(props: { title: string; groups: Group[]; name: (k: string) => string; hint?: (k: string) => string; onPick?: (k: string) => void }) {
  const { title, groups, name, hint, onPick } = props
  if (groups.length === 0) return null
  const metric = groups.some((g) => spentOf(g.totals) > 0) ? 'cost' : 'tokens'
  const widths = barHeights(groups.map((g) => metricValue(g.totals, metric)))
  return (
    <section className="usage-section">
      <h4>{title}</h4>
      {groups.map((g, i) => (
        <div
          key={g.key}
          className={`usage-row${onPick ? ' pickable' : ''}`}
          title={`${hint ? hint(g.key) + '\n' : ''}${totalsTooltip(g.totals)}${onPick ? '\nClick to filter' : ''}`}
          onClick={onPick ? () => onPick(g.key) : undefined}
        >
          <span className="usage-name">{name(g.key)}</span>
          <span className="usage-value">{costLabel(g.totals) || `${formatTokens(tokensOf(g.totals))} tok`}</span>
          <span className="usage-bar" style={{ width: `${widths[i]}%` }} />
        </div>
      ))}
    </section>
  )
}

function TopChats({ chats, taskName }: { chats: ChatRow[]; taskName: (id: string) => string }) {
  const open = useAgentChat((s) => s.sessions)
  if (chats.length === 0) return null
  return (
    <section className="usage-section">
      <h4>Top chats</h4>
      {chats.map((c) => {
        const isOpen = !!open[c.id]
        return (
          <div
            key={c.id}
            className={`usage-row${isOpen ? ' pickable' : ''}`}
            title={`${c.label}\n${c.agent}${c.model ? ` · ${c.model}` : ''} · ${projectName(c.project)}${c.task ? ` · ${taskName(c.task)}` : ''}\n${totalsTooltip(c.totals)}${isOpen ? '\nClick to show the chat' : ''}`}
            onClick={isOpen ? () => useEditor.getState().setActive(chatTabId(c.id)) : undefined}
          >
            <span className="usage-name">{c.label || `${c.agent} chat`}</span>
            <span className="usage-value">{totalsLabel(c.totals)}</span>
          </div>
        )
      })}
    </section>
  )
}

async function exportCSV(filter: Filter): Promise<void> {
  const q = models.Query.createFrom(filter)
  try {
    const path = await UsageAPI.ExportCSV(q)
    if (path) showToast(`Saved ${path}`)
  } catch {
    // No save dialog (the browser build): download it instead.
    try {
      const csv = await UsageAPI.CSV(q)
      const a = document.createElement('a')
      a.href = URL.createObjectURL(new Blob([csv], { type: 'text/csv' }))
      a.download = 'flint-usage.csv'
      a.click()
      setTimeout(() => URL.revokeObjectURL(a.href), 1000)
    } catch (err) {
      showToast(errorMessage(err), 'error')
    }
  }
}
