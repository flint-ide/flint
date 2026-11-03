import { useEffect, useState } from 'react'
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import { errorMessage } from '../../api/backend'
import { useSettings } from '../../store/settings'
import { extensionEnabled, toolsOffered, withFlag } from './settings'
import { loadExtensions, restartExtension, uninstallExtension, useExtensions, type Info } from './store'
import { themeContributions, type ExtensionInfo } from './model'
import { activeThemeId, customThemeFrom } from '../../lib/contributedThemes'
import { useContributions } from '../../lib/contributions'
import type { ThemeContribution } from '../../lib/features'
import { BuiltinExtensions } from './BuiltinExtensions'
import { CatalogSection } from './CatalogSection'

const STATE_LABELS: Record<string, string> = {
  invalid: 'Broken',
  disabled: 'Off',
  stopped: 'Not running',
  starting: 'Starting…',
  running: 'Running',
  crashed: 'Crashed (starts again when used)',
  failed: 'Stopped after repeated crashes',
}

/** Settings ▸ Extensions: what is installed, its permissions and state, and the switches. */
export function ExtensionsSection() {
  const list = useExtensions((s) => s.list)
  const loaded = useExtensions((s) => s.loaded)
  useEffect(() => void loadExtensions(), [])
  return (
    <section>
      <h2>Extensions</h2>
      <p className="muted settings-note">
        Extensions add color themes, or are programs that add commands, panels, settings, status bar items and agent tools. Flint draws everything they
        show and checks every request against the permissions they declare, but a program runs with your user's permissions: install only ones you trust.
        One starts when you first use it and stops after five idle minutes. A theme-only extension has no program and runs no code.
      </p>
      <BuiltinExtensions />
      <CatalogSection />
      <h3>Installed</h3>
      {loaded && list.length === 0 && <p className="muted settings-note">No extensions installed.</p>}
      {list.map((info) => (
        <ExtensionCard key={info.id} info={info} />
      ))}
    </section>
  )
}

function ExtensionCard({ info }: { info: Info }) {
  const extensions = useSettings((s) => s.settings.extensions)
  const update = useSettings((s) => s.update)
  const conflicts = useExtensions((s) => s.conflicts[info.id])
  const [log, setLog] = useState<string | null>(null)
  const showLog = () =>
    API.Log(info.id).then(
      (text) => setLog(text || '(empty)'),
      (err: unknown) => setLog(errorMessage(err)),
    )
  const enabled = extensionEnabled(extensions, info.id)
  const broken = info.state === 'invalid'
  return (
    <div className="extension-card">
      <div className="extension-head">
        <label className="extension-toggle" title={enabled ? 'Turn off' : 'Turn on'}>
          <input
            type="checkbox"
            checked={enabled}
            disabled={broken}
            onChange={(e) => update((s) => ({ extensions: withFlag(s.extensions, 'enabled', info.id, e.target.checked) }))}
          />
          <strong>{info.name}</strong>
        </label>
        {info.version && <span className="muted">{info.version}</span>}
        {info.publisher && <span className="muted">by {info.publisher}</span>}
        <span className="muted">{info.id}</span>
        <span className={`extension-state ${info.state}`}>{STATE_LABELS[info.state] ?? info.state}</span>
      </div>
      {info.description && <p className="extension-description">{info.description}</p>}
      {info.error && <p className="error">{info.error}</p>}
      {[...info.warnings, ...(conflicts ?? [])].map((w) => (
        <p key={w} className="muted extension-warning">{w}</p>
      ))}
      {!broken && (
        <dl className="extension-facts">
          <dt>Program</dt>
          <dd>
            {info.program ? (
              <>
                <code>{info.program}</code> in <code>{info.dir}</code>
                {info.runsAtStartup ? ' (starts with Flint)' : ''}
              </>
            ) : (
              <>
                None: it only adds data and runs no code (<code>{info.dir}</code>)
              </>
            )}
          </dd>
          {info.themes.length > 0 && (
            <>
              <dt>Color themes</dt>
              <dd>
                <ExtensionThemes themes={themeContributions(info as ExtensionInfo)} />
              </dd>
            </>
          )}
          {(info.languages?.length ?? 0) > 0 && (
            <>
              <dt>Languages</dt>
              <dd>
                {info.languages.map((l) => (
                  <div key={l.id}>
                    {l.label} <span className="muted">({[...(l.extensions ?? []), ...(l.filenames ?? [])].join(', ')})</span>
                    {l.server && (
                      <div className="muted">
                        Language server: <code>{l.server.candidates.map((c) => c.join(' ')).join(' | ')}</code> (in trusted folders)
                      </div>
                    )}
                  </div>
                ))}
              </dd>
            </>
          )}
          <dt>Permissions</dt>
          <dd>{info.permissionTexts.length ? info.permissionTexts.map((p) => <div key={p}>{p}</div>) : 'None'}</dd>
          {info.agentTools.length > 0 && (
            <>
              <dt>Agent tools</dt>
              <dd>
                <label>
                  <input
                    type="checkbox"
                    checked={toolsOffered(extensions, info.id)}
                    onChange={(e) => update((s) => ({ extensions: withFlag(s.extensions, 'agentTools', info.id, e.target.checked) }))}
                  />{' '}
                  Offer to agents (read-only, in trusted folders, when Settings ▸ Agent Context is on)
                </label>
                {info.agentTools.map((t) => (
                  <div key={t.name} className="muted">
                    <code>{t.agentName || t.name}</code> {t.description}
                  </div>
                ))}
              </dd>
            </>
          )}
        </dl>
      )}
      <div className="settings-actions">
        <button onClick={() => (log === null ? void showLog() : setLog(null))}>{log === null ? 'Show Log' : 'Hide Log'}</button>
        {log !== null && <button onClick={() => void showLog()}>Refresh Log</button>}
        {(info.state === 'failed' || info.state === 'crashed') && <button onClick={() => void restartExtension(info.id)}>Restart</button>}
        <button className="danger" onClick={() => void uninstallExtension(info.id)}>Uninstall…</button>
      </div>
      {log !== null && <pre className="extension-log">{log}</pre>}
    </div>
  )
}

const SWATCHES = ['--bg-editor', '--fg', '--accent', '--syn-keyword', '--syn-string']

/** One button per theme: choosing it makes it the custom theme (Settings ▸ Appearance). */
function ExtensionThemes({ themes }: { themes: readonly ThemeContribution[] }) {
  const all = useContributions((s) => s.themes)
  const active = useSettings((s) => activeThemeId(s.settings, all))
  const update = useSettings((s) => s.update)
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
