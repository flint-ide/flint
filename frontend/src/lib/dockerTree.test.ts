import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { dockerTree, logsCommand, shellCommand, statusOf, type ComposeProject, type Container } from './dockerTree'

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

  it('builds shell commands only for valid names', () => {
    assert.equal(logsCommand('shop-db-1', 100), 'docker logs -f --tail 100 shop-db-1')
    assert.match(shellCommand('shop-db-1') ?? '', /^docker exec -it shop-db-1 sh -c /)
    for (const bad of ['a; rm -rf ~', '-it', '$(x)', 'a b', '']) {
      assert.equal(shellCommand(bad), null)
      assert.equal(logsCommand(bad), null)
    }
  })
})
