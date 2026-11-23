import { useEffect, useState } from 'react'
import { Alert, Button } from '@flint-ide/ui'
import { FileText, Play } from '@flint-ide/ui/icons'
import { MarkerBadge } from '../../components/tasks/badges'
import type { ContributedTab } from '../../lib/features'
import { KindSummary } from './KindSummary'
import { ScriptNotice } from './ScriptNotice'
import { StepDetail } from './StepDetail'
import { StepFlow } from './StepFlow'
import { loadList, openRunDialog, openWorkflowFile, useWorkflows } from './store'
import { clampStep, usable, workflowHeader, workflowViewData, workflowViewState, type WorkflowInfo } from './workflows'

/** The workflow tab: one workflow of a folder as a diagram (read only), the text of the picked step below it. */
export function WorkflowView({ tab }: { tab: ContributedTab; active: boolean }) {
  const data = workflowViewData(tab.data)
  const listed = useWorkflows((s) => (data ? s.lists[data.dir] : undefined))
  useEffect(() => {
    if (data) void loadList(data.dir)
  }, [data?.dir]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!data) return <p className="muted empty">This tab's data is not valid: close it and open the workflow from the Workflows panel.</p>
  const state = workflowViewState(listed?.listing ?? null, listed?.error ?? '', data.name)
  if (state.kind === 'loading') return <p className="muted empty">Loading the workflow…</p>
  if (state.kind === 'error') return <p className="muted empty" role="alert">{state.text}</p>
  if (state.kind === 'missing') return <p className="muted empty" role="status">{state.text}</p>
  return <Diagram dir={data.dir} workflow={state.workflow} problem={state.problem} sandbox={listed?.listing?.sandbox ?? ''} allowed={listed?.listing?.unsandboxedOK ?? false} />
}

function Diagram({ dir, workflow, problem, sandbox, allowed }: { dir: string; workflow: WorkflowInfo; problem: string; sandbox: string; allowed: boolean }) {
  const [picked, setPicked] = useState<number | null>(null)
  const selected = clampStep(picked, workflow.steps.length)
  const head = workflowHeader(workflow)
  const step = selected === null ? undefined : workflow.steps[selected]
  const ok = usable(workflow)
  return (
    <div className="wf-run">
      <header className="wf-head" aria-label="Workflow">
        <div className="wf-head-row">
          <div className="wf-head-names">
            <h2 className="wf-head-title truncate" title={head.name}>{head.name}</h2>
            <div className="wf-head-meta">
              <MarkerBadge tone={workflow.problem ? 'error' : undefined}>{head.source}</MarkerBadge>
              <span className="wf-head-fact">{head.steps}</span>
              <KindSummary steps={workflow.steps} />
            </div>
          </div>
          <div className="wf-head-actions">
            <span title={ok ? `Run ${workflow.name}: you say what to do, then pick the agent` : `Can't run: ${problem}`}>
              <Button variant="primary" size="md" disabled={!ok} onClick={() => openRunDialog(workflow.name, dir)}>
                <Play aria-hidden />
                Run…
              </Button>
            </span>
            {workflow.path && (
              <Button variant="plain" size="sm" onClick={() => openWorkflowFile(workflow.path)} title={`Open ${workflow.path}`}>
                <FileText aria-hidden />
                Open file
              </Button>
            )}
          </div>
        </div>
        {head.description && <p className="wf-head-description">{head.description}</p>}
      </header>
      <div className="wf-run-body scroll">
        {problem && <Alert variant="negative" size="small" role="alert" className="wf-notice">{problem}</Alert>}
        <StepFlow steps={workflow.steps} defaultAgent="" selected={selected ?? -1} onSelect={setPicked} label={`Steps of ${workflow.name}`} />
        {step && selected !== null ? (
          <StepDetail step={step} index={selected} steps={workflow.steps} defaultAgent="" />
        ) : (
          workflow.steps.length > 0 && <p className="wf-hint muted">Choose a step to read what it is told.</p>
        )}
        <ScriptNotice steps={workflow.steps} sandbox={sandbox} allowed={allowed} dir={dir} />
      </div>
    </div>
  )
}
