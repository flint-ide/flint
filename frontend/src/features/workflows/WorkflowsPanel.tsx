import { useEffect, useState } from 'react'
import { Dropdown } from '../../components/ui/Dropdown'
import { MarkerBadge } from '../../components/tasks/badges'
import { runCommandById } from '../../lib/commands'
import { isFeatureActive } from '../../lib/activeFeatures'
import { activeRoot } from '../../store/activeRoot'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { WorkflowDialogs } from './Dialogs'
import { Schedules } from './Schedules'
import { KindSummary } from './KindSummary'
import { StateBadge } from './StepParts'
import { runProgress, stepLook } from './stepLook'
import {
  groupRuns, originText, restrictedNote, runStateLabel, runStatus, runTone, runTooltip, sourceLabel, stepCount, usable, primaryAction, actionHint,
  type RunInfo, type WorkflowInfo,
} from './workflows'
import {
  loadList, loadRuns, openCoordinateDialog, openNewDialog, openRun, openRunDialog, openWorkflowFile, openWorkflowView, perform, trustWorkflowFolder, useWorkflows, type Listed,
} from './store'
import { Button, IconButton, PanelHeader, TaskCard, TreeRow, TreeSection } from '@flint-ide/ui'
import { Ellipsis, Plus, RefreshCw } from '@flint-ide/ui/icons'

const basename = (p: string) => p.replace(/\/+$/, '').slice(p.replace(/\/+$/, '').lastIndexOf('/') + 1)

/** The Workflows panel: the runs (the ones that need you first) and the workflows of the active folder. */
export function WorkflowsPanel() {
  const { runs, error, lists } = useWorkflows()
  // The folder follows the active tab: read it again when the tab or the folders change.
  useWorkspace((s) => s.roots)
  useEditor((s) => s.activeId)
  const dir = activeRoot()
  useEffect(() => {
    void loadRuns()
    void loadList(dir)
  }, [dir])
  const listed = dir ? lists[dir] : undefined
  const board = isFeatureActive('projects')
  const refresh = () => {
    void loadRuns()
    void loadList(dir)
  }
  return (
    <div className="panel workflows-panel">
      <PanelHeader
        title="Workflows"
        actions={
          <>
            <IconButton variant="ghost" size="sm" aria-label="Run Workflow" title="Run Workflow…" onClick={() => openRunDialog()}>
              <Plus />
            </IconButton>
            <IconButton variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onClick={refresh}>
              <RefreshCw />
            </IconButton>
            <Dropdown
              align="end"
              minWidth={190}
              items={[
                { label: 'Run Workflow…', onSelect: () => openRunDialog() },
                { label: 'New Workflow…', onSelect: openNewDialog },
                { label: 'Coordinate with Agent…', onSelect: openCoordinateDialog },
                ...(board ? [{ type: 'separator' as const }, { label: 'Open project board', onSelect: () => void runCommandById('projects.board') }] : []),
              ]}
              trigger={(t) => (
                <IconButton variant="ghost" size="sm" {...t} aria-label="Workflow actions" title="Run or write a workflow">
                  <Ellipsis />
                </IconButton>
              )}
            />
          </>
        }
      />
      <div className="tree scroll">
        {error && <div className="placeholder error">{error}</div>}
        <Runs runs={runs} />
        <Available dir={dir} listed={listed} />
        <Schedules />
      </div>
      <WorkflowDialogs host="panel" />
    </div>
  )
}

function Runs({ runs }: { runs: RunInfo[] | null }) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({ finished: true })
  if (runs === null) return null
  if (runs.length === 0) {
    return (
      <div className="placeholder muted">
        <span>
          A workflow is a repeatable way to deliver a change: steps that agents, scripts and you take in order, the same way every time. Nothing has run yet.
        </span>
        <span>
          <Button variant="link" size="sm" onClick={() => openRunDialog()}>Run Workflow…</Button>
        </span>
      </div>
    )
  }
  return (
    <>
      {groupRuns(runs).map((g) => (
        <TreeSection
          key={g.id}
          title={g.title}
          count={g.runs.length}
          open={!collapsed[g.id]}
          onOpenChange={(open) => setCollapsed((c) => ({ ...c, [g.id]: !open }))}
        >
          {g.runs.map((r) => <RunRow key={r.id} run={r} />)}
        </TreeSection>
      ))}
    </>
  )
}

/**
 * A run in the panel, as a kit TaskCard like every agent panel's item: the state badge, the title, the step segments
 * and the workflow, the status sentence; the whole card opens the run's tab. Its one action that waits for a person
 * (Approve, Check again, Resume, Retry) sits in the card's actions bar, always shown, in the accent's text colour.
 */
