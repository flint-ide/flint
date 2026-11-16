import { useSettings } from '../../store/settings'
import { CI_LIMITS, DEFAULT_CI, type CISettings } from '../../lib/ci'
import { DEFAULT_PR_REVIEW, PR_REVIEW_LIMITS, type PRReviewSettings } from '../../lib/prAgent'
import { NumberField, Row } from './fields'

const NUMBERS: { key: keyof typeof CI_LIMITS; label: string; hint: string; suffix: string }[] = [
  {
    key: 'pollSeconds',
    label: 'Check again every',
    hint: "How often your pull request's checks are re-read while some are still running (longer while nothing changes). Nothing is read while no check runs.",
    suffix: 's',
  },
  { key: 'logTailLines', label: 'Log lines from the end', hint: "Lines from the end of the failed step that Fix with agent puts in the agent's draft.", suffix: 'lines' },
  {
    key: 'logErrorLines',
    label: 'Error lines before them',
    hint: 'Test failures, compiler errors and file:line references picked from the rest of the failed step.',
    suffix: 'lines',
  },
]

const REVIEW_NUMBERS: { key: keyof typeof PR_REVIEW_LIMITS; label: string; hint: string; suffix: string }[] = [
  {
    key: 'maxThreads',
    label: 'Review threads per draft',
    hint: 'Unresolved threads one Address review comments draft carries; the rest are listed at its end for the next round.',
    suffix: 'threads',
  },
  {
    key: 'contextLines',
    label: 'Code around a comment',
    hint: "Lines before and after a commented line that go into the draft: from the working tree when the pull request's branch is checked out, else from its diff. 0 leaves the code out.",
    suffix: 'lines',
  },
]

/** Settings ▸ CI: following your pull requests' checks, and handing their review comments to the agent. */
export function CISection() {
  const ci = useSettings((s) => s.settings.ci)
  const notifyInBackground = useSettings((s) => s.settings.notifyInBackground)
  const update = useSettings((s) => s.update)
  const prReview = useSettings((s) => s.settings.prReview)
  const set = (change: Partial<CISettings>) => update((s) => ({ ci: { ...s.ci, ...change } }))
  const setReview = (change: Partial<PRReviewSettings>) => update((s) => ({ prReview: { ...s.prReview, ...change } }))
  const isDefault =
    (Object.keys(DEFAULT_CI) as (keyof CISettings)[]).every((k) => ci[k] === DEFAULT_CI[k]) &&
    (Object.keys(DEFAULT_PR_REVIEW) as (keyof PRReviewSettings)[]).every((k) => prReview[k] === DEFAULT_PR_REVIEW[k])
  return (
    <section>
      <h2>CI</h2>
      <p className="muted settings-note">
        Flint follows the checks of your current branch's pull request and of agent tasks' pull requests through GitHub's API (Settings ▸ Accounts); polls are conditional requests, which cost no rate limit while nothing changes. A failing check gets a
        red CI badge in the status bar and Fix with agent, which puts the failed step's log (redacted) in the agent chat for you to review.
      </p>
      {NUMBERS.map((n) => (
        <Row key={n.key} label={n.label} hint={n.hint}>
          <NumberField
            value={ci[n.key]}
            min={CI_LIMITS[n.key].min}
            max={CI_LIMITS[n.key].max}
            step={1}
            suffix={n.suffix}
            onCommit={(v) => set({ [n.key]: Math.round(v) })}
          />
        </Row>
      ))}
      <Row
        label="Tell me when CI fails or passes"
        hint={`A macOS notification when your pull request's CI fails or turns green while Flint isn't the active app.${notifyInBackground ? '' : ' Needs "Tell me when work finishes in the background" (Settings ▸ Agents), which is off.'}`}
      >
        <input type="checkbox" checked={ci.notify} onChange={(e) => set({ notify: e.target.checked })} />
      </Row>
      <h3>Review comments</h3>
      <p className="muted settings-note">
        Ask agent on a review thread and Address review comments on a pull request put the reviewers' comments (quoted and redacted) and the code they are about in the agent chat for you to review.
      </p>
      {REVIEW_NUMBERS.map((n) => (
        <Row key={n.key} label={n.label} hint={n.hint}>
          <NumberField
            value={prReview[n.key]}
            min={PR_REVIEW_LIMITS[n.key].min}
            max={PR_REVIEW_LIMITS[n.key].max}
            step={1}
            suffix={n.suffix}
            onCommit={(v) => setReview({ [n.key]: Math.round(v) })}
          />
        </Row>
      ))}
      <div className="settings-actions">
        <button disabled={isDefault} onClick={() => update({ ci: DEFAULT_CI, prReview: DEFAULT_PR_REVIEW })}>Reset to Defaults</button>
      </div>
    </section>
  )
}
