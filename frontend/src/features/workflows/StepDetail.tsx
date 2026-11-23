import type { ReactNode } from 'react'
import { FileText, SquareTerminal } from '@flint-ide/ui/icons'
import { KindLabel, KindTile, LookBadge, ReworkNote } from './StepParts'
import type { StepLook } from './stepLook'
import { attemptsText, noTextNote, onFailHint, outputLabel, stepWho, type StepInfo } from './workflows'

interface Props {
  step: StepInfo
  index: number
  steps: readonly StepInfo[]
  defaultAgent: string
  /** The step's state in a run; omitted in the diagram of a definition. */
  look?: StepLook
  /** Buttons at the end of the head (Open chat, Open run tab). */
  actions?: ReactNode
}

/**
 * The step picked in the line, as a panel: its tile, title, kind and who takes it, its state; then what it is
 * told (prose) next to what it said or printed last (monospace). The diagram view and the run tab share it.
 */
export function StepDetail({ step, index, steps, defaultAgent, look, actions }: Props) {
  const fail = onFailHint(step, steps)
  const attempts = attemptsText(step)
  const said = outputLabel(step)
  const who = step.kind === 'script' && step.command ? step.command : stepWho(step, defaultAgent)
  return (
    <section className="wf-detail" aria-label={`Step ${index + 1}: ${step.title}`} data-status={look?.status ?? 'none'}>
      <header className="wf-detail-head">
        <KindTile kind={step.kind} size={36} />
        <div className="wf-detail-names">
          <h3 className="wf-detail-title">
            <span className="wf-detail-no">Step {index + 1}</span>
            <span>{step.title}</span>
          </h3>
          <div className="wf-detail-sub">
            <KindLabel kind={step.kind} />
            <span className={step.kind === 'script' && step.command ? 'fui-mono' : undefined}>{who}</span>
            {step.takenBy && step.agent && <span title="This agent reached its usage limit">instead of {step.agent}</span>}
            {attempts && <span>{attempts}</span>}
            <ReworkNote text={fail} />
          </div>
        </div>
        {look && <LookBadge look={look} />}
        {actions && <div className="wf-detail-actions">{actions}</div>}
      </header>
      <div className="wf-detail-grid">
        <div className="wf-pane">
          <div className="wf-pane-head">
            <FileText aria-hidden />
            Instructions
          </div>
          {step.text ? <p className="wf-prose">{step.text}</p> : <p className="wf-prose muted">{noTextNote(step.kind)}</p>}
        </div>
        {said && (
          <div className="wf-pane">
            <div className="wf-pane-head">
              <SquareTerminal aria-hidden />
              {said}
            </div>
            <pre className="wf-output fui-mono">{step.output}</pre>
          </div>
        )}
      </div>
    </section>
  )
}
