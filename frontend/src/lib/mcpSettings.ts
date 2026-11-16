// The "mcpServer" part of settings.json (Go: internal/settings/mcpserver.go):
// what Flint's own MCP server tells the agents Flint starts. Go validates it;
// this only shapes it for the Settings tab and fills in defaults.

/** The server's tools, each on or off. */
export interface McpTools {
  diagnostics: boolean
  editor: boolean
  terminals: boolean
  tests: boolean
}

export type McpToolKey = keyof McpTools

/**
 * The tools that act for an agent, by group. All off by default; when a group
 * is on, Flint asks before every call.
 */
export interface McpActions {
  /** run_tests */
  tests: boolean
  /** run_configuration and get_run_output */
  run: boolean
  /** send_http_request */
  http: boolean
  /** run_sql_query (read-only connections) */
  sql: boolean
}

export type McpActionKey = keyof McpActions

export interface McpServerSettings {
  /** Offer the server to new agent sessions. */
  enabled: boolean
  tools: McpTools
  actions: McpActions
  /** The most lines of one terminal's output an agent gets. */
  terminalLines: number
  /** How long run_tests waits for a test run before it answers with the results so far. */
  testWaitSeconds: number
}

/** Same bounds as Go's Min/MaxTerminalLines. */
export const TERMINAL_LINES = { min: 10, max: 2000 }
/** Same bounds as Go's Min/MaxTestWaitSeconds. */
export const TEST_WAIT_SECONDS = { min: 10, max: 540 }

/** Mirrors Go's defaultMCPServer. */
export const DEFAULT_MCP_SERVER: McpServerSettings = Object.freeze({
  enabled: true,
  tools: Object.freeze({ diagnostics: true, editor: true, terminals: false, tests: true }),
  actions: Object.freeze({ tests: false, run: false, http: false, sql: false }),
  terminalLines: 200,
  testWaitSeconds: 120,
}) as McpServerSettings

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const bool = (v: unknown, d: boolean) => (typeof v === 'boolean' ? v : d)

/** Settings from untrusted JSON: missing or invalid values get the defaults, like Go's Normalize. */
export function coerceMcpServer(v: unknown): McpServerSettings {
  const d = DEFAULT_MCP_SERVER
  if (!isObject(v)) return d
  const t = isObject(v.tools) ? v.tools : {}
  const lines = typeof v.terminalLines === 'number' && Number.isFinite(v.terminalLines) && v.terminalLines !== 0
    ? Math.min(TERMINAL_LINES.max, Math.max(TERMINAL_LINES.min, Math.round(v.terminalLines)))
    : d.terminalLines
  const a = isObject(v.actions) ? v.actions : {}
  const wait = typeof v.testWaitSeconds === 'number' && Number.isFinite(v.testWaitSeconds) && v.testWaitSeconds !== 0
    ? Math.min(TEST_WAIT_SECONDS.max, Math.max(TEST_WAIT_SECONDS.min, Math.round(v.testWaitSeconds)))
    : d.testWaitSeconds
  return {
    enabled: bool(v.enabled, d.enabled),
    tools: {
      diagnostics: bool(t.diagnostics, d.tools.diagnostics),
      editor: bool(t.editor, d.tools.editor),
      terminals: bool(t.terminals, d.tools.terminals),
      tests: bool(t.tests, d.tools.tests),
    },
    // Only a literal true switches an acting tool on.
    actions: { tests: a.tests === true, run: a.run === true, http: a.http === true, sql: a.sql === true },
    terminalLines: lines,
    testWaitSeconds: wait,
  }
}

/** Whether the editor's state should be sent to Go at all. */
export const sharesEditor = (m: McpServerSettings) => m.enabled && m.tools.editor
