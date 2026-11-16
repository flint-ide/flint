import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceNotes, linkLabel, notesSections, notesTitle, releaseDate, type ReleaseNotes } from './notes'

const notes = (over: Partial<ReleaseNotes> = {}): ReleaseNotes => ({
  tag: 'v1.3.0',
  name: 'Flint v1.3.0',
  url: 'https://github.com/flint-ide/flint/releases/tag/v1.3.0',
  publishedAt: '2026-10-30T09:15:00Z',
  features: [{ text: 'Release notes page', url: 'https://github.com/flint-ide/flint/pull/80' }],
  fixes: [{ text: 'Toast position', url: '' }],
  other: [],
  ...over,
})

describe('release notes', () => {
  it('reads notes from Go', () => {
    assert.deepEqual(coerceNotes(notes()), notes())
    assert.deepEqual(coerceNotes({ tag: 'v1.3.0', features: [{ text: 'A' }, { url: 'x' }, 'B', null], fixes: null }), {
      tag: 'v1.3.0',
      name: '',
      url: '',
      publishedAt: '',
      features: [{ text: 'A', url: '' }],
      fixes: [],
      other: [],
    })
  })

  it('rejects anything else', () => {
    for (const v of [null, undefined, 'v1.3.0', [], {}, { tag: '' }, { tag: 3 }]) {
      assert.equal(coerceNotes(v), null, JSON.stringify(v))
    }
  })

  it('titles the tab', () => {
    assert.equal(notesTitle(notes()), "What's New in v1.3.0")
  })

  it('lists features first and leaves empty sections out', () => {
    assert.deepEqual(notesSections(notes()).map((s) => [s.id, s.title, s.items.length]), [
      ['features', 'New Features', 1],
      ['fixes', 'Bug Fixes', 1],
    ])
    assert.deepEqual(notesSections(notes({ features: [], fixes: [], other: [{ text: 'Cleanup', url: '' }] })).map((s) => s.title), ['Other Changes'])
    assert.deepEqual(notesSections(notes({ features: [], fixes: [] })), [])
  })

  it('labels an entry link by its number', () => {
    assert.equal(linkLabel('https://github.com/flint-ide/flint/pull/80'), '#80')
    assert.equal(linkLabel('https://github.com/flint-ide/flint/issues/7'), '#7')
    assert.equal(linkLabel('https://github.com/flint-ide/flint/compare/v1.2.0...v1.3.0'), 'link')
  })

  it('formats the release date', () => {
    assert.equal(releaseDate('2026-10-30T09:15:00Z'), 'October 30, 2026')
    assert.equal(releaseDate('0001-01-01T00:00:00Z'), '')
    assert.equal(releaseDate(''), '')
    assert.equal(releaseDate('soon'), '')
  })
})
