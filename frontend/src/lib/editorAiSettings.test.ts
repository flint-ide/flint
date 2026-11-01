import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { coerceCompletions, coerceInlineEdit, DEFAULT_COMPLETIONS, DEFAULT_INLINE_EDIT, validEndpoint, validModel } from './editorAiSettings'
import { coerceSettings } from './settings'

describe('completion settings', () => {
  it('are off by default and default when missing', () => {
    assert.equal(DEFAULT_COMPLETIONS.enabled, false)
    assert.deepEqual(coerceSettings({}).completions, DEFAULT_COMPLETIONS)
    assert.deepEqual(coerceSettings({}).inlineEdit, DEFAULT_INLINE_EDIT)
  })

  it('keep valid values and replace invalid ones', () => {
    const got = coerceCompletions({
      enabled: true, provider: 'gemini', endpoint: 'http://example.com', model: 'a b', debounceMs: 10, maxTokens: 128, contextLines: 'x',
    })
    assert.deepEqual(got, { ...DEFAULT_COMPLETIONS, enabled: true, maxTokens: 128 })
    assert.deepEqual(coerceInlineEdit({ contextLines: 0 }), { contextLines: 0 })
    assert.deepEqual(coerceInlineEdit({ contextLines: 9999 }), DEFAULT_INLINE_EDIT)
  })

  it('allow plain http only to this machine', () => {
    for (const e of ['http://localhost:11434', 'http://127.0.0.1:8080/', 'http://[::1]:1', 'https://api.example.com/v1']) {
      assert.equal(validEndpoint(e), true, e)
    }
    for (const e of ['http://example.com', 'ftp://localhost', 'https://u:p@example.com', 'https://example.com/?k=1', 'localhost:11434']) {
      assert.equal(validEndpoint(e), false, e)
    }
  })

  it('check model names', () => {
    assert.equal(validModel('qwen2.5-coder:1.5b-base'), true)
    assert.equal(validModel(''), false)
    assert.equal(validModel('a b'), false)
  })
})
