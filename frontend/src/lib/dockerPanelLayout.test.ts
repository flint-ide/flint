import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

// `.tree-row` is position:absolute (the virtualized file tree places rows itself).
// The Docker panel renders plain rows, so every one needs `static`; without it
// they all stack at the window's top-left, over the title bar.
describe('Docker panel rows', () => {
  it('are laid out in the sidebar flow', () => {
    const src = readFileSync('src/components/sidebar/DockerPanel.tsx', 'utf8')
    const rows = [...src.matchAll(/className=(?:"|\{`)(tree-row [^"`$]*)/g)].map((m) => m[1])
    assert.ok(rows.length >= 3, 'project, group and container rows')
    for (const r of rows) assert.match(r, /\bstatic\b/, r)
  })
})
