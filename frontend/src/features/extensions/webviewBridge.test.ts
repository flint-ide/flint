import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import vm from 'node:vm'
import { WEBVIEW_CSP, parseFrameMessage, webviewDocument, type WebviewInit } from './webviewBridge'

const init: WebviewInit = { view: 'flint.database.console', key: 'c1', data: { sql: '</script><b>' }, theme: { vars: { '--fg': '#fff' }, dark: true } }

describe('webviewDocument', () => {
  it('puts the policy and the bridge first in <head>', () => {
    const doc = webviewDocument('<!doctype html><html><head><title>x</title></head><body></body></html>', init)
    const head = doc.indexOf('<head>') + '<head>'.length
    assert.ok(doc.startsWith(`<meta http-equiv="Content-Security-Policy" content="${WEBVIEW_CSP}">`, head))
    assert.ok(doc.indexOf('<script>') < doc.indexOf('<title>'))
    assert.match(WEBVIEW_CSP, /connect-src 'none'/)
  })

  it('adds a head when the file has none, after the doctype', () => {
    const doc = webviewDocument('<!DOCTYPE html><p>hi</p>', init)
    assert.ok(doc.startsWith('<!DOCTYPE html><head><meta http-equiv="Content-Security-Policy"'))
  })

  it('cannot be broken out of by the init data', () => {
    const doc = webviewDocument('<head></head>', init)
    const script = doc.slice(doc.indexOf('<script>'), doc.indexOf('</script>') + 9)
    assert.ok(!script.slice(8, -9).includes('</script>'), 'init data closed the script element')
  })
})

/** Runs the bridge of a document in a fake frame; returns the frame's window and what it posted. */
function runBridge() {
  const doc = webviewDocument('<head></head>', init)
  const code = doc.slice(doc.indexOf('<script>') + 8, doc.indexOf('</script>'))
  const posted: Record<string, unknown>[] = []
  const handlers: Record<string, ((e: unknown) => void)[]> = {}
  const parent = { postMessage: (m: Record<string, unknown>) => posted.push(m) }
  const styles: Record<string, string> = {}
  const attrs: Record<string, string> = {}
  const window: Record<string, unknown> = {
    parent,
    addEventListener: (type: string, fn: (e: unknown) => void) => void (handlers[type] ??= []).push(fn),
  }
  const document = { documentElement: { style: { setProperty: (k: string, v: string) => void (styles[k] = v) }, setAttribute: (k: string, v: string) => void (attrs[k] = v) } }
  vm.runInNewContext(code, { window, document, console, Promise, Map, Set, Object, JSON, setTimeout })
  const fire = (type: string, e: unknown) => handlers[type]?.forEach((fn) => fn(e))
  return { flint: window.flint as Record<string, (...a: unknown[]) => unknown> & { theme: unknown }, posted, fire, parent, styles, attrs }
}

describe('bridge', () => {
  it('relays requests and answers only from the parent', async () => {
    const { flint, posted, fire, parent } = runBridge()
    const answer = flint.request('schema', { db: 'x' }) as Promise<unknown>
    assert.deepEqual(JSON.parse(JSON.stringify(posted[0])), { type: 'request', id: 1, method: 'schema', params: { db: 'x' }, flint: 1 })
    fire('message', { source: {}, data: { flint: 1, type: 'response', id: 1, result: 'forged' } })
    fire('message', { source: parent, data: { flint: 1, type: 'response', id: 1, result: ['t'] } })
    assert.deepEqual(await answer, ['t'])
    const failing = flint.request('boom') as Promise<unknown>
    fire('message', { source: parent, data: { flint: 1, type: 'response', id: 2, error: 'no' } })
    await assert.rejects(failing, /no/)
  })

  it('applies the theme and delivers messages', () => {
    const { flint, fire, parent, styles, attrs } = runBridge()
    assert.equal(styles['--fg'], '#fff')
    assert.equal(attrs['data-theme'], 'dark')
    const got: unknown[] = []
    flint.onMessage((m: unknown) => got.push(m))
    fire('message', { source: parent, data: { flint: 1, type: 'message', message: { rows: 3 } } })
    fire('message', { source: parent, data: { flint: 1, type: 'theme', theme: { vars: { '--fg': '#000' }, dark: false } } })
    assert.deepEqual(got, [{ rows: 3 }])
    assert.equal(styles['--fg'], '#000')
    assert.equal(attrs['data-theme'], 'light')
  })

  it('forwards shortcuts the page left alone, not editing keys', async () => {
    const { posted, fire } = runBridge()
    const key = (k: string, mods: Record<string, boolean> = {}, defaultPrevented = false) =>
      fire('keydown', { key: k, code: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...mods, defaultPrevented })
    key('p', { metaKey: true, shiftKey: true })
    key('c', { metaKey: true })
    key('Enter', { metaKey: true }, true)
    key('a')
    key('Escape')
    await new Promise((r) => setTimeout(r, 5))
    assert.deepEqual(posted.map((m) => m.key), ['p', 'Escape'])
  })
})

describe('parseFrameMessage', () => {
  const ext = 'flint.database'
  it('keeps well-formed messages for the extension’s own ids', () => {
    assert.deepEqual(parseFrameMessage({ flint: 1, type: 'request', id: 3, method: 'run' }, ext), { type: 'request', id: 3, method: 'run', params: null })
    assert.ok(parseFrameMessage({ flint: 1, type: 'openTab', view: 'flint.database.console', key: 'k', title: 'SQL' }, ext))
    assert.ok(parseFrameMessage({ flint: 1, type: 'runCommand', id: 'flint.database.newConsole' }, ext))
  })

  it('refuses other extensions’ ids, oversize data and unknown types', () => {
    for (const m of [
      { flint: 1, type: 'openTab', view: 'acme.jira.issue', key: 'k', title: 'x' },
      { flint: 1, type: 'runCommand', id: 'git.push' },
      { flint: 1, type: 'setData', data: 'x'.repeat(70 << 10) },
      { flint: 1, type: 'request', id: 1.5, method: 'run' },
      { flint: 1, type: 'openFile', path: 'relative/path' },
      { flint: 1, type: 'eval', code: '1' },
      { type: 'request', id: 1, method: 'run' },
      null,
    ]) {
      assert.equal(parseFrameMessage(m, ext), null, JSON.stringify(m)?.slice(0, 60))
    }
  })
})
