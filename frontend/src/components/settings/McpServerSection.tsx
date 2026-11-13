import { useSettings } from '../../store/settings'
import { DEFAULT_MCP_SERVER, TERMINAL_LINES, TEST_WAIT_SECONDS, type McpActionKey, type McpToolKey } from '../../lib/mcpSettings'
import { NumberField, Row } from './fields'

const TOOLS: { key: McpToolKey; label: string; hint: string }[] = [
  { key: 'diagnostics', label: 'Diagnostics', hint: 'Errors and warnings from the language servers.' },
  { key: 'editor', label: 'Open editors and selection', hint: 'Which files are open, the active one, and the selected text.' },
  { key: 'terminals', label: 'Terminal output', hint: 'Recent output printed in the project by terminals and run consoles. Off by default; the agent asks before each call.' },
  { key: 'tests', label: 'Test results', hint: 'The last test run: totals and failed tests with their output.' },
]

const ACTIONS: { key: McpActionKey; label: string; hint: string }[] = [
  { key: 'tests', label: 'Run tests', hint: 'run_tests: a Go package, a test file or single tests, in the Tests tab, like the ▶ in the gutter.' },
  { key: 'run', label: 'Run configurations', hint: 'run_configuration starts one of the project\'s run configurations in its run tab; get_run_output reads its output.' },
  { key: 'http', label: 'Send HTTP requests', hint: 'send_http_request: a named request of a .http file in the project; the response is kept in the file\'s history.' },
  { key: 'sql', label: 'Run SQL queries', hint: 'run_sql_query: one reading statement on a saved connection, only when that connection is read-only.' },
]

/** Settings ▸ Agent Context: Flint's own MCP server for the agents it starts. */
export function McpServerSection() {
  const mcp = useSettings((s) => s.settings.mcpServer)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<typeof mcp>) => update((s) => ({ mcpServer: { ...s.mcpServer, ...change } }))
  return (
    <section>
      <h2>Agent Context</h2>
      <Row label="Share Flint's context with agents" hint="Agent chats started in Flint can read what Flint knows through MCP tools. The tools under Tools only read.">
        <input type="checkbox" checked={mcp.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
      </Row>
      <h3>Tools</h3>
      {TOOLS.map((t) => (
        <Row key={t.key} label={t.label} hint={t.hint}>
          <input
            type="checkbox"
            disabled={!mcp.enabled}
            checked={mcp.tools[t.key]}
            onChange={(e) => set({ tools: { ...mcp.tools, [t.key]: e.target.checked } })}
          />
        </Row>
      ))}
      <Row label="Terminal lines" hint="The most lines of one terminal's output an agent gets.">
        <NumberField value={mcp.terminalLines} min={TERMINAL_LINES.min} max={TERMINAL_LINES.max} step={10} onCommit={(v) => set({ terminalLines: Math.round(v) })} />
      </Row>
      <h3>Actions</h3>
      <p className="muted settings-note">
        Tools that do something in Flint for an agent. They are off until you turn them on, and Flint asks you before every call: in the chat when
        the agent asks there, otherwise in a dialog. "Always allow" and "Do everything" don't apply to them.
      </p>
      {ACTIONS.map((t) => (
        <Row key={t.key} label={t.label} hint={t.hint}>
          <input
            type="checkbox"
            disabled={!mcp.enabled}
            checked={mcp.actions[t.key]}
            onChange={(e) => set({ actions: { ...mcp.actions, [t.key]: e.target.checked } })}
          />
        </Row>
      ))}
      <Row label="Test wait (seconds)" hint="How long run_tests waits for a test run before it answers with the results so far.">
        <NumberField value={mcp.testWaitSeconds} min={TEST_WAIT_SECONDS.min} max={TEST_WAIT_SECONDS.max} step={10} onCommit={(v) => set({ testWaitSeconds: Math.round(v) })} />
      </Row>
      <div className="settings-actions">
        <button disabled={JSON.stringify(mcp) === JSON.stringify(DEFAULT_MCP_SERVER)} onClick={() => update({ mcpServer: DEFAULT_MCP_SERVER })}>
          Reset to Defaults
        </button>
      </div>
      <p className="muted settings-note">
        Flint serves these tools on 127.0.0.1 only, with a separate token per chat that ends with the chat: an agent sees only its own project.
        Answers are bounded and credentials are redacted. Folders in Restricted Mode get no access. Turning the server or a tool off applies at
        once. Commit messages and other one-shot runs don't use it.
      </p>
    </section>
  )
}
