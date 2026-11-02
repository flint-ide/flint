import { Suspense, useMemo, useState } from 'react'
import { useSettings } from '../../store/settings'
import { DEFAULT_SETTINGS, FONT_SIZE, LINE_HEIGHT, type FontSettings, type ThemeMode } from '../../lib/settings'
import { byOrder, useContributions } from '../../lib/contributions'
import { KeymapSection } from './KeymapSection'
import { AgentsSection } from './AgentsSection'
import { LanguageServersSection } from './LanguageServersSection'
import { RefreshSection } from './RefreshSection'
import { WatchSection } from './WatchSection'
import { McpServerSection } from './McpServerSection'
import { EditorAiSection } from './EditorAiSection'
import { UsageSection } from './UsageSection'
import { CISection } from './CISection'
import { AccountsSection } from './AccountsSection'
import { NumberField, Row, TextField } from './fields'
import { openVSCodeImport } from '../../store/vscodeImport'
import { isMac } from '../../lib/platform'

type Section = 'appearance' | 'editor' | 'terminal' | 'accounts' | 'agents' | 'agentContext' | 'usage' | 'editorAi' | 'lsp' | 'refresh' | 'ci' | 'keymap' | 'import'

// Flint's own pages. Feature modules add theirs between them by order (lib/features.ts:
// Docker 110, TODO List 130, Updates 150), so keep the gaps.
const SECTIONS: { id: Section; label: string; order: number }[] = [
  { id: 'appearance', label: 'Appearance', order: 10 },
  { id: 'editor', label: 'Editor', order: 20 },
  { id: 'terminal', label: 'Terminal', order: 30 },
  { id: 'accounts', label: 'Accounts', order: 40 },
  { id: 'agents', label: 'Agents', order: 50 },
  { id: 'agentContext', label: 'Agent Context', order: 60 },
  { id: 'usage', label: 'Usage', order: 70 },
  { id: 'editorAi', label: 'AI in the Editor', order: 80 },
  { id: 'lsp', label: 'Language Servers', order: 90 },
  { id: 'refresh', label: 'Refresh', order: 100 },
  { id: 'ci', label: 'CI', order: 120 },
  { id: 'keymap', label: 'Keymap', order: 140 },
  { id: 'import', label: 'Import', order: 160 },
]

// Offered in the font pickers; any installed font can be typed instead.
const MONO_FONTS = ['JetBrains Mono', 'SF Mono', 'Menlo', 'Monaco', 'Fira Code', 'Cascadia Code', 'Source Code Pro', 'IBM Plex Mono', 'Courier New']

/** The Settings tab (⌘,). Every change applies and saves right away. */
export function SettingsView({ active }: { active: boolean }) {
  const [section, setSection] = useState<string>('appearance')
  const contributed = useContributions((s) => s.settingsPages)
  const pages = useMemo(() => byOrder<{ id: string; label: string; order: number }>([...SECTIONS, ...contributed]), [contributed])
  const page = contributed.find((p) => p.id === section)
  return (
    <div className="settings-view">
      <nav className="settings-nav" aria-label="Settings sections">
        {pages.map((s) => (
          <button key={s.id} className={section === s.id ? 'active' : ''} onClick={() => setSection(s.id)}>
            {s.label}
          </button>
        ))}
      </nav>
      <div className="settings-body">
        {section === 'appearance' && <AppearanceSection />}
        {section === 'editor' && <EditorSection />}
        {section === 'terminal' && <TerminalSection />}
        {section === 'accounts' && <AccountsSection />}
        {section === 'agents' && <AgentsSection />}
        {section === 'agentContext' && <McpServerSection />}
        {section === 'usage' && <UsageSection />}
        {section === 'editorAi' && <EditorAiSection />}
        {section === 'lsp' && <LanguageServersSection />}
        {section === 'refresh' && (
          <>
            <RefreshSection />
            <WatchSection />
          </>
        )}
        {section === 'ci' && <CISection />}
        {section === 'keymap' && <KeymapSection active={active} />}
        {section === 'import' && <ImportSection />}
        {page && (
          <Suspense fallback={null}>
            <page.component active={active} />
          </Suspense>
        )}
      </div>
    </div>
  )
}

