import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { parseExcludes, validWatchExclude } from '../../lib/settings'
import { isMac } from '../../lib/platform'
import { Row } from './fields'

/**
 * Folders the Linux file watcher skips. Each watched folder costs an inotify
 * watch, so build output and caches are left out; macOS's FSEvents watches a
 * whole tree at once and has nothing to configure.
 */
export function WatchSection() {
  const saved = useSettings((s) => s.settings.watchExclude.join('\n'))
  const update = useSettings((s) => s.update)
  const [draft, setDraft] = useState(saved)
  useEffect(() => setDraft(saved), [saved])
  if (isMac()) return null
  const { patterns, invalid } = parseExcludes(draft, validWatchExclude)
  const commit = () => {
    if (invalid.length === 0 && patterns.join('\n') !== saved) update({ watchExclude: patterns })
  }
  return (
    <section>
      <h2>File Watching</h2>
      <Row
        label="Folders not watched"
        hint="Folder names, one per line (globs like *.egg-info work). Changes inside them don't refresh the file tree on their own. Dependency folders (node_modules, .venv, Cargo's target…) are never watched. Applies to folders opened after the change."
      >
        <div className="settings-control settings-control-stack">
          <textarea
            className={`settings-textarea${invalid.length ? ' invalid' : ''}`}
            value={draft}
            spellCheck={false}
            placeholder={'dist\n.cache'}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commit}
          />
          {invalid.length > 0 && <span className="error">Not allowed: {invalid.join(', ')} (folder names only: no slashes, leading ! or commas)</span>}
        </div>
      </Row>
    </section>
  )
}
