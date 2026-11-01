// The shape of settings.json (internal/settings in Go) and the pure parts of
// applying it: theme resolution and the CSS variables editors read. Framework-free,
// so it is testable with node:test; store/settings.ts does the applying.

import {
  coerceAgents, coerceRefresh, coerceServers, DEFAULT_REFRESH,
  type AgentSettings, type LanguageServerSettings, type RefreshSettings,
} from './toolSettings'
import { coerceCustomAgents, type CustomAgent } from './customAgents'
import {
  coerceCompletions, coerceInlineEdit, DEFAULT_COMPLETIONS, DEFAULT_INLINE_EDIT,
  type CompletionSettings, type InlineEditSettings,
} from './editorAiSettings'

export type ThemeMode = 'dark' | 'light' | 'system'
export type ResolvedTheme = 'dark' | 'light'

export interface FontSettings {
  fontFamily: string
  fontSize: number
  lineHeight: number
}

export interface AppSettings {
  version: number
  theme: ThemeMode
  editor: FontSettings
  terminal: FontSettings
  /** Agent for new chats (a built-in or custom agent's id); '' = the first installed one. */
  defaultAgent: string
  /** Save modified files when the window loses focus. */
  autoSave: boolean
  /** Notification and Dock badge when work finishes while Flint is in the background. */
  notifyInBackground: boolean
  /** Ask GitHub once a day whether a newer Flint release exists. */
  checkForUpdates: boolean
  /** Shortcut overrides: command id → keys ('' = none). */
  keymap: Record<string, string>
  /** Extra folders / globs the TODO list skips, gitignore-style. */
  todoExclude: string[]
  /** Agent CLIs by name ('claude', 'codex'); missing = defaults. */
  agents: Record<string, AgentSettings>
  /** Agents added by the user, spoken to over ACP. */
  customAgents: CustomAgent[]
  /** Language servers by language ('go', 'typescript', …); missing = defaults. */
  languageServers: Record<string, LanguageServerSettings>
  /** Polling intervals of the Kubernetes and AWS panels. */
  refresh: RefreshSettings
  /** Inline suggestions from a code model (lib/editorAiSettings.ts). */
  completions: CompletionSettings
  /** Edit with Agent on the selection. */
  inlineEdit: InlineEditSettings
}

/** Mirrors settings.Default() in Go (used until the backend answers). */
export const DEFAULT_SETTINGS: AppSettings = Object.freeze({
  version: 1,
  theme: 'dark',
  editor: Object.freeze({ fontFamily: `'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace`, fontSize: 13, lineHeight: 1.55 }),
  terminal: Object.freeze({ fontFamily: `"JetBrains Mono", "SF Mono", Menlo, monospace`, fontSize: 13, lineHeight: 1.2 }),
  defaultAgent: '',
  autoSave: false,
  notifyInBackground: true,
  checkForUpdates: true,
  keymap: Object.freeze({}) as Record<string, string>,
  todoExclude: Object.freeze([]) as unknown as string[],
  agents: Object.freeze({}) as Record<string, AgentSettings>,
  customAgents: Object.freeze([]) as unknown as CustomAgent[],
  languageServers: Object.freeze({}) as Record<string, LanguageServerSettings>,
  refresh: DEFAULT_REFRESH,
  completions: DEFAULT_COMPLETIONS,
  inlineEdit: DEFAULT_INLINE_EDIT,
}) as AppSettings

/** Same limits as Go's settings.Validate, for the inputs. */
export const FONT_SIZE = { min: 8, max: 32 }
export const LINE_HEIGHT = { min: 1, max: 2.5 }
export const MAX_EXCLUDES = 100

/** Mirrors Go's validExclude: rg reads a leading "!" as "include" and excludes travel comma-separated. */
export const validExclude = (p: string) => p !== '' && p.length <= 200 && !p.startsWith('!') && !/[,\x00-\x1f]/.test(p)

/** One pattern per line → the patterns to save (trimmed, unique) and the lines that are not valid. */
export function parseExcludes(text: string): { patterns: string[]; invalid: string[] } {
  const patterns: string[] = []
  const invalid: string[] = []
  for (const line of text.split('\n')) {
    const p = line.trim()
    if (!p || patterns.includes(p)) continue
    if (validExclude(p)) patterns.push(p)
    else invalid.push(p)
  }
  if (patterns.length > MAX_EXCLUDES) invalid.push(...patterns.splice(MAX_EXCLUDES))
  return { patterns, invalid }
}

/** The palette to use: System follows macOS (prefers-color-scheme). */
export function resolveTheme(mode: ThemeMode, systemDark: boolean): ResolvedTheme {
  if (mode === 'system') return systemDark ? 'dark' : 'light'
  return mode
}

/** CSS custom properties on :root that the editors read (style.css, lib/editorSetup.ts). */
export function editorCssVars(s: AppSettings): Record<string, string> {
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
 */
export function coerceSettings(v: unknown): AppSettings {
  if (!isObject(v)) return DEFAULT_SETTINGS
  const d = DEFAULT_SETTINGS
  const keymap: Record<string, string> = {}
  if (isObject(v.keymap)) {
    for (const [id, keys] of Object.entries(v.keymap)) if (typeof keys === 'string') keymap[id] = keys
  }
  return {
    version: typeof v.version === 'number' ? v.version : d.version,
    theme: v.theme === 'light' || v.theme === 'system' || v.theme === 'dark' ? v.theme : d.theme,
    editor: font(v.editor, d.editor),
    terminal: font(v.terminal, d.terminal),
    defaultAgent: typeof v.defaultAgent === 'string' ? v.defaultAgent : d.defaultAgent,
    autoSave: typeof v.autoSave === 'boolean' ? v.autoSave : d.autoSave,
    notifyInBackground: typeof v.notifyInBackground === 'boolean' ? v.notifyInBackground : d.notifyInBackground,
    checkForUpdates: typeof v.checkForUpdates === 'boolean' ? v.checkForUpdates : d.checkForUpdates,
    keymap,
    todoExclude: Array.isArray(v.todoExclude) ? v.todoExclude.filter((p): p is string => typeof p === 'string' && validExclude(p)) : [],
    agents: coerceAgents(v.agents),
    customAgents: coerceCustomAgents(v.customAgents),
    languageServers: coerceServers(v.languageServers),
    refresh: coerceRefresh(v.refresh),
    completions: coerceCompletions(v.completions),
    inlineEdit: coerceInlineEdit(v.inlineEdit),
  }
}