function AppearanceSection() {
  const mode = useSettings((s) => s.settings.theme)
  const theme = useSettings((s) => s.theme)
  const custom = useSettings((s) => s.settings.customTheme)
  const update = useSettings((s) => s.update)
  const hasCustom = Object.keys(custom.colors).length > 0
  const options: { value: ThemeMode; label: string }[] = [
    { value: 'dark', label: 'Dark' },
    { value: 'light', label: 'Light' },
    { value: 'system', label: 'System' },
    ...(hasCustom || mode === 'custom' ? [{ value: 'custom' as const, label: 'Custom' }] : []),
  ]
  const hint =
    mode === 'system' ? `Follows ${isMac() ? 'macOS' : 'the system'} (now ${theme})`
    : mode === 'custom' ? `${custom.name || 'Imported theme'} on the ${custom.base} palette`
    : undefined
  return (
    <section>
      <h2>Appearance</h2>
      <Row label="Theme" hint={hint}>
        <div className="segmented" role="radiogroup" aria-label="Theme">
          {options.map((o) => (
            <button key={o.value} role="radio" aria-checked={mode === o.value} className={mode === o.value ? 'on' : ''} onClick={() => update({ theme: o.value })}>
              {o.label}
            </button>
          ))}
        </div>
      </Row>
    </section>
  )
}

function EditorSection() {
  const editor = useSettings((s) => s.settings.editor)
  const autoSave = useSettings((s) => s.settings.autoSave)
  const update = useSettings((s) => s.update)
  return (
    <section>
      <h2>Editor</h2>
      <FontRows font={editor} defaults={DEFAULT_SETTINGS.editor} onChange={(f) => update({ editor: { ...editor, ...f } })} />
      <pre className="settings-preview editor-preview">{`func greet(name string) string {\n\treturn "Hello, " + name // 0O 1lI {}[]\n}`}</pre>
      <h3>Saving</h3>
      <Row label="Save files when Flint loses focus" hint="Modified files are written when you switch to another app.">
        <input type="checkbox" checked={autoSave} onChange={(e) => update({ autoSave: e.target.checked })} />
      </Row>
      <h3>Keys</h3>
      <Row label="Vim mode" hint={`Vim key bindings in every editor; :w saves, :q closes the tab. ${isMac() ? "A focused Vim editor keeps Vim's ⌃ keys (⌃R redo, not Run)" : 'A focused Vim editor keeps Ctrl+R, Ctrl+D, Ctrl+U, Ctrl+V, Ctrl+A, Ctrl+X and Ctrl+['}; other Flint shortcuts keep working.`}>
        <input type="checkbox" checked={editor.vimMode} onChange={(e) => update({ editor: { ...editor, vimMode: e.target.checked } })} />
      </Row>
    </section>
  )
}

function ImportSection() {
  return (
    <section>
      <h2>Import</h2>
      <p className="muted settings-note">
        Bring your fonts, auto-save, keyboard shortcuts, color theme and Vim extension over from Visual Studio Code, Cursor or VSCodium. You see
        everything that would change, and what is skipped and why, before anything is saved.
      </p>
      <div className="settings-actions">
        <button onClick={() => void openVSCodeImport()}>Import from VS Code…</button>
      </div>
    </section>
  )
}

function TerminalSection() {
  const terminal = useSettings((s) => s.settings.terminal)
  const update = useSettings((s) => s.update)
  return (
    <section>
      <h2>Terminal</h2>
      <FontRows font={terminal} defaults={DEFAULT_SETTINGS.terminal} onChange={(f) => update({ terminal: f })} />
      <p className="muted settings-note">Applies to terminals and run consoles (one point smaller there).</p>
    </section>
  )
}

/** Family, size and line height of a monospace font. */
function FontRows({ font, defaults, onChange }: { font: FontSettings; defaults: FontSettings; onChange: (f: FontSettings) => void }) {
  const isDefault = font.fontFamily === defaults.fontFamily && font.fontSize === defaults.fontSize && font.lineHeight === defaults.lineHeight
  return (
    <>
      <Row label="Font family" hint="A CSS font list; the first installed font is used.">
        <TextField
          value={font.fontFamily}
          list="settings-mono-fonts"
          valid={(v) => v.trim() !== '' && !/[;{}<>\\]/.test(v)}
          onCommit={(v) => onChange({ ...font, fontFamily: v.trim() })}
        />
        <datalist id="settings-mono-fonts">
          {MONO_FONTS.map((f) => <option key={f} value={f} />)}
        </datalist>
      </Row>
      <Row label="Font size">
        <NumberField value={font.fontSize} min={FONT_SIZE.min} max={FONT_SIZE.max} step={1} suffix="px" onCommit={(v) => onChange({ ...font, fontSize: Math.round(v) })} />
      </Row>
      <Row label="Line height">
        <NumberField value={font.lineHeight} min={LINE_HEIGHT.min} max={LINE_HEIGHT.max} step={0.05} onCommit={(v) => onChange({ ...font, lineHeight: Math.round(v * 100) / 100 })} />
      </Row>
      <div className="settings-actions">
        <button
          disabled={isDefault}
          onClick={() => onChange({ fontFamily: defaults.fontFamily, fontSize: defaults.fontSize, lineHeight: defaults.lineHeight })}
        >
          Reset to Defaults
        </button>
      </div>
    </>
  )
}
