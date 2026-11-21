import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { confirmVariant, splitConfirm } from './confirmText'

describe('splitConfirm', () => {
  it('makes the first question the title and the rest the description', () => {
    assert.deepEqual(splitConfirm('Delete connection "prod"? Its password is removed from the keychain.'), {
      title: 'Delete connection "prod"?',
      description: 'Its password is removed from the keychain.',
    })
  })
  it('keeps line breaks in the description', () => {
    assert.deepEqual(splitConfirm('Trust this folder?\nTasks in it can run commands.\nOnly trust code you know.'), {
      title: 'Trust this folder?',
      description: 'Tasks in it can run commands.\nOnly trust code you know.',
    })
  })
  it('is all title when the question comes last', () => {
    assert.deepEqual(splitConfirm('The table is large. Run EXPLAIN ANALYZE of “select…”?'), {
      title: 'The table is large. Run EXPLAIN ANALYZE of “select…”?',
    })
  })
  it('is all title without a question', () => {
    assert.deepEqual(splitConfirm('  The repository moved.  '), { title: 'The repository moved.' })
  })
  it('is all title when the question is too long for one', () => {
    const long = `${'x'.repeat(130)}? More.`
    assert.deepEqual(splitConfirm(long), { title: long })
  })
})

describe('confirmVariant', () => {
  it('is destructive for labels that lose something', () => {
    for (const l of ['Delete', 'Revert', 'Clear History', 'Discard Changes', 'Force Push']) assert.equal(confirmVariant(l), 'destructive', l)
  })
  it('is primary for labels that go ahead', () => {
    for (const l of ['OK', 'Trust Folder', 'Choose Folder…', 'Explain Analyze', 'Install']) assert.equal(confirmVariant(l), 'primary', l)
  })
})
