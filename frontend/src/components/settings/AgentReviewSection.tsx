import { useSettings } from '../../store/settings'
import { useAgent } from '../../store/agent'
import { AGENT_REVIEW_LIMITS, DEFAULT_AGENT_REVIEW, type AgentReviewSettings, type ReviewScope } from '../../lib/reviewSettings'
import { Select } from '../ui/Select'
import { NumberField, Row } from './fields'

const SCOPES: { value: ReviewScope; label: string }[] = [
  { value: 'selected', label: 'Ticked changes' },
  { value: 'all', label: 'All changes' },
]

/** Settings ▸ Agents ▸ Review: Review Changes with Agent. */
export function AgentReviewSection() {
  const review = useSettings((s) => s.settings.agentReview)
  const update = useSettings((s) => s.update)
  const available = useAgent((s) => s.available)
  const set = (change: Partial<AgentReviewSettings>) => update((s) => ({ agentReview: { ...s.agentReview, ...change } }))
  // An agent that is no longer installed still shows, so it can be changed back.
  const names = review.agent && !available.includes(review.agent) ? [...available, review.agent] : available
  const isDefault = JSON.stringify(review) === JSON.stringify(DEFAULT_AGENT_REVIEW)
  return (
    <>
      <h3>Review</h3>
      <Row label="Scope" hint="What Review Changes with Agent covers: the changes ticked in the Commit panel, or every change. A diff tab's file is always included.">
        <Select value={review.scope} options={SCOPES} onChange={(scope) => set({ scope })} />
      </Row>
      <Row label="Agent" hint="Runs read-only and headless, like commit messages. Secrets are redacted and files that hold credentials are never sent.">
        <Select
          value={review.agent}
          allowEmpty="Default agent"
          options={names.map((a) => ({ value: a, label: a, hint: available.includes(a) ? undefined : 'not installed' }))}
          onChange={(agent) => set({ agent })}
        />
      </Row>
      <Row label="Largest diff" hint="Larger diffs are shortened per file (small files whole, large ones cut at hunks or summarized); the agent can read the files itself.">
        <NumberField value={review.maxDiffKB} {...AGENT_REVIEW_LIMITS.maxDiffKB} step={8} suffix="KB" onCommit={(v) => set({ maxDiffKB: Math.round(v) })} />
      </Row>
      <Row label="Time limit" hint="A review that takes longer is stopped.">
        <NumberField value={review.timeoutSeconds} {...AGENT_REVIEW_LIMITS.timeoutSeconds} step={30} suffix="s" onCommit={(v) => set({ timeoutSeconds: Math.round(v) })} />
      </Row>
      <div className="settings-actions">
        <button disabled={isDefault} onClick={() => set(DEFAULT_AGENT_REVIEW)}>Reset to Defaults</button>
      </div>
    </>
  )
}
