import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react'
import { IconButton } from '@flint-ide/ui'
import { ChevronLeft, ChevronRight } from '@flint-ide/ui/icons'
import { centredScroll, flowLayout, LINK_WIDTH, NODE_MAX, NODE_MIN, scrollEdges, stepForKey } from './flowLayout'
import { KindTile, ReworkNote, StatusMark } from './StepParts'
import { linkState, stepLook, type StepLook } from './stepLook'
import { attemptsText, onFailHint, stepKind, stepWho, type RunInfo, type StepInfo } from './workflows'

interface Props {
  steps: readonly StepInfo[]
  /** The agent that takes steps naming none: the run's, or '' in a preview. */
  defaultAgent: string
  /** The run whose steps these are (state, current step); omitted for a workflow's definition (a preview). */
  run?: Pick<RunInfo, 'state' | 'current' | 'queued' | 'hold'>
  /** The step shown below the line; with onSelect the blocks are buttons. */
  selected?: number
  onSelect?: (index: number) => void
  label: string
}

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
const scrollBehavior = (): ScrollBehavior => (reducedMotion() ? 'auto' : 'smooth')

/** The width of the element, kept up to date while it resizes (0 until it is measured). */
function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.clientWidth)
    const observer = new ResizeObserver(() => setWidth(el.clientWidth))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  return { ref, width }
}

/**
 * The steps of a workflow as ONE line of blocks joined by connectors (flowLayout.ts has the rules): the blocks
 * shrink to a minimum, then the line scrolls sideways with fades and previous/next buttons at the hidden ends;
 * only without room for two blocks does it become a vertical stepper. One component for the definition (read
 * only: the diagram view and the Run Workflow dialog) and for a run (state, attempts, selection), so a workflow
 * looks the same before and during a run.
 */
export function StepFlow({ steps, defaultAgent, run, selected = -1, onSelect, label }: Props) {
  const { ref: frame, width } = useWidth<HTMLDivElement>()
  const line = useRef<HTMLOListElement>(null)
  const { vertical, overflow } = flowLayout(width, steps.length)
  const [edges, setEdges] = useState({ before: false, after: false })
  const looks = steps.map((s, i) => stepLook(run ?? null, i, s))
  const current = run ? run.current : -1
  const focusable = selected >= 0 ? selected : Math.max(0, Math.min(current, steps.length - 1))

  const measure = useCallback(() => {
    const el = line.current
    if (!el) return
    const next = vertical ? { before: false, after: false } : scrollEdges(el.scrollLeft, el.clientWidth, el.scrollWidth)
    setEdges((e) => (e.before === next.before && e.after === next.after ? e : next))
  }, [vertical])
  useLayoutEffect(measure, [measure, width, steps.length])

  // The step the run is at comes into view when the tab opens and whenever the run moves on.
  useEffect(() => {
    const el = line.current
    const at = current >= 0 ? current : selected
    const node = el?.querySelector<HTMLElement>(`[data-index="${at}"]`)
    if (!el || !node || vertical) return
    const item = node.parentElement ?? node
    el.scrollTo({ left: centredScroll(item.offsetLeft, node.offsetWidth, el.clientWidth, el.scrollWidth), behavior: 'auto' })
    measure()
  }, [current, run?.state, vertical, overflow, steps.length]) // eslint-disable-line react-hooks/exhaustive-deps

  const page = (dir: 1 | -1) => line.current?.scrollBy({ left: dir * Math.max(NODE_MIN, line.current.clientWidth * 0.8), behavior: scrollBehavior() })

  const onKeyDown = (e: KeyboardEvent<HTMLOListElement>) => {
    const from = Number((e.target as HTMLElement).dataset.index)
    if (!onSelect || Number.isNaN(from)) return
    const next = stepForKey(e.key, from, steps.length)
    if (next === null) return
    e.preventDefault()
    onSelect(next)
    const node = line.current?.querySelector<HTMLElement>(`[data-index="${next}"]`)
    node?.focus({ preventScroll: true })
    node?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: scrollBehavior() })
  }

  return (
    <div
      ref={frame}
      className="wf-flow"
      data-layout={vertical ? 'vertical' : 'row'}
      data-before={edges.before || undefined}
      data-after={edges.after || undefined}
      style={{ '--wf-node-min': `${NODE_MIN}px`, '--wf-node-max': `${NODE_MAX}px`, '--wf-link': `${LINK_WIDTH}px` } as CSSProperties}
    >
      <ol
        ref={line}
        className="wf-flow-line"
        aria-label={label}
        onScroll={measure}
        onKeyDown={onKeyDown}
        // A read-only line that scrolls is reachable by keyboard itself (its blocks are not buttons).
        tabIndex={!onSelect && overflow && !vertical ? 0 : undefined}
      >
        {steps.map((step, i) => (
          <li key={`${step.id}:${i}`} className="wf-flow-item">
            <StepNode
              step={step}
              index={i}
              look={looks[i]}
              steps={steps}
              defaultAgent={defaultAgent}
              current={i === current}
              selected={!!onSelect && i === selected}
              tabIndex={i === focusable ? 0 : -1}
              onSelect={onSelect}
            />
            {i < steps.length - 1 && <span className="wf-link" data-state={linkState(looks, i)} aria-hidden />}
          </li>
        ))}
      </ol>
      {!vertical && edges.before && (
        <IconButton className="wf-flow-nav" data-side="before" variant="plain" size="sm" shape="circle" tabIndex={-1} aria-label="Show earlier steps" title="Earlier steps" onClick={() => page(-1)}>
          <ChevronLeft />
        </IconButton>
      )}
      {!vertical && edges.after && (
        <IconButton className="wf-flow-nav" data-side="after" variant="plain" size="sm" shape="circle" tabIndex={-1} aria-label="Show later steps" title="Later steps" onClick={() => page(1)}>
          <ChevronRight />
        </IconButton>
      )}
    </div>
  )
}

