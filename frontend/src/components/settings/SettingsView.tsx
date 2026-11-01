import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { DEFAULT_SETTINGS, FONT_SIZE, LINE_HEIGHT, parseExcludes, type FontSettings, type ThemeMode } from '../../lib/settings'
import * as TodoAPI from '../../../bindings/flint/todoapi'
import { KeymapSection } from './KeymapSection'
import { checkForUpdates, useUpdate } from '../../store/update'
import { AgentsSection } from './AgentsSection'
import { LanguageServersSection } from './LanguageServersSection'
import { RefreshSection } from './RefreshSection'
import { WatchSection } from './WatchSection'
import { McpServerSection } from './McpServerSection'
import { EditorAiSection } from './EditorAiSection'
import { UsageSection } from './UsageSection'
import { CISection } from './CISection'
import { AccountsSection } from './AccountsSection'
import { DockerSection } from './DockerSection'
import { NumberField, Row, TextField } from './fields'
import { openVSCodeImport } from '../../store/vscodeImport'
import { isMac } from '../../lib/platform'

type Section =
  | 'appearance' | 'editor' | 'terminal' | 'accounts' | 'agents' | 'agentContext' | 'usage' | 'editorAi' | 'lsp' | 'refresh' | 'docker' | 'ci' | 'todo'
  | 'keymap' | 'updates' | 'import'

const SECTIONS: { id: Section; label: string }[] = [
  { id: 'appearance', label: 'Appearance' },
  { id: 'editor', label: 'Editor' },
  { id: 'terminal', label: 'Terminal' },
  { id: 'accounts', label: 'Accounts' },
  { id: 'agents', label: 'Agents' },
  { id: 'agentContext', label: 'Agent Context' },
  { id: 'usage', label: 'Usage' },
  { id: 'editorAi', label: 'AI in the Editor' },
  { id: 'lsp', label: 'Language Servers' },
  { id: 'refresh', label: 'Refresh' },
  { id: 'docker', label: 'Docker' },
  { id: 'ci', label: 'CI' },
  { id: 'todo', label: 'TODO List' },
  { id: 'keymap', label: 'Keymap' },
  { id: 'updates', label: 'Updates' },
  { id: 'import', label: 'Import' },
]

// Offered in the font pickers; any installed font can be typed instead.
const MONO_FONTS = ['JetBrains Mono', 'SF Mono', 'Menlo', 'Monaco', 'Fira Code', 'Cascadia Code', 'Source Code Pro', 'IBM Plex Mono', 'Courier New']

/** The Settings tab (⌘,). Every change applies and saves right away. */
export function SettingsView({ active }: { active: boolean }) {
  const [section, setSection] = useState<Section>('appearance')
  return (
    <div className="settings-view">
      <nav className="settings-nav" aria-label="Settings sections">
        {SECTIONS.map((s) => (
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
        {section === 'docker' && <DockerSection />}
        {section === 'ci' && <CISection />}
        {section === 'todo' && <TodoSection />}
        {section === 'keymap' && <KeymapSection active={active} />}
        {section === 'updates' && <UpdatesSection />}
        {section === 'import' && <ImportSection />}
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

function UpdatesSection() {
  const enabled = useSettings((s) => s.settings.checkForUpdates)
  const checking = useUpdate((s) => s.checking)
  const update = useSettings((s) => s.update)
  return (
    <section>
      <h2>Updates</h2>
      <Row label="Check for new releases" hint="Once a day Flint asks GitHub which release is the latest and shows a notice in the status bar when there is a newer one.">
        <input type="checkbox" checked={enabled} onChange={(e) => update({ checkForUpdates: e.target.checked })} />
      </Row>
      <div className="settings-actions">
        <button disabled={checking} onClick={() => void checkForUpdates()}>{checking ? 'Checking…' : 'Check Now'}</button>
      </div>
      <p className="muted settings-note">
        The check is one anonymous request to api.github.com for the latest release of flint-ide/flint, with Flint's version in the User-Agent and no
        account, token, cookie or identifier. Flint never downloads or installs anything itself: Download opens the release page in your browser.
        With the check off, GitHub is asked only when you choose Check Now or Help ▸ Check for Updates….
      </p>
    </section>
  )
}

function TodoSection() {
  const saved = useSettings((s) => s.settings.todoExclude.join('\n'))
  const update = useSettings((s) => s.update)
  const [draft, setDraft] = useState(saved)
  const [auto, setAuto] = useState<string[]>([])
  useEffect(() => setDraft(saved), [saved])
  useEffect(() => {
    TodoAPI.AutoExcluded().then(setAuto, () => setAuto([]))
  }, [])
  const { patterns, invalid } = parseExcludes(draft)
  const commit = () => {
    if (invalid.length === 0 && patterns.join('\n') !== saved) update({ todoExclude: patterns })
  }
  return (
    <section>
      <h2>TODO List</h2>
      <Row label="Excluded folders" hint="One per line, as in .gitignore: docs/legacy, /generated, *.min.js. Saved when you leave the field.">
        <div className="settings-control settings-control-stack">
          <textarea
            className={`settings-textarea${invalid.length ? ' invalid' : ''}`}
            value={draft}
            spellCheck={false}
            placeholder={'docs/legacy\n*.min.js'}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commit}
          />
          {invalid.length > 0 && <span className="error">Not allowed: {invalid.join(', ')} (no leading ! or commas)</span>}
        </div>
      </Row>
      <h3>Always skipped</h3>
      <p className="muted settings-note">
        Dependency, cache and build folders at any depth{auto.length ? `: ${auto.join(', ')}` : ''}. Also Python environments of any name (a folder
        with pyvenv.cfg or conda-meta, e.g. .env), folders tagged with CACHEDIR.TAG (Cargo's target, tool caches), and target, build or _build next to
        Cargo.toml, pom.xml, build.gradle or mix.exs. Files ignored by .gitignore are skipped too.
      </p>
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
