// The shape of settings.json (internal/settings in Go) and the pure parts of
// applying it: theme resolution and the CSS variables editors read. Framework-free,
// so it is testable with node:test; store/settings.ts does the applying.
// Keys that feature modules own are not listed here: see lib/settingsSections.ts.

import {
  coerceAgents, coerceRefresh, coerceServers, DEFAULT_REFRESH,
  type AgentSettings, type LanguageServerSettings, type RefreshSettings,
} from './toolSettings'
import { coerceCustomAgents, type CustomAgent } from './customAgents'
import { coerceMcpServer, DEFAULT_MCP_SERVER, type McpServerSettings } from './mcpSettings'
import {
  coerceCompletions, coerceInlineEdit, DEFAULT_COMPLETIONS, DEFAULT_INLINE_EDIT,
  type CompletionSettings, type InlineEditSettings,
} from './editorAiSettings'
import { coerceAgentTasks, DEFAULT_AGENT_TASKS, type AgentTaskSettings } from './agentTasks'
import { coerceUsage, DEFAULT_USAGE, type UsageSettings } from './usageSettings'
import { coercePromptSettings, DEFAULT_PROMPT_SETTINGS, type PromptSettings } from './prompts'
import { coerceAgentReview, DEFAULT_AGENT_REVIEW, type AgentReviewSettings } from './reviewSettings'
import { coerceCI, DEFAULT_CI, type CISettings } from './ci'
import { coerceGitHub, DEFAULT_GITHUB, type GitHubSettings } from './githubSettings'
import { coerceSections, type FeatureSettings } from './settingsSections'
import { TERMINAL_COLOR_NAMES } from './terminalTheme'

import type { ResolvedTheme, ThemeMode } from './themeMode'

export { resolveTheme, type ResolvedTheme, type ThemeMode } from './themeMode'

export interface FontSettings {
  fontFamily: string
  fontSize: number
  lineHeight: number
}

export interface EditorSettings extends FontSettings {
  /** Vim key bindings in every editor. */
  vimMode: boolean
}

/** A palette laid over the dark or light one (Theme "custom"), e.g. imported from VS Code. */
export interface CustomTheme {
  name: string
  base: ResolvedTheme
  /** CSS variable ('--bg-editor') → hex color; Go allows only style.css's theme variables. */
  colors: Record<string, string>
  /** xterm color ('red', 'brightBlue', 'background') → hex color; see TERMINAL_COLOR_NAMES. */
  terminal: Record<string, string>
  /** The extension theme it was taken from ('acme.nord.dark'), or '' (imported or hand-written). */
  source: string
}

/** The keys of settings.json that Flint itself owns. */
export interface CoreSettings {
  version: number
  theme: ThemeMode
  editor: EditorSettings
  terminal: FontSettings
  customTheme: CustomTheme
  /** Agent for new chats (a built-in or custom agent's id); '' = the first installed one. */
  defaultAgent: string
  /** Save modified files when the window loses focus. */
  autoSave: boolean
  /** Notification and Dock badge when work finishes while Flint is in the background. */
  notifyInBackground: boolean
  /** Shortcut overrides: command id → keys ('' = none). */
  keymap: Record<string, string>
  /** Folder names (or globs over one) the Linux file watcher skips, on top of dependency folders. */
  watchExclude: string[]
  /** Agent CLIs by name ('claude', 'codex'); missing = defaults. */
  agents: Record<string, AgentSettings>
  /** Agents added by the user, spoken to over ACP. */
  customAgents: CustomAgent[]
  /** Language servers by language ('go', 'typescript', …); missing = defaults. */
  languageServers: Record<string, LanguageServerSettings>
  /** Polling intervals of the Kubernetes and AWS panels. */
  refresh: RefreshSettings
  /** What Flint's own MCP server tells agents. */
  mcpServer: McpServerSettings
  /** Inline suggestions from a code model (lib/editorAiSettings.ts). */
  completions: CompletionSettings
  /** Edit with Agent on the selection. */
  inlineEdit: InlineEditSettings
  /** Where agent task worktrees go and how their branches start. */
  agentTasks: AgentTaskSettings
  /** Usage dashboard: retention, budgets and prices for estimates. */
  usage: UsageSettings
  /** The prompt library: whether Flint's own prompts are offered. */
  prompts: PromptSettings
  /** Review Changes with Agent (lib/reviewSettings.ts). */
  agentReview: AgentReviewSettings
  /** Following your pull requests' CI: poll interval, log excerpt size, notifications. */
  ci: CISettings
  /** GitHub host, the OAuth App Flint signs in with, scopes and the retry wait (lib/githubSettings.ts). */
  github: GitHubSettings
}

/** The whole settings.json: Flint's keys plus the sections features registered. */
export type AppSettings = CoreSettings & FeatureSettings

