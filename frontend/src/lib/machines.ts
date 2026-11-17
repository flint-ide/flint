// Remote machines ("machines" in settings.json; Go: internal/settings/machines.go
// and internal/remote): terminals that run on another computer over the user's own
// ssh. Go validates every value and stays the authority; the patterns here mirror
// it so the form can answer at once. Pure helpers, so node:test can run them.

export interface Machine {
  /** Names the machine in terminal ids ("<id>:<session>"). */
  id: string
  /** Shown in tabs and menus; '' shows the id. */
  name: string
  /** What ssh connects to: host, user@host or an alias of ~/.ssh/config. */
  target: string
  /** ssh port; 0 leaves it to ssh. */
  port: number
  /** Flint on the remote machine: a name or a path, never with arguments. */
  command: string
}

/** A machine settings.json held that validation dropped (Go: MachineIssue). */
export interface MachineIssue {
  index: number
  id: string
  reason: string
}

export type MachineState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'failed' | 'closed'

/** A machine's connection as Go reports it (machine:status). */
export interface MachineStatus {
  id: string
  name: string
  state: MachineState
  error: string
  stderr: string
  attempt: number
  retryInSeconds: number
}

/** The verdict of a Check (Go: remote.CheckResult). */
export interface CheckResult {
  status: 'ok' | 'incompatible' | 'notFound' | 'error'
  version: string
  protocol: number
  message: string
}

// Same limits and patterns as Go (settings.MaxMachines and the patterns of machines.go).
export const MAX_MACHINES = 32
export const MAX_MACHINE_NAME = 64
export const DEFAULT_MACHINE_COMMAND = 'flint'
const ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,31}$/
const TARGET_PATTERN = /^[A-Za-z0-9_][A-Za-z0-9._-]{0,62}(@[A-Za-z0-9][A-Za-z0-9.-]{0,252})?$/
const COMMAND_PATTERN = /^[A-Za-z0-9_./~-]{1,256}$/

const ID_HINT = 'lowercase letters, digits and dashes, starting with a letter or digit, at most 32'
const TARGET_HINT = "host or user@host: letters, digits, '.', '_' and '-', not starting with '-'"
const COMMAND_HINT = "a file name or path made of letters, digits, '.', '_', '/', '~' and '-', not starting with '-'"

export const EMPTY_MACHINE: Machine = Object.freeze({ id: '', name: '', target: '', port: 0, command: '' })

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

/** Machines from untrusted JSON: only the shape is checked, validation is Go's. */
export function coerceMachines(v: unknown): Machine[] {
  if (!Array.isArray(v)) return []
  return v.filter(isObject).map((m) => ({
    id: typeof m.id === 'string' ? m.id : '',
    name: typeof m.name === 'string' ? m.name : '',
    target: typeof m.target === 'string' ? m.target : '',
    port: typeof m.port === 'number' && Number.isInteger(m.port) ? m.port : 0,
    command: typeof m.command === 'string' ? m.command : '',
  }))
}

const clip = (s: string) => (s.length > 64 ? `${s.slice(0, 64)}…` : s)

/** Why a machine would be refused, '' when it would not (mirrors Go's CheckMachine; an empty command is the default). */
export function checkMachine(m: Machine): string {
  const command = m.command === '' ? DEFAULT_MACHINE_COMMAND : m.command
  if (!ID_PATTERN.test(m.id)) return `id "${clip(m.id)}": use ${ID_HINT}`
  if (!TARGET_PATTERN.test(m.target)) return `target "${clip(m.target)}": use ${TARGET_HINT}`
  if (!Number.isInteger(m.port) || m.port < 0 || m.port > 65535) return `port ${m.port} out of range 1–65535 (0 leaves it to ssh)`
  if (!COMMAND_PATTERN.test(command) || command.startsWith('-')) return `command "${clip(command)}": use ${COMMAND_HINT}`
  return checkName(m.name)
}

function checkName(name: string): string {
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f-\u009f]/.test(name)) return `invalid name "${clip(name)}"`
  if (name !== name.trim()) return `invalid name "${clip(name)}"`
  if ([...name].length > MAX_MACHINE_NAME) return `name longer than ${MAX_MACHINE_NAME} characters`
  // A lone surrogate is not valid UTF-8 once it reaches Go.
  if (/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/.test(name)) return 'name is not valid UTF-8'
  return ''
}

/** Why a list would be refused (too many, an invalid entry, a repeated id), '' when it would not. */
export function checkMachines(list: readonly Machine[]): string {
  if (list.length > MAX_MACHINES) return `machines: too many (max ${MAX_MACHINES})`
  const seen = new Set<string>()
  for (const m of list) {
    const why = checkMachine(m)
    if (why) return `machines: ${why}`
    if (seen.has(m.id)) return `machines: id "${m.id}" is used twice`
    seen.add(m.id)
  }
  return ''
}

/** The name to show for a machine. */
export function machineLabel(m: Pick<Machine, 'id' | 'name'>): string {
  return m.name || m.id
}

/** An id for a new machine from its name or target, not used by list. */
export function suggestMachineId(source: string, list: readonly Machine[]): string {
  const base =
    source
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 28) || 'machine'
  const taken = new Set(list.map((m) => m.id))
  if (!taken.has(base)) return base
  for (let n = 2; ; n++) {
    const id = `${base}-${n}`
    if (!taken.has(id)) return id
  }
}

