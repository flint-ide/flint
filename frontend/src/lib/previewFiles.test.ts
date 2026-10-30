import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { headingAt, headingLines, isImage, isMarkdown, resolveRelative } from './previewFiles'

describe('preview files', () => {
  it('recognizes images and Markdown', () => {
    assert.ok(isImage('/p/logo.PNG') && isImage('/p/a.svg') && !isImage('/p/a.ts'))
    assert.ok(isMarkdown('/p/README.md') && !isMarkdown('/p/md.go'))
  })

  it('resolves relative references against the file', () => {
    assert.equal(resolveRelative('/p/docs/a.md', 'img/x.png'), '/p/docs/img/x.png')
    assert.equal(resolveRelative('/p/docs/a.md', '../README.md#top'), '/p/README.md')
    assert.equal(resolveRelative('/p/docs/a.md', './my%20file.md'), '/p/docs/my file.md')
    for (const ref of ['https://x.dev', '#anchor', 'mailto:a@b', '//cdn.x/y.png', '']) assert.equal(resolveRelative('/p/a.md', ref), null)
  })

  it('finds heading lines outside code fences', () => {
    const src = ['# One', 'text', '```', '# not a heading', '```', 'Two', '===', '', '## Three', '- item', '---'].join('\n')
    assert.deepEqual(headingLines(src), [1, 6, 9])
  })

  it('maps a line to the heading above it', () => {
    assert.equal(headingAt([1, 6, 9], 7), 1)
    assert.equal(headingAt([3, 6], 1), -1)
    assert.equal(headingAt([1, 6, 9], 20), 2)
  })
})
