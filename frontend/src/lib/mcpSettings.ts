// The "mcpServer" part of settings.json (Go: internal/settings/mcpserver.go):
// what Flint's own MCP server tells the agents Flint starts. Go validates it;
// this only shapes it for the Settings tab and fills in defaults.

/** The server's tools, each on or off. */
export interface McpTools {
  diagnostics: boolean
  editor: boolean
  terminals: boolean
  tests: boolean
  kubernetes: boolean
}

export type McpToolKey = keyof McpTools

export interface McpServerSettings {
  /** Offer the server to new agent sessions. */
  enabled: boolean
  tools: McpTools
  /** The most lines of one terminal's output an agent gets. */
  terminalLines: number
}

/** Same bounds as Go's Min/MaxTerminalLines. */
export const TERMINAL_LINES = { min: 10, max: 2000 }

/** Mirrors Go's defaultMCPServer. */
export const DEFAULT_MCP_SERVER: McpServerSettings = Object.freeze({
  enabled: true,
  tools: Object.freeze({ diagnostics: true, editor: true, terminals: true, tests: true, kubernetes: true }),
  terminalLines: 200,
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
  return {
    enabled: bool(v.enabled, d.enabled),
    tools: {
      diagnostics: bool(t.diagnostics, d.tools.diagnostics),
      editor: bool(t.editor, d.tools.editor),
      terminals: bool(t.terminals, d.tools.terminals),
      tests: bool(t.tests, d.tools.tests),
      kubernetes: bool(t.kubernetes, d.tools.kubernetes),
    },
    terminalLines: lines,
  }
}

/** Whether the editor's state should be sent to Go at all. */
export const sharesEditor = (m: McpServerSettings) => m.enabled && m.tools.editor
