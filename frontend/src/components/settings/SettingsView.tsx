import { useEffect, useState, type ReactNode } from 'react'
import { useSettings } from '../../store/settings'
import { useAgent } from '../../store/agent'
import { DEFAULT_SETTINGS, FONT_SIZE, LINE_HEIGHT, parseExcludes, type FontSettings, type ThemeMode } from '../../lib/settings'
import * as TodoAPI from '../../../bindings/flint/todoapi'
import { Select } from '../ui/Select'
import { KeymapSection } from './KeymapSection'

type Section = 'appearance' | 'editor' | 'terminal' | 'agents' | 'todo' | 'keymap'

const SECTIONS: { id: Section; label: string }[] = [
  { id: 'appearance', label: 'Appearance' },
  { id: 'editor', label: 'Editor' },
  { id: 'terminal', label: 'Terminal' },
  { id: 'agents', label: 'Agents' },
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

function AgentsSection() {
  const available = useAgent((s) => s.available)
  const loaded = useAgent((s) => s.loaded)
  const defaultAgent = useSettings((s) => s.settings.defaultAgent)
  const notify = useSettings((s) => s.settings.notifyInBackground)
  const update = useSettings((s) => s.update)
  useEffect(() => {
    if (!loaded) void useAgent.getState().load()
  }, [loaded])
  const first = available[0]
  // A default that is no longer installed still shows, so it can be changed back.
  const names = defaultAgent && !available.includes(defaultAgent) ? [...available, defaultAgent] : available
  return (
    <section>
      <h2>Agents</h2>
      <Row label="Default agent" hint="Starts new agent chats, writes commit messages and resolves conflicts.">
        {loaded && available.length === 0 ? (
          <span className="muted">No agent installed (claude or codex)</span>
        ) : (
          <Select
            value={defaultAgent}
            allowEmpty={first ? `First installed (${first})` : 'First installed'}
            options={names.map((a) => ({ value: a, label: a, hint: available.includes(a) ? undefined : 'not installed' }))}
            onChange={(a) => useAgent.getState().setPreferred(a)}
          />
        )}
      </Row>
      <h3>Notifications</h3>
      <Row label="Tell me when work finishes in the background" hint="A macOS notification and a count on the Dock icon when a run, tests or an agent finish while Flint isn't the active app.">
        <input type="checkbox" checked={notify} onChange={(e) => update({ notifyInBackground: e.target.checked })} />
      </Row>
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

function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="settings-row">
      <div className="settings-label">
        <span>{label}</span>
        {hint && <span className="muted">{hint}</span>}
      </div>
      <div className="settings-control">{children}</div>
    </div>
  )
}

/** A number input that saves valid values as you type and resets to the saved one on blur. */
function NumberField(props: { value: number; min: number; max: number; step: number; suffix?: string; onCommit: (v: number) => void }) {
  const { value, min, max, step, suffix, onCommit } = props
  const [draft, setDraft] = useState(String(value))
  useEffect(() => setDraft(String(value)), [value])
  const parse = (s: string) => {
    const n = Number(s)
    return s.trim() !== '' && Number.isFinite(n) && n >= min && n <= max ? n : null
  }
  const invalid = parse(draft) === null
  return (
    <span className="number-field">
      <input
        type="number"
        className={invalid ? 'invalid' : ''}
        value={draft}
        min={min}
        max={max}
        step={step}
        onChange={(e) => {
          setDraft(e.target.value)
          const n = parse(e.target.value)
          if (n !== null && n !== value) onCommit(n)
        }}
        onBlur={() => setDraft(String(value))}
      />
      {suffix && <span className="muted">{suffix}</span>}
      {invalid && <span className="error">{min}–{max}</span>}
    </span>
  )
}

/** A text input that saves on Enter or blur when valid. */
function TextField(props: { value: string; list?: string; valid: (v: string) => boolean; onCommit: (v: string) => void }) {
  const { value, list, valid, onCommit } = props
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])
  const commit = () => {
    if (draft !== value && valid(draft)) onCommit(draft)
    else setDraft(value)
  }
  return (
    <input
      className={`settings-text${valid(draft) ? '' : ' invalid'}`}
      value={draft}
      list={list}
      spellCheck={false}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit()
        if (e.key === 'Escape') setDraft(value)
      }}
    />
  )
}
