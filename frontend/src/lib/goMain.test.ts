import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { goMainLine, isGoProgramFile, mainConfigFor, relativeDir } from './goMain'

describe('goMainLine', () => {
  it('finds func main in package main', () => {
    assert.equal(goMainLine('package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println()\n}\n'), 5)
  })
  it('ignores other packages', () => {
    assert.equal(goMainLine('package server\n\nfunc main() {}\n'), null)
  })
  it('ignores a main method, a mainly-named func and an indented one', () => {
    assert.equal(goMainLine('package main\n\nfunc (s *S) main() {}\nfunc mainLoop() {}\n'), null)
    assert.equal(goMainLine('package main\n\n  func main() {}\n'), null)
  })
  it('tolerates spacing', () => {
    assert.equal(goMainLine('package main\nfunc main ( ) {}\n'), 2)
  })
})

describe('isGoProgramFile', () => {
  it('excludes tests and other languages', () => {
    assert.equal(isGoProgramFile('/p/main.go'), true)
    assert.equal(isGoProgramFile('/p/main_test.go'), false)
    assert.equal(isGoProgramFile('/p/main.ts'), false)
  })
})

describe('relativeDir', () => {
  it('is relative to the root', () => {
    assert.equal(relativeDir('/p', '/p/main.go'), '')
    assert.equal(relativeDir('/p', '/p/cmd/app/main.go'), 'cmd/app')
    assert.equal(relativeDir('/p', '/px/main.go'), null)
  })
})

describe('mainConfigFor', () => {
  const detected = [
    { id: 'detected:go:.', name: 'flint', type: 'go', package: '.' },
    { id: 'detected:go:./cmd/app', name: 'app', type: 'go', package: './cmd/app' },
  ]
  it('reuses the configuration of the package', () => {
    assert.equal(mainConfigFor('/p', '/p/cmd/app/main.go', detected)?.id, 'detected:go:./cmd/app')
    assert.equal(mainConfigFor('/p', '/p/main.go', detected)?.id, 'detected:go:.')
  })
  it('prefers a saved configuration listed first, written without ./', () => {
    const saved = [{ id: 'server', name: 'Server', type: 'go', package: 'cmd/app/' }, ...detected]
    assert.equal(mainConfigFor('/p', '/p/cmd/app/main.go', saved)?.id, 'server')
  })
  it('builds one in the file folder otherwise', () => {
    const c = mainConfigFor('/p', '/p/tools/gen/main.go', detected)
    assert.deepEqual(c, { id: 'detected:go-main:./tools/gen', name: 'gen', type: 'go', package: '.', cwd: 'tools/gen' })
  })
  it('names a root-level program after the folder', () => {
    assert.equal(mainConfigFor('/p/demo', '/p/demo/main.go', [])?.name, 'demo')
  })
  it('is null outside the project', () => {
    assert.equal(mainConfigFor('/p', '/q/main.go', detected), null)
  })
})
