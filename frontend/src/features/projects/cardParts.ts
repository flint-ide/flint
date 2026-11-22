// Pure helpers that turn project data into the parts of the kit's TaskCard (no DOM, unit tested).
import type { TaskCardNote } from '@flint-ide/ui'
import type { DependencyLine } from './projects'

type Tone = NonNullable<TaskCardNote['tone']>

// A dependency that is merely done reads quieter than the card's meta, as it always has.
const TONES: Record<DependencyLine['cls'], Tone> = { muted: 'faint', warn: 'warn', error: 'danger' }

/** The notes under an item's meta line: what it waits for, its dependency problem, its waiting start. */
export function dependencyNotes(lines: readonly DependencyLine[]): TaskCardNote[] {
  return lines.map((l) => ({ text: l.text, title: l.text, tone: TONES[l.cls] }))
}

/** The priority badge's colour: only high stands out. */
export function priorityColor(priority: string): 'yellow' | 'grey' {
  return priority === 'high' ? 'yellow' : 'grey'
}

/** The two lines of a repository: its URL and its checkout here (an error while there is none). */
export function repoNotes(repo: { url: string; folder: string }): TaskCardNote[] {
  return [
    { text: repo.url, title: repo.url, truncate: true },
    repo.folder
      ? { text: repo.folder, title: repo.folder, truncate: true }
      : { text: 'no checkout on this computer', tone: 'danger', truncate: true },
  ]
}
