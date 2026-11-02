import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { parseExcludes } from '../../lib/settings'
import * as TodoAPI from '../../../bindings/flint/internal/features/todo/api'
import { Row } from '../../components/settings/fields'

/** Settings ▸ TODO List: folders the scan skips. */
export function TodoSection() {
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
