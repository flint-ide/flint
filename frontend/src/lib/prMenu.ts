// The right-click menu of a row in the Pull Requests view: what the pull
// request's tab offers in its header, without opening it first. Pure: the
// panel passes in what each item does, so the list of items is testable.
import type { DropdownItem } from '../components/ui/Dropdown'
import { modifierName } from './platform'

/** What the menu reads from a row of the list. */
export interface PullRequestMenuSubject {
  number: number
  url: string
  /** "OPEN", "CLOSED" or "MERGED". */
  state: string
}

export interface PullRequestMenuActions {
  open: () => void
  /** send: put it in the chat and send it right away (Alt held). */
  review: (send: boolean) => void
  address: (send: boolean) => void
  checkout: () => void
  openOnGitHub: () => void
  copy: (text: string) => void
}

export function pullRequestMenu(pr: PullRequestMenuSubject, a: PullRequestMenuActions): DropdownItem[] {
  const sends = `${modifierName('Alt')}-click sends`
  return [
    { label: `Open #${pr.number}`, onSelect: () => a.open() },
    { type: 'separator' },
    { label: '✦ Review with Agent', hint: sends, keywords: 'code review', onSelect: (e) => a.review(e.altKey) },
    { label: '✦ Address Review Comments', hint: sends, keywords: 'threads changes requested', onSelect: (e) => a.address(e.altKey) },
    { type: 'separator' },
    // Only an open pull request has a branch worth switching to.
    ...(pr.state === 'OPEN' ? [{ label: 'Check Out', onSelect: () => a.checkout() }] : []),
    { label: 'Open on GitHub', onSelect: () => a.openOnGitHub() },
    { type: 'separator' },
    { label: 'Copy Link', onSelect: () => a.copy(pr.url) },
    { label: 'Copy Number', onSelect: () => a.copy(`#${pr.number}`) },
  ]
}
