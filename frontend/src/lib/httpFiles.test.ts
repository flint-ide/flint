import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { bodyLanguagePath, envDir, formatBytes, formatTime, freeHttpFileName, isHttpFile, statusTone, suggestedFileName } from './httpFiles'

describe('http client helpers', () => {
  it('recognizes request files', () => {
    assert.ok(isHttpFile('/p/api.http'))
    assert.ok(isHttpFile('/p/API.REST'))
    assert.ok(!isHttpFile('/p/http.go'))
    assert.ok(!isHttpFile('/p/http'))
  })

  it('finds the folder of the environment files', () => {
    assert.equal(envDir('/p/api/users.http'), '/p/api')
    assert.equal(envDir('/users.http'), '/')
  })

  it('formats sizes and times', () => {
    assert.equal(formatBytes(512), '512 B')
    assert.equal(formatBytes(1536), '1.5 KB')
    assert.equal(formatBytes(5 * 1024 * 1024), '5.0 MB')
    assert.equal(formatTime(0.4), '<1 ms')
    assert.equal(formatTime(12.6), '13 ms')
    assert.equal(formatTime(1234), '1.23 s')
  })

  it('colours statuses by class', () => {
    assert.equal(statusTone(204), 'ok')
    assert.equal(statusTone(302), 'redirect')
    assert.equal(statusTone(404), 'client-error')
    assert.equal(statusTone(503), 'server-error')
    assert.equal(statusTone(101), 'info')
  })

  it('suggests a file name for saved bodies', () => {
    assert.equal(suggestedFileName('http://x.test/files/report.pdf?v=1', 'application/pdf', 'binary'), 'report.pdf')
    assert.equal(suggestedFileName('http://x.test/users/42', 'application/json', 'json'), '42.json')
    assert.equal(suggestedFileName('http://x.test/', 'text/html', 'html'), 'response.html')
    assert.equal(suggestedFileName('http://x.test/a b', '', 'binary'), 'a_b.bin')
    assert.equal(suggestedFileName('not a url', '', 'text'), 'response.txt')
  })

  it('maps body kinds to highlighting', () => {
    assert.equal(bodyLanguagePath('json'), 'body.json')
    assert.equal(bodyLanguagePath('xml'), 'body.xml')
    assert.equal(bodyLanguagePath('text'), 'body.txt')
  })

  it('picks a free name for a new request file', () => {
    assert.equal(freeHttpFileName(new Set()), 'requests.http')
    assert.equal(freeHttpFileName(new Set(['requests.http', 'requests-2.http'])), 'requests-3.http')
  })
})
