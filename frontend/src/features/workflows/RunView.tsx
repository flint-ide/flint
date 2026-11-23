import { Fragment, useEffect, useState } from 'react'
import type { ContributedTab } from '../../lib/features'
import { useEditor } from '../../store/editor'
import { WorkflowDialogs } from './Dialogs'
import { StepDetail } from './StepDetail'
import { StepFlow } from './StepFlow'
import { LogMark, StateBadge } from './StepParts'
import { logKind, runProgress, stepLook, waitingNote } from './stepLook'
import {
  actionHint, AGENT_ANSWER_NOTE, capitalize, canRecheck, chatButton, DONE_NOTE, holdDetail, initialStep, logLines, orderedActions, originText,
  primaryAction, runIdOf, runStateLabel, runStatus, runTabTitle, runTone, type ActionInfo, type RunInfo,
} from './workflows'
import { loadRuns, openRunChat, openStepRun, openTask, perform, showTaskChanges, useWorkflows } from './store'
import { Alert, AlertDescription, Button, Progress, ProgressLabel, ProgressValue } from '@flint-ide/ui'
import { Check, Hand, LoaderCircle, Pause, Square, Workflow, X, type LucideIcon } from '@flint-ide/ui/icons'

/** The run tab: the one place a run is steered from. Its steps are a line of blocks; a click shows one below. */
export function RunView({ tab }: { tab: ContributedTab; active: boolean }) {
  const id = runIdOf(tab.data)
  const run = useWorkflows((s) => s.runs?.find((r) => r.id === id))
  const loaded = useWorkflows((s) => s.runs !== null)
  useEffect(() => void loadRuns(), [])
  useEffect(() => {
    if (run) useEditor.getState().retitleTab(tab.id, runTabTitle(run))
  }, [run, tab.id])
  if (!id) return <p className="muted empty">This tab's data is not valid: close it and open the run from the Workflows panel.</p>
  if (!run) {
    return <p className="muted empty">{loaded ? 'This run is not in the list any more (it was removed). Close this tab.' : 'Loading the run…'}</p>
  }
  return <Run run={run} />
}

const RUN_ICONS: Record<string, LucideIcon> = { running: LoaderCircle, waiting: Hand, failed: X, paused: Pause, done: Check, stopped: Square }

function Run({ run }: { run: RunInfo }) {
  const [picked, setPicked] = useState<number | null>(null)
  const selected = picked ?? initialStep(run)
  const step = run.steps[selected]
  return (
    <div className="wf-run">
      <RunHeader run={run} />
      <div className="wf-run-body scroll">
        <RunNotice run={run} />
        <StepFlow steps={run.steps} defaultAgent={run.agent} run={run} selected={selected} onSelect={setPicked} label="Steps of this run" />
        {step && (
          <StepDetail
            step={step}
            index={selected}
            steps={run.steps}
            defaultAgent={run.agent}
            look={stepLook(run, selected, step)}
            actions={
              <>
                {step.kind !== 'script' && step.kind !== 'gate' && <ChatButton run={run} index={selected} />}
                {step.kind === 'script' && (
                  <span title={step.runTab ? 'Show the output of this step' : 'This step has not run yet'}>
                    <Button variant="plain" size="sm" disabled={!step.runTab} onClick={() => openStepRun(step)}>Open run tab</Button>
                  </span>
                )}
              </>
            }
          />
        )}
        <div className="wf-cards">
          <TaskCard run={run} />
          <LogCard run={run} />
        </div>
      </div>
      <WorkflowDialogs host="run" runId={run.id} />
    </div>
  )
}

/**
 * The head of a run: title, state, workflow and who started it, the progress, and the actions: the ONE primary
 * action large, the others quieter, the ones that end the run apart at the end.
 */
function RunHeader({ run }: { run: RunInfo }) {
  const busy = useWorkflows((s) => !!s.busy[run.id])
  const actions = orderedActions(run.actions)
  const origin = originText(run.origin)
  const progress = runProgress(run)
  const ends = actions.filter((a) => a.tone === 'danger')
  const moves = actions.filter((a) => a.tone !== 'danger')
  const button = (a: ActionInfo) => (
    <span key={a.name} title={actionHint(a, run)}>
      <Button
        variant={a.tone === 'primary' ? 'primary' : a.tone === 'danger' ? 'ghost' : 'plain'}
        size={a.tone === 'primary' ? 'md' : 'sm'}
        className={a.tone === 'danger' ? 'wf-danger' : undefined}
        disabled={busy}
        onClick={() => void perform(run, a.name)}
      >
        {a.label}
      </Button>
    </span>
  )
  return (
    <header className="wf-head" aria-label="Workflow run">
      <div className="wf-head-row">
        <div className="wf-head-names">
          <h2 className="wf-head-title truncate" title={run.title}>{run.title || run.workflow}</h2>
          <div className="wf-head-meta">
            <StateBadge tone={runTone(run)} icon={RUN_ICONS[run.state]}>{runStateLabel(run)}</StateBadge>
            <span className="wf-head-fact" title="The workflow">
              <Workflow aria-hidden />
              {run.workflow}
            </span>
            {origin && <span className="wf-head-fact">{origin}</span>}
          </div>
        </div>
        <div className="wf-head-actions">
          {moves.map((a) => (
            <Fragment key={a.name}>
              {button(a)}
              {a.name === 'approve' && <ChangesButton run={run} variant="plain" />}
            </Fragment>
          ))}
          {ends.length > 0 && moves.length > 0 && <span className="wf-head-sep" aria-hidden />}
          {ends.map(button)}
        </div>
      </div>
      <Progress value={progress.percent} className="wf-progress" data-tone={runTone(run)} aria-label={`Progress: ${progress.text}`}>
        <ProgressLabel className="wf-progress-label" role="status">{runStatus(run)}</ProgressLabel>
        <ProgressValue className="wf-progress-value">{() => progress.text}</ProgressValue>
      </Progress>
    </header>
  )
}

