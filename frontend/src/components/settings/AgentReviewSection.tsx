import { useSettings } from '../../store/settings'
import { useAgent } from '../../store/agent'
import { AGENT_REVIEW_LIMITS, DEFAULT_AGENT_REVIEW, autoApproveArgs, type AgentReviewSettings, type ReviewScope } from '../../lib/reviewSettings'
import { Select } from '../ui/Select'
import { NumberField, Row } from './fields'

const SCOPES: { value: ReviewScope; label: string }[] = [
  { value: 'selected', label: 'Ticked changes' },
  { value: 'all', label: 'All changes' },
]

/** Settings ▸ Agents ▸ Review: Review Changes with Agent. */
export function AgentReviewSection() {
  const review = useSettings((s) => s.settings.agentReview)
  const customAgents = useSettings((s) => s.settings.customAgents)
  const update = useSettings((s) => s.update)
  const available = useAgent((s) => s.available)
  const preferred = useAgent((s) => s.preferred)
  const set = (change: Partial<AgentReviewSettings>) => update((s) => ({ agentReview: { ...s.agentReview, ...change } }))
  // An agent that is no longer installed still shows, so it can be changed back.
  const names = review.agent && !available.includes(review.agent) ? [...available, review.agent] : available
  const isDefault = JSON.stringify(review) === JSON.stringify(DEFAULT_AGENT_REVIEW)
  const reviewer = review.agent || preferred
  const custom = customAgents.find((c) => c.id === reviewer)
  const autoApprove = custom ? autoApproveArgs(custom.args) : []
  return (
    <>
      <h3>Review</h3>
      <Row label="Scope" hint="What Review Changes with Agent covers: the changes ticked in the Commit panel, or every change. A diff tab's file is always included.">
        <Select value={review.scope} options={SCOPES} onChange={(scope) => set({ scope })} />
      </Row>
      <Row label="Agent" hint="Runs once, headless, like commit messages. Secrets in the diff are redacted and files that hold credentials are not sent.">
        <Select
          value={review.agent}
          allowEmpty="Default agent"
          options={names.map((a) => ({ value: a, label: a, hint: available.includes(a) ? undefined : 'not installed' }))}
          onChange={(agent) => set({ agent })}
        />
      </Row>
      {autoApprove.length > 0 && (
        <p className="settings-note review-warning">
          {reviewer} is started with {autoApprove.join(' ')}: it runs its own tools without asking, so reviews with it are not read-only. Remove
          the flag in Custom agents above, or pick another agent.
        </p>
      )}
      <Row label="Largest diff" hint="Larger diffs are shortened per file (small files whole, large ones cut at hunks or summarized); the agent can read the files itself.">
        <NumberField value={review.maxDiffKB} {...AGENT_REVIEW_LIMITS.maxDiffKB} step={8} suffix="KB" onCommit={(v) => set({ maxDiffKB: Math.round(v) })} />
      </Row>
      <Row label="Time limit" hint="A review that takes longer is stopped.">
        <NumberField value={review.timeoutSeconds} {...AGENT_REVIEW_LIMITS.timeoutSeconds} step={30} suffix="s" onCommit={(v) => set({ timeoutSeconds: Math.round(v) })} />
      </Row>
      <p className="muted settings-note">
        What the agent can read on its own: Claude Code gets only Read, Grep and Glob and may not read credential files (.env, keys, cloud
        credentials). Codex runs in its read-only sandbox, which can still read any file you can. A custom agent's own tools are outside
        Flint's control: Flint declines its permission requests, but an agent configured to skip them runs its tools anyway.
      </p>
      <div className="settings-actions">
        <button disabled={isDefault} onClick={() => set(DEFAULT_AGENT_REVIEW)}>Reset to Defaults</button>
      </div>
    </>
  )
}
