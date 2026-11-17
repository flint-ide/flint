import { it } from 'node:test'
import assert from 'node:assert/strict'
import { pullRequestMenu, type PullRequestMenuActions } from './prMenu'

const pr = (state = 'OPEN') => ({ number: 7, title: 'Add rate limiting', url: 'https://example.test/acme/widgets/pull/7', state, headRef: 'feat/rate-limit' })

function recorder() {
  const calls: string[] = []
  const actions: PullRequestMenuActions = {
    open: () => calls.push('open'),
    review: (send) => calls.push(`review:${send}`),
    address: (send) => calls.push(`address:${send}`),
    checkout: () => calls.push('checkout'),
    openOnGitHub: () => calls.push('github'),
    copy: (text) => calls.push(`copy:${text}`),
  }
  return { calls, actions }
}

const labels = (state?: string) =>
  pullRequestMenu(pr(state), recorder().actions).map((i) => ('label' in i ? i.label : '—'))

it('offers what the pull request tab offers, for an open pull request', () => {
  assert.deepEqual(labels(), [
    'Open #7',
    '—',
    '✦ Review with Agent',
    '✦ Address Review Comments',
    '—',
    'Check Out',
    'Open on GitHub',
    '—',
    'Copy Link',
    'Copy Number',
  ])
})

it('leaves out Check Out for a closed or merged pull request', () => {
  assert.ok(!labels('MERGED').includes('Check Out'))
  assert.ok(!labels('CLOSED').includes('Check Out'))
  assert.ok(labels('MERGED').includes('✦ Review with Agent'))
})

it('runs the action of the chosen item, sending right away with Alt', () => {
  const { calls, actions } = recorder()
  const items = pullRequestMenu(pr(), actions)
  const select = (label: string, altKey = false) => {
    const item = items.find((i) => 'label' in i && i.label === label)
    assert.ok(item && 'onSelect' in item, label)
    item.onSelect({ altKey, metaKey: false })
  }
  select('Open #7')
  select('✦ Review with Agent')
  select('✦ Review with Agent', true)
  select('✦ Address Review Comments', true)
  select('Check Out')
  select('Open on GitHub')
  select('Copy Link')
  select('Copy Number')
  assert.deepEqual(calls, ['open', 'review:false', 'review:true', 'address:true', 'checkout', 'github', 'copy:https://example.test/acme/widgets/pull/7', 'copy:#7'])
})
