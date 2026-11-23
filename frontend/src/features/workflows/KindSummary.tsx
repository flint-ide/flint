import { KindTile } from './StepParts'
import { stepKind, type StepInfo } from './workflows'

/** The most tiles a summary draws; the rest are counted ("+3"). */
const MAX_TILES = 10

/**
 * The shape of a workflow at a glance: one small kind tile per step, in order (the same tiles as the blocks of the
 * diagram). One image for assistive tech, named by the kinds in order.
 */
export function KindSummary({ steps }: { steps: readonly Pick<StepInfo, 'id' | 'title' | 'kind'>[] }) {
  if (steps.length === 0) return null
  const shown = steps.length > MAX_TILES ? steps.slice(0, MAX_TILES - 1) : steps
  const rest = steps.length - shown.length
  const name = steps.map((s) => stepKind(s.kind).label).join(', ')
  return (
    <span className="wf-kinds" role="img" aria-label={`Steps: ${name}`} title={steps.map((s, i) => `${i + 1}. ${s.title} (${stepKind(s.kind).label})`).join('\n')}>
      {shown.map((s, i) => (
        <KindTile key={`${s.id}:${i}`} kind={s.kind} size={24} />
      ))}
      {rest > 0 && <span className="wf-kinds-more">+{rest}</span>}
    </span>
  )
}
