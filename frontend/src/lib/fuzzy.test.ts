import { describe, expect, it } from 'vitest'
import { highlight, prepare, score, search } from './fuzzy'

const files = [
  'internal/db/store.go',
  'internal/db/store_test.go',
  'frontend/src/store/editor.ts',
  'frontend/src/components/editor/FileView.tsx',
  'README.md',
  'api_workspace.go',
  'frontend/src/components/sidebar/SearchPanel.tsx',
].map(prepare)

describe('fuzzy search', () => {
  it('matches subsequences and rejects non-matches', () => {
    expect(score('stgo', prepare('internal/db/store.go'))).not.toBeNull()
    expect(score('xyz', prepare('internal/db/store.go'))).toBeNull()
  })

  it('prefers base-name and boundary matches', () => {
    const [first] = search('fileview', files, 5)
    expect(first.target).toBe('frontend/src/components/editor/FileView.tsx')
  })

  it('ranks a shorter exact file name above its test sibling', () => {
    const results = search('store.go', files, 5).map((r) => r.target)
    expect(results[0]).toBe('internal/db/store.go')
  })

  it('handles camelCase initials', () => {
    const [first] = search('SP', files, 3)
    expect(first.target).toBe('frontend/src/components/sidebar/SearchPanel.tsx')
  })

  it('returns everything (score 0) for an empty query and honours the limit', () => {
    expect(search('', files, 3)).toHaveLength(3)
  })

  it('reports matched indices that spell the query', () => {
    const r = score('rdm', prepare('README.md'))!
    expect(r.indices.map((i) => 'README.md'[i].toLowerCase()).join('')).toBe('rdm')
  })
})

describe('highlight', () => {
  it('wraps runs of matches and escapes HTML', () => {
    const r = score('ab', prepare('<a>b'))!
    expect(highlight(r)).toBe('&lt;<b>a</b>&gt;<b>b</b>')
  })
})
