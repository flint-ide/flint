import { useSettings } from '../../store/settings'
import { DEFAULT_REFRESH, REFRESH_LIMITS, type RefreshKey } from '../../lib/toolSettings'
import { NumberField, Row } from './fields'

const ROWS: { key: RefreshKey; label: string; hint: string }[] = [
  { key: 'kubernetesSeconds', label: 'Kubernetes panel', hint: 'How often workloads, pods, services and jobs are re-read while the panel is shown.' },
  { key: 'awsSeconds', label: 'AWS panel', hint: 'How often profiles and credential expiry are re-read while the panel is shown.' },
]

export function RefreshSection() {
  const refresh = useSettings((s) => s.settings.refresh)
  const update = useSettings((s) => s.update)
  const isDefault = ROWS.every((r) => refresh[r.key] === DEFAULT_REFRESH[r.key])
  return (
    <section>
      <h2>Refresh</h2>
      {ROWS.map((r) => (
        <Row key={r.key} label={r.label} hint={r.hint}>
          <NumberField
            value={refresh[r.key]}
            min={REFRESH_LIMITS[r.key].min}
            max={REFRESH_LIMITS[r.key].max}
            step={1}
            suffix="s"
            onCommit={(v) => update((s) => ({ refresh: { ...s.refresh, [r.key]: Math.round(v) } }))}
          />
        </Row>
      ))}
      <div className="settings-actions">
        <button disabled={isDefault} onClick={() => update({ refresh: DEFAULT_REFRESH })}>Reset to Defaults</button>
      </div>
      <p className="muted settings-note">
        Git status, Docker and pull requests don't poll: they follow change events, and pull requests reload when the window regains focus.
      </p>
    </section>
  )
}