function RunRow({ run }: { run: RunInfo }) {
  const busy = useWorkflows((s) => !!s.busy[run.id])
  const primary = primaryAction(run.actions)
  return (
    <TaskCard
      tooltip={runTooltip(run)}
      status={<StateBadge tone={runTone(run)}>{runStateLabel(run)}</StateBadge>}
      label={run.title || run.workflow}
      meta={[<StepDots key="d" run={run} />, run.workflow, originText(run.origin) || false]}
      notes={[{ text: runStatus(run), truncate: true, title: runStatus(run) }]}
      onOpen={() => openRun(run.id)}
      actions={
        primary && (
          <Button variant="ghost" size="sm" data-primary title={actionHint(primary, run)} disabled={busy} onClick={() => void perform(run, primary.name)}>
            {primary.label}
          </Button>
        )
      }
    />
  )
}

/** A run's progress in a row: one segment per step in the colour of its state, and "2 of 5 steps done". */
function StepDots({ run }: { run: RunInfo }) {
  const progress = runProgress(run)
  return (
    <span className="wf-dots" role="img" aria-label={progress.text} title={progress.text}>
      <span className="wf-dots-line">
        {run.steps.map((s, i) => (
          <span key={`${s.id}:${i}`} className="wf-dot" data-status={stepLook(run, i, s).status} />
        ))}
      </span>
      <span className="wf-dots-text">{`${progress.done}/${progress.total}`}</span>
    </span>
  )
}

function Available({ dir, listed }: { dir: string; listed: Listed | undefined }) {
  if (!dir) return <div className="placeholder muted">Open a folder to see the workflows you can run in it.</div>
  const listing = listed?.listing
  return (
    <div className="panel-section-divided">
      <TreeRow emphasis="group" label={`Workflows in ${basename(dir)}`} badge={listing?.workflows.length} />
      {listed?.error && <div className="placeholder error">{listed.error}</div>}
      {listing?.restricted && <Restricted dir={dir} hidden={listing.hidden} />}
      {listing?.workflows.map((w) => <WorkflowRow key={w.name} dir={dir} workflow={w} />)}
      {listing && listing.workflows.length === 0 && !listing.restricted && (
        <div className="placeholder muted">
          <span>This folder has no workflow of its own yet, and none is built in. A workflow is one Markdown file in .flint/workflows.</span>
          <span>
            <Button variant="link" size="sm" onClick={openNewDialog}>New Workflow…</Button>
          </span>
        </div>
      )}
      {listing && listing.problems.length > 0 && (
        <>
          <TreeRow emphasis="group" label="Problems" badge={listing.problems.length} />
          {listing.problems.map((p, i) => <ProblemRow key={`${p}:${i}`} text={p} />)}
        </>
      )}
    </div>
  )
}

function Restricted({ dir, hidden }: { dir: string; hidden: number }) {
  return (
    <div className="restricted-note" role="status">
      <MarkerBadge tone="warn">Restricted Mode</MarkerBadge>
      <span className="muted">{restrictedNote(hidden)}</span>
      <Button variant="ghost" size="sm" onClick={() => void trustWorkflowFolder(dir)} title={dir}>Trust Folder…</Button>
    </div>
  )
}

/** A workflow in the panel, as a kit TaskCard: name, kind tiles, steps and source, description; Run…, View and Open in its actions bar. */
function WorkflowRow({ dir, workflow }: { dir: string; workflow: WorkflowInfo }) {
  const ok = usable(workflow)
  return (
    <TaskCard
      tooltip={workflow.path || 'Built in'}
      label={workflow.name}
      onOpen={() => openWorkflowView(dir, workflow.name)}
      badges={workflow.problem ? <MarkerBadge tone="error">{sourceLabel(workflow.source)}</MarkerBadge> : undefined}
      meta={ok ? [<KindSummary key="k" steps={workflow.steps} />, stepCount(workflow.steps.length), sourceLabel(workflow.source)] : [sourceLabel(workflow.source)]}
      notes={[
        !!workflow.description && { text: workflow.description, truncate: true, title: workflow.description },
        !!workflow.problem && { text: workflow.problem, tone: 'danger' },
      ]}
      actions={
        <>
          <span title={ok ? `Run ${workflow.name}: you say what to do, then pick the agent` : `Can't run: ${workflow.problem || 'it has no steps'}`}>
            <Button variant="ghost" size="sm" aria-label={`Run ${workflow.name}…`} disabled={!ok} onClick={() => openRunDialog(workflow.name, dir)}>
              Run…
            </Button>
          </span>
          <Button variant="ghost" size="sm" aria-label={`View ${workflow.name}`} title={`Show ${workflow.name} as a diagram`} onClick={() => openWorkflowView(dir, workflow.name)}>
            View
          </Button>
          {workflow.path && (
            <Button variant="ghost" size="sm" aria-label={`Open ${workflow.name}`} title={`Open ${workflow.path}`} onClick={() => openWorkflowFile(workflow.path)}>
              Open
            </Button>
          )}
        </>
      }
    />
  )
}

/** "name.md: reason" as a file and its reason. */
function ProblemRow({ text }: { text: string }) {
  const at = text.indexOf(': ')
  const [file, reason] = at > 0 ? [text.slice(0, at), text.slice(at + 2)] : ['', text]
  return <TaskCard label={file ? <span className="fui-mono" title={file}>{file}</span> : 'A workflow file'} notes={[{ text: reason, tone: 'danger' }]} />
}
