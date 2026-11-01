import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { dockerTree, statusOf, validContainerRef, rowClass, type ComposeProject, type Container } from './dockerTree'

const c = (name: string, state: string, project = '', service = '', workingDir = ''): Container =>
  ({ id: name, name, image: 'x', state, status: '', ports: [], created: '', project, service, configFiles: '', workingDir }) as unknown as Container

describe('docker tree', () => {
  it('groups containers under their compose project and keeps the rest apart', () => {
    const projects = [{ name: 'shop', dir: '/p/shop', files: ['/p/shop/compose.yaml'] }, { name: '', dir: '/p/api', files: ['/p/api/compose.yaml'] }] as ComposeProject[]
    const t = dockerTree([c('shop-web-1', 'running', 'shop', 'web', '/p/shop'), c('lonely', 'exited'), c('shop-db-1', 'exited', 'shop', 'db', '/p/shop'), c('up', 'running')], projects)
    assert.deepEqual(t.projects[0].containers.map((x) => x.name), ['shop-db-1', 'shop-web-1'])
    assert.equal(t.projects[0].running, 1)
    assert.equal(t.projects[1].containers.length, 0)
    assert.deepEqual(t.others.map((x) => x.name), ['up', 'lonely'])
  })

  it('reads health from the status', () => {
    assert.equal(statusOf({ state: 'running', status: 'Up 2 hours (unhealthy)' }), 'unhealthy')
    assert.equal(statusOf({ state: 'running', status: 'Up 2 hours (healthy)' }), 'running')
    assert.equal(statusOf({ state: 'exited', status: 'Exited (1)' }), 'stopped')
  })

  it('lays rows out in the sidebar flow, not absolutely over the title bar', () => {
    assert.equal(rowClass('docker-container', 'indent'), 'tree-row static docker-container indent')
    assert.equal(rowClass('docker-project', ''), 'tree-row static docker-project')
  })

  it('accepts only container ids and names the engine accepts', () => {
    for (const ok of ['shop-db-1', 'abc123', 'a_b.c']) assert.ok(validContainerRef(ok), ok)
    for (const bad of ['a; rm -rf ~', '-it', '$(x)', 'a b', '', '../x', 'a/b', 'a?b']) assert.ok(!validContainerRef(bad), bad)
  })
})
