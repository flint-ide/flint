// User settings (settings.json via SettingsAPI), applied live: the theme and editor
// fonts as attributes/variables on <html>, terminal fonts by the xterm views
// (useTerminalSettings), editors by lib/editorSetup, shortcuts by lib/commands.
//
// The last settings are cached in localStorage and applied when this module loads,
// before React's first paint, so a light theme doesn't flash dark on startup; the
// backend's copy replaces them a moment later.
import { createStore } from '../lib/store'
import * as SettingsAPI from '../../bindings/flint/settingsapi'
import { settings as models } from '../wails/models'
import { coerceSettings, DEFAULT_SETTINGS, editorCssVars, resolveTheme, themeCssVars, type AppSettings, type ResolvedTheme } from '../lib/settings'
import { setKeyOverrides } from '../lib/commands'
import { errorMessage } from '../api/backend'
import { showToast } from './dialog'
import { saveTab, useEditor } from './editor'

const CACHE_KEY = 'flint.settings'
const SAVE_DELAY_MS = 300 // typing a font size saves once

interface SettingsState {
  settings: AppSettings
  /** The backend's settings arrived (until then: the cached copy). */
  loaded: boolean
  /** macOS appearance, for the System theme. */
  systemDark: boolean
  /** The palette in use. */
  theme: ResolvedTheme
  /** Applies a change right away and saves it shortly after. */
  update: (change: Partial<AppSettings> | ((s: AppSettings) => Partial<AppSettings>)) => void
}

const darkQuery = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null
const initial = readCache()
const initialDark = darkQuery?.matches ?? true

export const useSettings = createStore<SettingsState>((set, get) => ({
  settings: initial,
  loaded: false,
  systemDark: initialDark,
  theme: resolved(initial, initialDark),

  update: (change) => {
    const prev = get().settings
    const settings = { ...prev, ...(typeof change === 'function' ? change(prev) : change) }
    set({ settings, theme: resolved(settings, get().systemDark) })
    scheduleSave()
  },
}))

/** The palette in use for these settings. */
function resolved(s: AppSettings, systemDark: boolean): ResolvedTheme {
  return resolveTheme(s.theme, systemDark, s.customTheme.base)
}

/**
 * Takes settings the backend just saved by another path (Import from VS Code):
 * pending edits are written first by the caller (flushSettings).
 */
export function replaceSettings(saved: unknown): void {
  const settings = coerceSettings(saved)
  lastSaved = settings
  writeCache(settings)
  useSettings.setState((s) => ({ settings, theme: resolved(settings, s.systemDark) }))
}

/** Fetches the settings from the backend (once, at startup). */
export async function loadSettings(): Promise<void> {
  try {
    const settings = coerceSettings(await SettingsAPI.Get())
    lastSaved = settings
    writeCache(settings)
    useSettings.setState((s) => ({ settings, loaded: true, theme: resolved(settings, s.systemDark) }))
  } catch (err) {
    useSettings.setState({ loaded: true })
    showToast(`Settings not loaded: ${errorMessage(err)}`, 'error')
  }
}

// ---------- saving ----------

let lastSaved: AppSettings = initial
let saveTimer: ReturnType<typeof setTimeout> | undefined
let saving: Promise<void> = Promise.resolve()

function scheduleSave(): void {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => void flushSettings(), SAVE_DELAY_MS)
}

/** Writes pending changes now (quitting). */
export function flushSettings(): Promise<void> {
  if (saveTimer === undefined) return saving
  clearTimeout(saveTimer)
  saveTimer = undefined
  const next = useSettings.getState().settings
  saving = saving.then(async () => {
    try {
      await SettingsAPI.Save(models.Settings.createFrom(next))
      lastSaved = next
      writeCache(next)
    } catch (err) {
      showToast(`Settings not saved: ${errorMessage(err)}`, 'error')
      // Back to what is on disk, unless the user changed something meanwhile.
      if (useSettings.getState().settings === next) {
        useSettings.setState((s) => ({ settings: lastSaved, theme: resolved(lastSaved, s.systemDark) }))
      }
    }
  })
  return saving
}

function readCache(): AppSettings {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    return raw ? coerceSettings(JSON.parse(raw)) : DEFAULT_SETTINGS
  } catch {
    return DEFAULT_SETTINGS
  }
}

function writeCache(s: AppSettings): void {
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify(s))
  } catch {
    // Storage blocked: the next start just paints with defaults first.
  }
}

// ---------- applying ----------

// Custom theme variables set on :root, removed again when the theme changes.
let customVars: string[] = []

function applyToDocument(s: AppSettings, theme: ResolvedTheme): void {
  const root = document.documentElement
  root.dataset.theme = theme
  for (const [name, value] of Object.entries(editorCssVars(s))) root.style.setProperty(name, value)
  const custom = themeCssVars(s)
  for (const name of customVars) if (!(name in custom)) root.style.removeProperty(name)
  for (const [name, value] of Object.entries(custom)) root.style.setProperty(name, value)
  customVars = Object.keys(custom)
}

if (typeof document !== 'undefined') {
  applyToDocument(initial, useSettings.getState().theme)
  setKeyOverrides(initial.keymap)
  useSettings.subscribe((s, prev) => {
    if (s.settings !== prev.settings || s.theme !== prev.theme) applyToDocument(s.settings, s.theme)
    if (s.settings.keymap !== prev.settings.keymap) setKeyOverrides(s.settings.keymap)
  })
  // NSApp's appearance drives prefers-color-scheme: the system's in System mode.
  darkQuery?.addEventListener('change', (e) =>
    useSettings.setState((s) => ({ systemDark: e.matches, theme: resolved(s.settings, e.matches) })),
  )
}

/** Saves modified files when the window loses focus, if the setting is on. */
export function subscribeAutoSave(): () => void {
  const onBlur = () => {
    if (!useSettings.getState().settings.autoSave) return
    for (const tab of useEditor.getState().tabs) {
      if (tab.kind === 'file' && tab.dirty) void saveTab(tab.id)
    }
  }
  window.addEventListener('blur', onBlur)
  return () => window.removeEventListener('blur', onBlur)
}
