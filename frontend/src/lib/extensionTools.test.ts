import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { setExtensionLanguages } from './extensionLanguages'
import { extensionDebuggerFor, extensionTestRunnerFor, fileLanguage, setExtensionTools } from './extensionTools'

describe('extension debuggers and test runners', () => {
  afterEach(() => {
    setExtensionLanguages([])
    setExtensionTools([])
  })

  it('finds the language of a file, Flint’s own first', () => {
    setExtensionLanguages([{ id: 'ruby', label: 'Ruby', extensions: ['.rb'], filenames: ['Rakefile'] }, { id: 'mygo', label: 'Go', extensions: ['.go'] }])
    assert.equal(fileLanguage('/p/app.rb'), 'ruby')
    assert.equal(fileLanguage('/p/Rakefile'), 'ruby')
    assert.equal(fileLanguage('/p/main.go'), 'go')
    assert.equal(fileLanguage('/p/a.TSX'), 'typescript')
    assert.equal(fileLanguage('/p/notes.txt'), undefined)
  })

  it('offers the first enabled extension’s tools, never for Flint’s own languages', () => {
    setExtensionLanguages([{ id: 'ruby', label: 'Ruby', extensions: ['.rb'] }])
    const rdbg = { label: 'Ruby (rdbg)', languages: ['ruby', 'go'] }
    const rspec = { label: 'RSpec', languages: ['ruby', 'typescript', 'python'] }
    setExtensionTools([
      { id: 'a.off', enabled: false, state: 'disabled', debuggers: [{ label: 'Off', languages: ['ruby'] }] },
      { id: 'b.broken', enabled: true, state: 'invalid', testRunners: [{ label: 'Broken', languages: ['ruby'] }] },
      { id: 'c.ruby', enabled: true, state: 'stopped', debuggers: [rdbg], testRunners: [rspec] },
      { id: 'd.other', enabled: true, state: 'stopped', debuggers: [{ label: 'Other', languages: ['ruby', 'python'] }] },
    ])
    assert.equal(extensionDebuggerFor('/p/app.rb'), rdbg)
    assert.equal(extensionDebuggerFor('/p/a.py')?.label, 'Other')
    assert.equal(extensionDebuggerFor('/p/main.go'), undefined)
    assert.equal(extensionTestRunnerFor('/p/app_spec.rb'), rspec)
    assert.equal(extensionTestRunnerFor('/p/a.test.ts'), undefined)
    assert.equal(extensionTestRunnerFor('/p/a.py'), rspec)
    assert.equal(extensionTestRunnerFor('/p/notes.txt'), undefined)
  })
})
