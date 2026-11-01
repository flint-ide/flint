import { useSettings } from '../../store/settings'
import { DEFAULT_MCP_SERVER, TERMINAL_LINES, type McpToolKey } from '../../lib/mcpSettings'
import { NumberField, Row } from './fields'

const TOOLS: { key: McpToolKey; label: string; hint: string }[] = [
  { key: 'diagnostics', label: 'Diagnostics', hint: 'Errors and warnings from the language servers.' },
  { key: 'editor', label: 'Open editors and selection', hint: 'Which files are open, the active one, and the selected text.' },
  { key: 'terminals', label: 'Terminal output', hint: 'Recent output printed in the project by terminals and run consoles. Off by default; the agent asks before each call.' },
  { key: 'tests', label: 'Test results', hint: 'The last test run: totals and failed tests with their output.' },
  { key: 'kubernetes', label: 'Kubernetes status', hint: 'Failing apps, pods and warning events of the current context. No secrets or logs. Off by default; the agent asks before each call.' },
]

/** Settings ▸ Agent Context: Flint's own MCP server for the agents it starts. */
export function McpServerSection() {
  const mcp = useSettings((s) => s.settings.mcpServer)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<typeof mcp>) => update((s) => ({ mcpServer: { ...s.mcpServer, ...change } }))
  return (
    <section>
      <h2>Agent Context</h2>
      <Row label="Share Flint's context with agents" hint="Agent chats started in Flint can read what Flint knows through read-only MCP tools.">
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
