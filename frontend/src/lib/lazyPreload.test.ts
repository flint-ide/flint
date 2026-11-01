import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { preloadAll, preloadable } from './lazyPreload'

describe('preloadable', () => {
  it('fetches once and then answers synchronously', async () => {
    let calls = 0
    const p = preloadable(async () => (calls++, { default: 'View' }))
    await Promise.all([p.fetch(), p.fetch()])
    assert.equal(calls, 1)
    let got: string | undefined
    void p.factory().then((m) => (got = m.default))
    assert.equal(got, 'View', 'resolved in the same tick, so React does not suspend')
  })

  it('shares an in-flight fetch with the first render', async () => {
    let calls = 0
    const p = preloadable(async () => (calls++, { default: 1 }))
    const a = p.fetch()
    const b = p.factory()
    assert.equal((await a).default, (await b).default)
    assert.equal(calls, 1)
  })

  it('tries again after a failed fetch', async () => {
    let calls = 0
    const p = preloadable(async () => {
      if (calls++ === 0) throw new Error('offline')
      return { default: 'ok' }
    })
    assert.equal(await p.fetch().then(() => 'ok', () => 'failed'), 'failed')
    assert.equal((await p.factory()).default, 'ok')
  })
})

describe('preloadAll', () => {
  it('loads in order, pausing before each, and skips failures', async () => {
    const log: string[] = []
    const wait = async () => void log.push('wait')
    await preloadAll(
      [async () => log.push('a'), async () => Promise.reject(new Error('x')), async () => log.push('c')],
      wait,
    )
    assert.deepEqual(log, ['wait', 'a', 'wait', 'wait', 'c'])
  })

  it('also loads components a fetched chunk registers meanwhile', async () => {
    const log: string[] = []
    const loaders: Array<() => Promise<unknown>> = []
    loaders.push(async () => {
      log.push('view')
      loaders.push(async () => log.push('preview')) // e.g. FileView registers MarkdownPreview
    })
    await preloadAll(loaders, async () => undefined)
    assert.deepEqual(log, ['view', 'preview'])
  })
})