interface NodeProps {
  step: StepInfo
  index: number
  look: StepLook
  steps: readonly StepInfo[]
  defaultAgent: string
  current: boolean
  selected: boolean
  tabIndex: number
  onSelect?: (index: number) => void
}

/**
 * One step as a block: the kind's tile, the title (strongest), the kind's name and who or what takes it, the state
 * mark, and a foot with the state in words, the attempt and where a failure sends the work back.
 */
function StepNode({ step, index, look, steps, defaultAgent, current, selected, tabIndex, onSelect }: NodeProps) {
  const kind = stepKind(step.kind)
  const command = step.kind === 'script' && step.command ? step.command : ''
  const who = command || stepWho(step, defaultAgent)
  const attempts = attemptsText(step)
  const fail = onFailHint(step, steps)
  const foot = !!look.label || !!attempts || !!fail
  const body = (
    <>
      <span className="wf-node-head">
        <KindTile kind={step.kind} />
        <span className="wf-node-names">
          <span className="wf-node-title">
            <span className="wf-node-no">{index + 1}</span>
            <span className="truncate" title={step.title}>{step.title}</span>
          </span>
          <span className="wf-node-sub">
            <span className="wf-node-kind" data-kind={step.kind}>{kind.label}</span>
            <span className={`wf-node-who truncate${command ? ' fui-mono' : ''}`} title={who}>{who}</span>
          </span>
        </span>
        <StatusMark status={look.status} />
      </span>
      {foot && (
        <span className="wf-node-foot">
          {look.label && <span className="wf-node-state" data-status={look.status}>{look.label}</span>}
          {attempts && <span className="wf-node-attempt">{attempts}</span>}
          <ReworkNote text={fail} />
        </span>
      )}
    </>
  )
  const common = {
    className: 'wf-node',
    'data-index': index,
    'data-status': look.status,
    'data-current': current || undefined,
    'data-selected': selected || undefined,
    'aria-current': current ? ('step' as const) : undefined,
  }
  if (!onSelect) return <div {...common}>{body}</div>
  return (
    <button {...common} type="button" tabIndex={tabIndex} aria-pressed={selected} title={`Show step ${index + 1}: ${step.title}`} onClick={() => onSelect(index)}>
      {body}
    </button>
  )
}
