import { describe, expect, it } from 'vitest'
import { detectIndent } from './indent'

describe('detectIndent', () => {
  it('always uses tabs for Go and Makefiles', () => {
    expect(detectIndent('package main\n\nfunc f() {\n  x()\n}\n', '/p/main.go')).toBe('\t')
    expect(detectIndent('', '/p/go.mod')).toBe('\t')
    expect(detectIndent('all:\n\tgo build\n', '/p/Makefile')).toBe('\t')
  })

  it('detects tab-indented files', () => {
    expect(detectIndent('a {\n\tb\n\t\tc\n}\n', '/p/x.js')).toBe('\t')
  })

  it('detects 2- and 4-space indentation', () => {
    expect(detectIndent('a:\n  b:\n    c: 1\n  d: 2\n', '/p/x.yaml')).toBe('  ')
    expect(detectIndent('def f():\n    if x:\n        y()\n    return 1\n', '/p/x.py')).toBe('    ')
  })

  it('falls back when there is no indentation', () => {
    expect(detectIndent('one\ntwo\n', '/p/x.txt')).toBe('  ')
    expect(detectIndent('', '/p/x.ts', '    ')).toBe('    ')
  })

  it('ignores blank lines and odd one-space alignment', () => {
    expect(detectIndent('/**\n * doc\n */\nfunction f() {\n    a()\n\n    b()\n}\n', '/p/x.ts')).toBe('    ')
  })
})
