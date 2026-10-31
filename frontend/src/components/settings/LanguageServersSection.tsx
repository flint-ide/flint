import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { EMPTY_SERVER, formatArgs, isDefaultServer, parseArgs, withServer, type LanguageServerSettings } from '../../lib/toolSettings'
import * as LSP from '../../../bindings/flint/lspapi'
import * as SettingsAPI from '../../../bindings/flint/settingsapi'
import { lsp, settings as models } from '../../wails/models'
import { CheckedField, Row } from './fields'

const LABELS: Record<string, string> = { go: 'Go', typescript: 'TypeScript / JavaScript', python: 'Python', rust: 'Rust' }

export function LanguageServersSection() {
  const [languages, setLanguages] = useState<lsp.LanguageInfo[]>([])
  useEffect(() => {
    LSP.Languages().then((l) => setLanguages(l ?? []), () => setLanguages([]))
  }, [])
  return (
    <section>
      <h2>Language Servers</h2>
      <p className="muted settings-note">
        A change restarts that language's servers. Servers that run the project's code (rust-analyzer, the Python servers) and servers Flint
        doesn't know start only in trusted folders; extra arguments are ignored in Restricted Mode.
      </p>
      {languages.map((l) => (
        <ServerRows key={l.language} info={l} />
      ))}
    </section>
  )
}

/** On/off, server and extra arguments of one language. */
function ServerRows({ info }: { info: lsp.LanguageInfo }) {
  const { language, servers, needsTrust, install } = info
  const server = useSettings((s) => s.settings.languageServers[language]) ?? EMPTY_SERVER
  const update = useSettings((s) => s.update)
  const check = (change: Partial<LanguageServerSettings>) =>
    SettingsAPI.CheckLanguageServer(language, models.LanguageServerSettings.createFrom({ ...server, ...change }))
  const commit = (change: Partial<LanguageServerSettings>) => update((s) => ({ languageServers: withServer(s.languageServers, language, change) }))
  const listId = `settings-lsp-${language}`
  return (
    <>
      <h3>{LABELS[language] ?? language}</h3>
      <Row label="Enabled" hint={needsTrust ? 'Runs the project\'s code: starts only in trusted folders.' : undefined}>
        <input type="checkbox" checked={!server.disabled} onChange={(e) => commit({ disabled: !e.target.checked })} />
      </Row>
      <Row label="Server" hint={`Empty tries ${(servers ?? []).join(', ')} in this order. Install: ${install}`}>
        <CheckedField
          value={server.command}
          list={listId}
          placeholder="Automatic"
          check={(command) => check({ command })}
          onCommit={(command) => commit({ command })}
        />
        <datalist id={listId}>
          {(servers ?? []).map((s) => <option key={s} value={s} />)}
        </datalist>
      </Row>
      <Row label="Extra arguments" hint="One per line, after the server's own (--stdio…).">
        <CheckedField
          multiline
          value={formatArgs(server.args)}
          check={(text) => check({ args: parseArgs(text) })}
          onCommit={(text) => commit({ args: parseArgs(text) })}
        />
      </Row>
      {!isDefaultServer(server) && (
        <div className="settings-actions">
          <button onClick={() => commit({ ...EMPTY_SERVER, args: [] })}>Reset {LABELS[language] ?? language} to Defaults</button>
        </div>
      )}
    </>
  )
}
