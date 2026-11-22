import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { dependencyNotes, priorityColor, repoNotes } from './cardParts'

describe('dependencyNotes', () => {
  it('keeps the order and maps each line to a tone', () => {
    const notes = dependencyNotes([
      { kind: 'problem', text: 'cycle', cls: 'error' },
      { kind: 'after', text: 'waits for A', cls: 'warn' },
      { kind: 'waiting', text: 'starts when A is done', cls: 'muted' },
    ])
    assert.deepEqual(
      notes.map((n) => [n.text, n.tone]),
      [
        ['cycle', 'danger'],
        ['waits for A', 'warn'],
        ['starts when A is done', 'faint'],
      ],
    )
  })
  it('puts the whole text in the tooltip', () => {
    assert.equal(dependencyNotes([{ kind: 'reason', text: 'Last start failed: x', cls: 'error' }])[0].title, 'Last start failed: x')
  })
  it('is empty without lines', () => {
    assert.deepEqual(dependencyNotes([]), [])
  })
})

describe('priorityColor', () => {
  it('marks only high', () => {
    assert.equal(priorityColor('high'), 'yellow')
    assert.equal(priorityColor('medium'), 'grey')
    assert.equal(priorityColor('low'), 'grey')
    assert.equal(priorityColor(''), 'grey')
  })
})

describe('repoNotes', () => {
  it('shows the URL and the checkout', () => {
    const notes = repoNotes({ url: 'git@x:a/b.git', folder: '/src/b' })
    assert.deepEqual(notes.map((n) => [n.text, n.tone ?? 'muted']), [
      ['git@x:a/b.git', 'muted'],
      ['/src/b', 'muted'],
    ])
  })
  it('says there is no checkout, as an error', () => {
    const [, folder] = repoNotes({ url: 'u', folder: '' })
    assert.equal(folder.text, 'no checkout on this computer')
    assert.equal(folder.tone, 'danger')
  })
})
