// Bringing back the agent CLI a terminal was running before a restart
// (Go: internal/terminal/resume.go finds it). Pure, so it is testable.

/** The agent conversation a terminal ran, as terminal.Resume in Go and in a saved session. */
export interface TerminalResume {
  /** 'claude' or 'codex'. */
  agent: string
  /** The conversation's id when known, else '' (the latest one in the folder is continued). */
  session: string
}

const SESSION_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

/**
 * What to keep of an answer from Go or of a saved session, or null when it names
 * no agent Flint can resume. A saved session is a plain file, and the result is
 * typed into a shell: nothing but a known agent and a well-formed id gets through.
 */
export function coerceResume(v: unknown): TerminalResume | null {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return null
  const { agent, session } = v as Record<string, unknown>
  if (agent !== 'claude' && agent !== 'codex') return null
  // Only Claude Code's conversations are found by id.
  const id = agent === 'claude' && typeof session === 'string' && SESSION_ID.test(session) ? session : ''
  return { agent, session: id }
}

/** The command that continues the conversation, typed into the reopened terminal; '' for none. */
export function resumeCommand(v: unknown): string {
  const r = coerceResume(v)
  if (!r) return ''
  if (r.agent === 'codex') return 'codex resume --last'
  return r.session ? `claude --resume ${r.session}` : 'claude --continue'
}
