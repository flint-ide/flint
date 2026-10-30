import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { detectIndent } from './indent'

describe('detectIndent', () => {
  it('always uses tabs for Go and Makefiles', () => {
    assert.equal(detectIndent('package main\n\nfunc f() {\n  x()\n}\n', '/p/main.go'), '\t')
    assert.equal(detectIndent('', '/p/go.mod'), '\t')
    assert.equal(detectIndent('all:\n\tgo build\n', '/p/Makefile'), '\t')
  })

  it('detects tab-indented files', () => {
    assert.equal(detectIndent('a {\n\tb\n\t\tc\n}\n', '/p/x.js'), '\t')
  })

  it('detects 2- and 4-space indentation', () => {
    assert.equal(detectIndent('a:\n  b:\n    c: 1\n  d: 2\n', '/p/x.yaml'), '  ')
    assert.equal(detectIndent('def f():\n    if x:\n        y()\n    return 1\n', '/p/x.py'), '    ')
  })

  it('falls back when there is no indentation', () => {
    assert.equal(detectIndent('one\ntwo\n', '/p/x.txt'), '  ')
    assert.equal(detectIndent('', '/p/x.ts', '    '), '    ')
  })

  it('ignores blank lines and odd one-space alignment', () => {
    assert.equal(detectIndent('/**\n * doc\n */\nfunction f() {\n    a()\n\n    b()\n}\n', '/p/x.ts'), '    ')
  })
})