/** The list with the machine at index replaced; the others are untouched. */
export function withMachine(list: readonly Machine[], index: number, change: Partial<Machine>): Machine[] {
  return list.map((m, i) => (i === index ? { ...m, ...change } : m))
}

export function withoutMachine(list: readonly Machine[], index: number): Machine[] {
  return list.filter((_, i) => i !== index)
}

// ---------- terminal ids ----------

/** The id of a terminal tab on a machine: "<machineId>:<session>" (Go: remote.JoinID). */
export function remoteTermId(machineId: string, session: string): string {
  return `${machineId}:${session}`
}

/**
 * The machine and session of a remote terminal id, null for a local id (which
 * never has a ':': UUIDs) and for a malformed one (Go: remote.SplitID).
 */
export function splitRemoteTermId(id: string): { machine: string; session: string } | null {
  const i = id.indexOf(':')
  if (i < 0) return null
  const machine = id.slice(0, i)
  const session = id.slice(i + 1)
  if (!ID_PATTERN.test(machine) || session === '' || session.includes(':')) return null
  return { machine, session }
}

/** Whether an id addresses a machine (it has a ':'), valid or not. */
export const isRemoteTermId = (id: string): boolean => id.includes(':')

// ---------- tabs ----------

/** The tab title of a remote terminal: "<machine name> ▸ <title>". */
export function remoteTabTitle(machineName: string, title: string): string {
  return `${machineName} ▸ ${title}`
}

/** The small badge on a remote tab: the machine's name, cut to fit. */
export function machineBadge(machineName: string): string {
  const chars = [...machineName]
  return chars.length > 12 ? `${chars.slice(0, 11).join('')}…` : machineName
}

/** What a restored remote tab does (see restoreRemoteTab). */
export type RemoteRestore =
  | { mode: 'attach'; machine: string }
  /** The tab opens as ended and says why; no shell is ever started on restore. */
  | { mode: 'ended'; machine: string; reason: string }

/**
 * How a saved terminal tab on a machine comes back. It never starts a new
 * remote shell: it attaches to the session the machine kept (Go answers "the
 * session no longer exists" when the machine lost it, and the tab then shows
 * that it ended), or it opens ended because the machine is no longer
 * configured. Returns null for a local tab.
 */
export function restoreRemoteTab(savedId: string | undefined, machineIds: ReadonlySet<string>): RemoteRestore | null {
  const parts = savedId ? splitRemoteTermId(savedId) : null
  if (!parts) return null
  if (!machineIds.has(parts.machine)) {
    return { mode: 'ended', machine: parts.machine, reason: `The machine "${parts.machine}" is no longer in Settings ▸ Machines, so this terminal cannot be reached.` }
  }
  return { mode: 'attach', machine: parts.machine }
}

/** The line a tab shows when its remote session is gone. */
export function endedOnMachine(machineName: string): string {
  return `session ended on ${machineName}`
}

// ---------- status ----------

export type MachineTone = 'busy' | 'error'

export interface MachinesSummary {
  label: string
  tone: MachineTone
  /** One line per machine that needs attention, for the tooltip. */
  details: string[]
}

/** What the status bar item says about the machines: null while none is connecting, reconnecting or failed. */
export function machinesSummary(statuses: readonly MachineStatus[]): MachinesSummary | null {
  const attention = statuses.filter((s) => s.state === 'connecting' || s.state === 'reconnecting' || s.state === 'failed')
  if (attention.length === 0) return null
  const failed = attention.filter((s) => s.state === 'failed')
  const name = (s: MachineStatus) => s.name || s.id
  const details = attention.map((s) => machineLine(s, name(s)))
  if (failed.length > 0) {
    const label = failed.length === 1 ? `${name(failed[0])}: connection failed` : `${failed.length} machines failed`
    return { label, tone: 'error', details }
  }
  const reconnecting = attention.some((s) => s.state === 'reconnecting')
  const label = attention.length === 1 ? `${name(attention[0])}: ${reconnecting ? 'reconnecting' : 'connecting'}…` : `${attention.length} machines connecting…`
  return { label, tone: 'busy', details }
}

/** One machine's state as a sentence, with ssh's redacted error and the wait before the next try. */
export function machineLine(s: MachineStatus, name = s.name || s.id): string {
  const retry = s.retryInSeconds > 0 ? ` (next try in ${s.retryInSeconds} s)` : ''
  switch (s.state) {
    case 'connecting':
      return `${name}: connecting…`
    case 'reconnecting':
      return `${name}: reconnecting…${retry}${s.error ? ` — ${s.error}` : ''}`
    case 'failed':
      return `${name}: ${s.error || 'connection failed'}${s.stderr ? ` — ${lastLine(s.stderr)}` : ''}${retry}`
    default:
      return `${name}: ${s.state}`
  }
}

const lastLine = (text: string) => text.trim().split('\n').pop() ?? ''

/** Replaces or adds one machine's status in a list (machine:status events). */
export function withStatus(list: readonly MachineStatus[], next: MachineStatus): MachineStatus[] {
  const i = list.findIndex((s) => s.id === next.id)
  return i < 0 ? [...list, next] : list.map((s, j) => (j === i ? next : s))
}

/** The status of a terminal tab's machine as a banner above the terminal; null when connected or idle. */
export function tabBanner(status: MachineStatus | undefined): { text: string; tone: MachineTone } | null {
  if (!status || (status.state !== 'connecting' && status.state !== 'reconnecting' && status.state !== 'failed')) return null
  return { text: machineLine(status), tone: status.state === 'failed' ? 'error' : 'busy' }
}
