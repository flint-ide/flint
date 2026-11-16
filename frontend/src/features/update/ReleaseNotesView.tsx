import type { ContributedTab } from '../../lib/features'
import { coerceNotes, linkLabel, notesSections, releaseDate } from './notes'
import { openNotesLink } from './store'

/** Opens the link a button carries in data-url (one handler for every link of the page). */
const openLink = (e: React.MouseEvent<HTMLButtonElement>) => openNotesLink(e.currentTarget.dataset.url ?? '')

/** The "What's New" tab: new features, bug fixes and other changes of one release. */
export function ReleaseNotesView({ tab }: { tab: ContributedTab; active: boolean }) {
  const notes = coerceNotes(tab.data)
  if (!notes) return <p className="muted empty">Release notes are not available.</p>
  const sections = notesSections(notes)
  const date = releaseDate(notes.publishedAt)
  return (
    <div className="release-notes">
      <div className="release-notes-page">
        <h1>What&apos;s New in Flint {notes.tag}</h1>
        <p className="muted release-notes-meta">
          {date && <span>Released {date} · </span>}
          <button className="link" data-url={notes.url} onClick={openLink}>View on GitHub</button>
        </p>
        {sections.length === 0 && <p className="muted">This release has no notes.</p>}
        {sections.map((s) => (
          <section key={s.id} className={`release-notes-section ${s.id}`}>
            <h2>
              {s.title} <span className="badge">{s.items.length}</span>
            </h2>
            <ul>
              {s.items.map((item, i) => (
                <li key={i}>
                  <span>{item.text}</span>
                  {item.url && (
                    <button className="link release-notes-link" title={item.url} data-url={item.url} onClick={openLink}>
                      {linkLabel(item.url)}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </div>
  )
}
