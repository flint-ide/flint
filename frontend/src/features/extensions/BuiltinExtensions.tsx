import { FEATURES } from '..'
import { useActiveFeatures } from '../../lib/activeFeatures'
import { coerceToggles, featureOn, pendingRestart, withFeature } from '../../lib/featureToggles'
import { useSettings } from '../../store/settings'

/**
 * Settings ▸ Extensions ▸ Built-in: the optional features (Database)
 * switched on or off. Go builds a feature only at launch, so a switch applies after a
 * restart; until then the card says so.
 */
export function BuiltinExtensions() {
  const toggles = coerceToggles(useSettings((s) => s.settings.features))
  const update = useSettings((s) => s.update)
  const active = useActiveFeatures((s) => s.ids)
  const optional = FEATURES.filter((m) => m.optional)
  const pending = new Set(pendingRestart(FEATURES, active, toggles))

  return (
    <>
      <h3>Built-in</h3>
      <p className="muted settings-note">
        Part of Flint and switched on by default. A switched-off extension isn’t started: its panel, commands, agent tools and backend are left
        out. Its settings and saved data are kept. Changes apply after restarting Flint.
      </p>
      {optional.map((m) => {
        const on = featureOn(toggles, m)
        return (
          <div key={m.id} className="extension-card">
            <div className="extension-head">
              <label className="extension-toggle" title={on ? 'Turn off' : 'Turn on'}>
                <input type="checkbox" checked={on} onChange={(e) => update((s) => ({ features: withFeature(coerceToggles(s.features), m.id, e.target.checked) }))} />
                <strong>{m.name ?? m.id}</strong>
              </label>
              <span className="muted">built-in</span>
              <span className={`extension-state ${active.has(m.id) ? 'running' : 'disabled'}`}>{active.has(m.id) ? 'On' : 'Off'}</span>
              {pending.has(m.id) && <span className="extension-state starting">Restart Flint to {on ? 'turn on' : 'turn off'}</span>}
            </div>
            {m.description && <p className="extension-description">{m.description}</p>}
          </div>
        )
      })}
      <h3>Installed</h3>
    </>
  )
}
