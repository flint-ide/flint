import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { DEFAULT_SETTINGS, FONT_SIZE, LINE_HEIGHT, parseExcludes, type FontSettings, type ThemeMode } from '../../lib/settings'
import * as TodoAPI from '../../../bindings/flint/todoapi'
import { KeymapSection } from './KeymapSection'
import { AgentsSection } from './AgentsSection'
import { LanguageServersSection } from './LanguageServersSection'
import { RefreshSection } from './RefreshSection'
import { NumberField, Row, TextField } from './fields'

type Section = 'appearance' | 'editor' | 'terminal' | 'agents' | 'lsp' | 'refresh' | 'todo' | 'keymap'

const SECTIONS: { id: Section; label: string }[] = [
  { id: 'appearance', label: 'Appearance' },
  { id: 'editor', label: 'Editor' },
  { id: 'terminal', label: 'Terminal' },
  { id: 'agents', label: 'Agents' },
  { id: 'lsp', label: 'Language Servers' },
  { id: 'refresh', label: 'Refresh' },
  { id: 'todo', label: 'TODO List' },
  { id: 'keymap', label: 'Keymap' },
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
        {section === 'agents' && <AgentsSection />}
        {section === 'lsp' && <LanguageServersSection />}
        {section === 'refresh' && <RefreshSection />}
        {section === 'todo' && <TodoSection />}
        {section === 'keymap' && <KeymapSection active={active} />}
      </div>
    </div>
  )
}

function AppearanceSection() {
  const mode = useSettings((s) => s.settings.theme)
  const theme = useSettings((s) => s.theme)
  const update = useSettings((s) => s.update)
  const options: { value: ThemeMode; label: string }[] = [
    { value: 'dark', label: 'Dark' },
    { value: 'light', label: 'Light' },
    { value: 'system', label: 'System' },
  ]
  return (
    <section>
      <h2>Appearance</h2>
      <Row label="Theme" hint={mode === 'system' ? `Follows macOS (now ${theme})` : undefined}>
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
      <FontRows font={editor} defaults={DEFAULT_SETTINGS.editor} onChange={(f) => update({ editor: f })} />
      <pre className="settings-preview editor-preview">{`func greet(name string) string {\n\treturn "Hello, " + name // 0O 1lI {}[]\n}`}</pre>
      <h3>Saving</h3>
      <Row label="Save files when Flint loses focus" hint="Modified files are written when you switch to another app.">
        <input type="checkbox" checked={autoSave} onChange={(e) => update({ autoSave: e.target.checked })} />
      </Row>
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
        <button disabled={isDefault} onClick={() => onChange(defaults)}>Reset to Defaults</button>
      </div>
    </>
  )
}