/**
 * What the run needs or tells, in one place and one shape: waiting for you (what the primary action does), paused
 * by a limit (why, when it goes on), failed (the error and what to do), done (what next). Nothing while it runs:
 * the progress line says where it is.
 */
function RunNotice({ run }: { run: RunInfo }) {
  const primary = primaryAction(run.actions)
  if (run.hold) {
    const more = holdDetail(run.hold)
    return (
      <Alert variant="warning" size="small" role="status" className="wf-notice">
        <AlertDescription>
          {more && <span className="wf-reason">{more}</span>}
          <span className="wf-reason">Nothing is lost: Resume takes the step again, and this start does not count as an attempt.</span>
        </AlertDescription>
      </Alert>
    )
  }
  if (run.state === 'failed' && run.error) {
    return (
      <Alert variant="negative" size="small" className="wf-notice">
        <AlertDescription>
          <span className="wf-reason">{capitalize(run.error)}</span>
          {canRecheck(run) && (
            <>
              <span className="wf-reason">{AGENT_ANSWER_NOTE}</span>
              <span className="wf-buttons">
                <ChatButton run={run} index={run.current} />
              </span>
            </>
          )}
        </AlertDescription>
      </Alert>
    )
  }
  if (run.state === 'done') {
    return (
      <Alert variant="positive" size="small" role="status" className="wf-notice">
        <AlertDescription>
          <span className="wf-reason">{DONE_NOTE}</span>
          <span className="wf-buttons">
            <ChangesButton run={run} variant="primary" />
            <Button variant="plain" size="sm" onClick={() => void openTask(run)} title="Open the agent task: its folder and chat">Open task</Button>
          </span>
        </AlertDescription>
      </Alert>
    )
  }
  if (run.state === 'waiting' && primary) {
    return (
      <Alert variant="primary" size="small" role="status" className="wf-notice" icon={<Hand />}>
        <AlertDescription>
          <span className="wf-reason">{waitingNote(run)}</span>
        </AlertDescription>
      </Alert>
    )
  }
  return null
}

function ChangesButton({ run, variant }: { run: RunInfo; variant: 'primary' | 'plain' }) {
  return (
    <Button variant={variant} size="sm" onClick={() => void showTaskChanges(run)} title="Show the changed files, Merge and Create PR in the Agent Tasks panel">
      Show changes
    </Button>
  )
}

/** What the run was asked to do, the note its agent gets, and the way to its agent task. */
function TaskCard({ run }: { run: RunInfo }) {
  return (
    <section className="wf-card" aria-label="Task">
      <div className="wf-pane-head">Task</div>
      <p className="wf-prose">{run.description}</p>
      {run.feedback && (
        <>
          <div className="wf-pane-head">Note the agent gets</div>
          <p className="wf-prose">{run.feedback}</p>
        </>
      )}
      <div className="wf-buttons">
        <Button variant="plain" size="sm" onClick={() => void openTask(run)} title="Open the agent task: its folder and chat">Open task</Button>
        <ChangesButton run={run} variant="plain" />
      </div>
    </section>
  )
}

/** The log as a compact timeline: one mark per kind of event, the text, the time. */
function LogCard({ run }: { run: RunInfo }) {
  return (
    <section className="wf-card" aria-label="Log">
      <div className="wf-pane-head">
        Log
        <span className="fui-tree-count">{run.log.length}</span>
      </div>
      <ol className="wf-log">
        {run.log.length === 0 && <li className="muted">Nothing yet.</li>}
        {logLines(run).map((l) => (
          <li key={l.key}>
            <LogMark kind={logKind(l.text)} />
            <span className="wf-log-text">{l.text}</span>
            <span className="wf-log-time fui-mono">{l.time}</span>
          </li>
        ))}
      </ol>
    </section>
  )
}

/** Open chat of a step: its own chat, else (the first step still in the task's chat) the agent task. */
function ChatButton({ run, index }: { run: RunInfo; index: number }) {
  const chat = chatButton(run, index)
  return (
    <span title={chat.hint}>
      <Button variant="plain" size="sm" disabled={!chat.enabled} onClick={() => void openRunChat(run, index)}>Open chat</Button>
    </span>
  )
}
