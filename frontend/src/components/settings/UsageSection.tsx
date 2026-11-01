import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { DEFAULT_USAGE, formatPrices, parsePrices, USAGE_LIMITS, type UsageSettings } from '../../lib/usageSettings'
import { NumberField, Row } from './fields'

/** Settings ▸ Usage: how long usage is kept, budgets, and prices for estimates. */
export function UsageSection() {
  const u = useSettings((s) => s.settings.usage)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<UsageSettings>) => update((s) => ({ usage: { ...s.usage, ...change } }))
  const usd = USAGE_LIMITS.budgetUsd
  const tok = USAGE_LIMITS.budgetTokens
  return (
    <section>
      <h2>Usage</h2>
      <p className="muted settings-note">
        Flint counts the tokens (and the cost, when the agent reports it) of every agent turn per day, project, agent, model and agent
        task, in usage.json in its configuration folder. Nothing leaves this Mac. See the Usage panel (∑).
      </p>
      <Row label="Keep daily totals for" hint="Older days are dropped.">
        <NumberField value={u.retentionDays} {...USAGE_LIMITS.retentionDays} step={1} suffix="days" onCommit={(v) => set({ retentionDays: Math.round(v) })} />
      </Row>

      <h3>Budgets</h3>
      <p className="muted settings-note">0 turns a budget off. When one is reached, the chat shows a warning and a notification tells you once per day (or per task).</p>
      <Row label="Daily budget" hint="All agent chats of the day. Costs Codex doesn't report count only with a price below.">
        <NumberField value={u.dailyBudgetUsd} min={usd.min} max={usd.max} step={1} suffix="USD" onCommit={(v) => set({ dailyBudgetUsd: v })} />
      </Row>
      <Row label="Daily token budget" hint="Input plus output tokens; cached input only if counted below.">
        <NumberField value={u.dailyBudgetTokens} min={tok.min} max={tok.max} step={100000} suffix="tokens" onCommit={(v) => set({ dailyBudgetTokens: Math.round(v) })} />
      </Row>
      <Row label="Budget per agent task" hint="Everything one agent task's chats used, over the days kept.">
        <NumberField value={u.taskBudgetUsd} min={usd.min} max={usd.max} step={1} suffix="USD" onCommit={(v) => set({ taskBudgetUsd: v })} />
      </Row>
      <Row label="Token budget per agent task">
        <NumberField value={u.taskBudgetTokens} min={tok.min} max={tok.max} step={100000} suffix="tokens" onCommit={(v) => set({ taskBudgetTokens: Math.round(v) })} />
      </Row>
      <Row
        label="Count cached input in token budgets"
        hint="Off: token budgets leave out input read from the prompt cache, which a long Claude chat re-reads on every call and which costs a fraction of fresh input."
      >
        <input type="checkbox" checked={u.countCacheReads} onChange={(e) => set({ countCacheReads: e.target.checked })} />
      </Row>
      <Row label="Pause new turns over budget" hint="A chat over budget doesn't send new messages until you choose Continue anyway in its warning.">
        <input type="checkbox" checked={u.pauseOverBudget} onChange={(e) => set({ pauseOverBudget: e.target.checked })} />
      </Row>

      <h3>Prices</h3>
      <PricesField />
      <div className="settings-actions">
        <button disabled={JSON.stringify(u) === JSON.stringify(DEFAULT_USAGE)} onClick={() => update({ usage: DEFAULT_USAGE })}>Reset to Defaults</button>
      </div>
    </section>
  )
}

function PricesField() {
  const prices = useSettings((s) => s.settings.usage.prices)
  const update = useSettings((s) => s.update)
  const saved = formatPrices(prices)
  const [draft, setDraft] = useState(saved)
  useEffect(() => setDraft(saved), [saved])
  const { prices: parsed, invalid } = parsePrices(draft)
  const commit = () => {
    if (invalid.length === 0 && formatPrices(parsed) !== saved) update((s) => ({ usage: { ...s.usage, prices: parsed } }))
  }
  return (
    <Row
      label="Model prices"
      hint={'One "model input output [cached]" per line, in USD per million tokens, for agents that report no cost (Codex). The model name must match the Usage panel. Without a cached-input price, cached tokens are left out of the estimate. Costs computed from these are marked as estimates.'}
    >
      <div className="settings-control settings-control-stack">
        <textarea
          className={`settings-textarea${invalid.length ? ' invalid' : ''}`}
          value={draft}
          spellCheck={false}
          placeholder={'gpt-5-codex 1.25 10 0.125'}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
        />
        {invalid.length > 0 && <span className="error">Not a price line: {invalid.join(', ')}</span>}
      </div>
    </Row>
  )
}