/** Mirrors settings.Default() in Go for Flint's own keys (used until the backend answers). */
export const DEFAULT_SETTINGS: CoreSettings = Object.freeze({
  version: 1,
  theme: 'dark',
  editor: Object.freeze({ fontFamily: `'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace`, fontSize: 13, lineHeight: 1.55, vimMode: false }),
  terminal: Object.freeze({ fontFamily: `"JetBrains Mono", "SF Mono", Menlo, monospace`, fontSize: 13, lineHeight: 1.2 }),
  customTheme: Object.freeze({
    name: '', base: 'dark', colors: Object.freeze({}) as Record<string, string>, terminal: Object.freeze({}) as Record<string, string>, source: '',
  }) as CustomTheme,
  defaultAgent: '',
  autoSave: false,
  notifyInBackground: true,
  keymap: Object.freeze({}) as Record<string, string>,
  watchExclude: Object.freeze(['dist', '.cache']) as unknown as string[],
  agents: Object.freeze({}) as Record<string, AgentSettings>,
  customAgents: Object.freeze([]) as unknown as CustomAgent[],
  languageServers: Object.freeze({}) as Record<string, LanguageServerSettings>,
  refresh: DEFAULT_REFRESH,
  mcpServer: DEFAULT_MCP_SERVER,
  completions: DEFAULT_COMPLETIONS,
  inlineEdit: DEFAULT_INLINE_EDIT,
  agentTasks: DEFAULT_AGENT_TASKS,
  usage: DEFAULT_USAGE,
  prompts: DEFAULT_PROMPT_SETTINGS,
  agentReview: DEFAULT_AGENT_REVIEW,
  ci: DEFAULT_CI,
  github: DEFAULT_GITHUB,
}) as CoreSettings

/** Flint's own keys; a feature's section may not use one. */
export const CORE_KEYS: readonly string[] = Object.freeze(Object.keys(DEFAULT_SETTINGS))

/** Same limits as Go's settings.Validate, for the inputs. */
export const FONT_SIZE = { min: 8, max: 32 }
export const LINE_HEIGHT = { min: 1, max: 2.5 }
export const MAX_EXCLUDES = 100

/** Mirrors Go's validExclude: rg reads a leading "!" as "include" and excludes travel comma-separated. */
export const validExclude = (p: string) => p !== '' && p.length <= 200 && !p.startsWith('!') && !/[,\x00-\x1f]/.test(p)

/** Mirrors Go's validWatchExclude: a folder name or a glob over one, so no slashes. */
export const validWatchExclude = (p: string) => validExclude(p) && !/[/\\]/.test(p)

/** One pattern per line → the patterns to save (trimmed, unique) and the lines that are not valid. */
export function parseExcludes(text: string, valid: (p: string) => boolean = validExclude): { patterns: string[]; invalid: string[] } {
  const patterns: string[] = []
  const invalid: string[] = []
  for (const line of text.split('\n')) {
    const p = line.trim()
    if (!p || patterns.includes(p)) continue
    if (valid(p)) patterns.push(p)
    else invalid.push(p)
  }
  if (patterns.length > MAX_EXCLUDES) invalid.push(...patterns.splice(MAX_EXCLUDES))
  return { patterns, invalid }
}

/** The custom theme's CSS variables, set on :root over the base palette (none unless Theme is Custom). */
export function themeCssVars(s: CoreSettings): Record<string, string> {
  return s.theme === 'custom' ? s.customTheme.colors : {}
}

const HEX_COLOR = /^#(?:[0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$/i
/** The CSS variables a custom theme may set: Go's settings.ThemeVariables (a Go test keeps them equal). */
export const THEME_VARIABLES: readonly string[] = Object.freeze([
  '--bg', '--bg-sidebar', '--bg-editor', '--bg-raised', '--bg-hover', '--bg-active', '--border',
  '--fg', '--fg-muted', '--fg-faint', '--fg-name',
  '--accent', '--accent-dim', '--accent-fg', '--danger', '--warn', '--info', '--ok',
  '--selection', '--selection-match', '--search-match', '--active-line',
  '--git-added', '--git-modified', '--git-deleted', '--diff-ins-bg', '--diff-del-bg',
  '--syn-keyword', '--syn-string', '--syn-number', '--syn-comment', '--syn-function',
  '--syn-type', '--syn-property', '--syn-definition', '--syn-punct',
])
const THEME_VARIABLE_SET: ReadonlySet<string> = new Set(THEME_VARIABLES)
const TERMINAL_COLOR_SET: ReadonlySet<string> = new Set(TERMINAL_COLOR_NAMES)
const MAX_THEME_SOURCE_LEN = 200 // Go's settings.MaxThemeSourceLen
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/

