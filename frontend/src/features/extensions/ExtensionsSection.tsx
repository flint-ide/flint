import { useSettings } from '../../store/settings'
import { activeThemeId, customThemeFrom } from '../../lib/contributedThemes'
import { useContributions } from '../../lib/contributions'
import { installFromFolder, uninstallExtension, useExtensions } from './store'
import { themeContributions, type InstalledExtension } from './extensions'
import type { ThemeContribution } from '../../lib/features'

const SWATCHES = ['--bg-editor', '--fg', '--accent', '--syn-keyword', '--syn-string']

/** Settings ▸ Extensions: what is installed, installing from a folder and uninstalling. */
export function ExtensionsSection() {
  const { extensions, loaded, busy, error } = useExtensions()
  return (
    <section>
      <h2>Extensions</h2>
      <p className="muted settings-note">
        Extensions are folders with a <code>flint-extension.json</code> manifest, installed only from a folder you choose. Flint draws
        everything they add with its own components and checks their colors like any custom theme. This version applies their color themes;
        extensions with a program (commands, panels, agent tools) are listed but not started yet.
      </p>
      <div className="settings-actions">
        <button disabled={busy} onClick={() => void installFromFolder()}>Install from Folder…</button>
      </div>
      {error && <p className="error">Can't read the extensions folder: {error}</p>}
      {loaded && !error && extensions.length === 0 && <p className="muted">No extensions installed.</p>}
      {extensions.map((e) => (
        <ExtensionCard key={e.id} ext={e} busy={busy} />
      ))}
    </section>
  )
}

function ExtensionCard({ ext, busy }: { ext: InstalledExtension; busy: boolean }) {
  return (
    <div className="account-card">
      <div className="account-head">
        <div className="account-who">
          <b>
            {ext.name} {ext.version && <span className="muted">{ext.version}</span>}
          </b>
          <span className="muted">
            {ext.id}
            {ext.publisher && ` · ${ext.publisher}`}
          </span>
        </div>
        <span className="spacer" />
        <button disabled={busy} onClick={() => void uninstallExtension(ext.id)}>Uninstall</button>
      </div>
      {ext.error ? (
        <span className="error">Disabled: {ext.error}</span>
      ) : (
        <>
          {ext.description && <span>{ext.description}</span>}
          {ext.contributes.length > 0 && <span className="muted">Adds {ext.contributes.join(', ')}.</span>}
          {ext.permissions.length > 0 && <span className="muted">Permissions: {ext.permissions.join(', ')}</span>}
          {ext.program && <span className="muted">Has a program, which this version of Flint does not start.</span>}
          <ExtensionThemes themes={themeContributions(ext)} />
        </>
      )}
      <span className="muted">
        <code>{ext.dir}</code>
      </span>
    </div>
  )
}

function ExtensionThemes({ themes }: { themes: readonly ThemeContribution[] }) {
  const all = useContributions((s) => s.themes)
  const active = useSettings((s) => activeThemeId(s.settings, all))
  const update = useSettings((s) => s.update)
  if (themes.length === 0) return null
  return (
    <div className="extension-themes">
      {themes.map((t) => (
        <button key={t.id} className={active === t.id ? 'on' : ''} aria-pressed={active === t.id} onClick={() => update({ theme: 'custom', customTheme: customThemeFrom(t) })}>
          <span className="theme-swatches" aria-hidden="true">
            {SWATCHES.filter((v) => t.colors[v]).map((v) => (
              <span key={v} className="theme-swatch" style={{ background: t.colors[v] }} />
            ))}
          </span>
          {active === t.id ? `${t.label} (in use)` : `Use ${t.label}`}
        </button>
      ))}
    </div>
  )
}