/** The hex colors of v whose names are allowed; anything else is dropped. */
export function coerceColorMap(v: unknown, allowed: ReadonlySet<string>): Record<string, string> {
  if (!isObject(v)) return {}
  return Object.fromEntries(Object.entries(v).filter(([name, color]) => allowed.has(name) && typeof color === 'string' && HEX_COLOR.test(color))) as Record<string, string>
}

/** A custom theme from untrusted JSON, filtered like Go's normalizeCustomTheme. */
export function coerceCustomTheme(v: unknown, def: CustomTheme = DEFAULT_SETTINGS.customTheme): CustomTheme {
  if (!isObject(v)) return def
  const source = typeof v.source === 'string' && v.source.length <= MAX_THEME_SOURCE_LEN && !CONTROL_CHARS.test(v.source) ? v.source : ''
  return {
    name: typeof v.name === 'string' ? v.name : '',
    base: v.base === 'light' ? 'light' : 'dark',
    colors: coerceColorMap(v.colors, THEME_VARIABLE_SET),
    terminal: coerceColorMap(v.terminal, TERMINAL_COLOR_SET),
    source,
  }
}

/** CSS custom properties on :root that the editors read (style.css, lib/editorSetup.ts). */
export function editorCssVars(s: CoreSettings): Record<string, string> {
  return {
    '--editor-font': s.editor.fontFamily,
    '--editor-font-size': `${s.editor.fontSize}px`,
    '--editor-line-height': String(s.editor.lineHeight),
  }
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

function font(v: unknown, def: FontSettings): FontSettings {
  if (!isObject(v)) return def
  const num = (x: unknown, min: number, max: number, d: number) => (typeof x === 'number' && x >= min && x <= max ? x : d)
  return {
    fontFamily: typeof v.fontFamily === 'string' && v.fontFamily.trim() && !/[;{}<>\\]/.test(v.fontFamily) ? v.fontFamily : def.fontFamily,
    fontSize: num(v.fontSize, FONT_SIZE.min, FONT_SIZE.max, def.fontSize),
    lineHeight: num(v.lineHeight, LINE_HEIGHT.min, LINE_HEIGHT.max, def.lineHeight),
  }
}

/**
 * Settings from untrusted JSON (the backend, or the copy cached in localStorage
 * for the first paint): missing or invalid values get defaults, like Go's Normalize.
 * Registered sections are coerced by their own feature (lib/settingsSections.ts).
 */
export function coerceSettings(v: unknown): AppSettings {
  const obj = isObject(v) ? v : {}
  return { ...coerceCore(obj), ...coerceSections(obj) }
}

function coerceCore(v: Record<string, unknown>): CoreSettings {
  const d = DEFAULT_SETTINGS
  const keymap: Record<string, string> = {}
  if (isObject(v.keymap)) {
    for (const [id, keys] of Object.entries(v.keymap)) if (typeof keys === 'string') keymap[id] = keys
  }
  return {
    version: typeof v.version === 'number' ? v.version : d.version,
    theme: v.theme === 'light' || v.theme === 'system' || v.theme === 'dark' || v.theme === 'custom' ? v.theme : d.theme,
    editor: {
      ...font(v.editor, d.editor),
      vimMode: isObject(v.editor) && typeof v.editor.vimMode === 'boolean' ? v.editor.vimMode : d.editor.vimMode,
    },
    terminal: font(v.terminal, d.terminal),
    customTheme: coerceCustomTheme(v.customTheme, d.customTheme),
    defaultAgent: typeof v.defaultAgent === 'string' ? v.defaultAgent : d.defaultAgent,
    autoSave: typeof v.autoSave === 'boolean' ? v.autoSave : d.autoSave,
    notifyInBackground: typeof v.notifyInBackground === 'boolean' ? v.notifyInBackground : d.notifyInBackground,
    keymap,
    watchExclude: Array.isArray(v.watchExclude) ? v.watchExclude.filter((p): p is string => typeof p === 'string' && validWatchExclude(p)) : d.watchExclude,
    agents: coerceAgents(v.agents),
    customAgents: coerceCustomAgents(v.customAgents),
    languageServers: coerceServers(v.languageServers),
    refresh: coerceRefresh(v.refresh),
    mcpServer: coerceMcpServer(v.mcpServer),
    completions: coerceCompletions(v.completions),
    inlineEdit: coerceInlineEdit(v.inlineEdit),
    agentTasks: coerceAgentTasks(v.agentTasks),
    usage: coerceUsage(v.usage),
    prompts: coercePromptSettings(v.prompts),
    agentReview: coerceAgentReview(v.agentReview),
    ci: coerceCI(v.ci),
    github: coerceGitHub(v.github),
  }
}
