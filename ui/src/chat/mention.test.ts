import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { applyMention, flatMentions, groupMentions, highlightRange, isImeKey, mentionAt, type MentionItem, mentionKeyAction, mentionQueryAt, wrapIndex } from './mention'

const at = (text: string) => mentionQueryAt(text, text.length)

describe('mentionQueryAt: where a mention starts', () => {
  it('opens at the start of the text', () => {
    assert.deepEqual(at('@src'), { start: 0, end: 4, query: 'src' })
  })
  it('opens on a bare trigger with an empty query', () => {
    assert.deepEqual(at('@'), { start: 0, end: 1, query: '' })
    assert.deepEqual(at('look at @'), { start: 8, end: 9, query: '' })
  })
  it('opens after a space, a newline, a tab and a non-breaking space', () => {
    assert.deepEqual(at('see @ap'), { start: 4, end: 7, query: 'ap' })
    assert.deepEqual(at('first line\n@ap'), { start: 11, end: 14, query: 'ap' })
    assert.deepEqual(at('a\t@ap'), { start: 2, end: 5, query: 'ap' })
    assert.deepEqual(at('a @ap'), { start: 2, end: 5, query: 'ap' })
  })
  it('opens after an opening bracket or quote', () => {
    assert.deepEqual(at('(see @x'), { start: 5, end: 7, query: 'x' })
    assert.deepEqual(at('(@x'), { start: 1, end: 3, query: 'x' })
    assert.deepEqual(at('[@x'), { start: 1, end: 3, query: 'x' })
    assert.deepEqual(at('"@x'), { start: 1, end: 3, query: 'x' })
  })
  it('never opens in the middle of a word or an e-mail address', () => {
    assert.equal(at('foo@bar'), null)
    assert.equal(at('write to user@example.com'), null)
    assert.equal(mentionQueryAt('user@example.com', 5), null)
    assert.equal(at('@@x'), null)
  })
})

describe('mentionQueryAt: the caret', () => {
  const text = 'see @src/app.ts now'
  it('is not in a mention before the trigger', () => {
    assert.equal(mentionQueryAt(text, 4), null)
    assert.equal(mentionQueryAt(text, 0), null)
  })
  it('right after the trigger gives an empty query that still spans the token', () => {
    assert.deepEqual(mentionQueryAt(text, 5), { start: 4, end: 15, query: '' })
  })
  it('inside the token: the query runs to the caret, the range to the end of the token', () => {
    assert.deepEqual(mentionQueryAt(text, 8), { start: 4, end: 15, query: 'src' })
  })
  it('at the end of the token gives the whole token', () => {
    assert.deepEqual(mentionQueryAt(text, 15), { start: 4, end: 15, query: 'src/app.ts' })
  })
  it('after the space that ends the token is not in a mention', () => {
    assert.equal(mentionQueryAt(text, 16), null)
  })
  it('a non-collapsed selection is never a mention', () => {
    assert.equal(mentionQueryAt(text, { start: 5, end: 8 }), null)
    assert.deepEqual(mentionQueryAt(text, { start: 8, end: 8 }), { start: 4, end: 15, query: 'src' })
  })
  it('an out-of-range caret is not a mention', () => {
    assert.equal(mentionQueryAt('@a', -1), null)
    assert.equal(mentionQueryAt('@a', 3), null)
    assert.equal(mentionQueryAt('@a', Number.NaN), null)
  })
  it('finds the mention under the caret when the text has several', () => {
    const two = '@a and @bc'
    assert.deepEqual(mentionQueryAt(two, 2), { start: 0, end: 2, query: 'a' })
    assert.deepEqual(mentionQueryAt(two, 10), { start: 7, end: 10, query: 'bc' })
    assert.equal(mentionQueryAt(two, 5), null)
  })
})

describe('mentionQueryAt: what a query may hold', () => {
  it('takes path characters', () => {
    assert.deepEqual(at('@src/a-b_c.d:12'), { start: 0, end: 15, query: 'src/a-b_c.d:12' })
  })
  it('takes unicode letters, Polish diacritics and decomposed marks', () => {
    assert.deepEqual(at('@żółw'), { start: 0, end: 5, query: 'żółw' })
    assert.deepEqual(at('zobacz @Zażółć'), { start: 7, end: 14, query: 'Zażółć' })
    assert.deepEqual(at('@文件'), { start: 0, end: 3, query: '文件' })
    const nfd = '@zó'
    assert.deepEqual(at(nfd), { start: 0, end: 4, query: 'zó' })
  })
  it('keeps letters outside the BMP whole on both sides of the caret', () => {
    assert.deepEqual(mentionQueryAt('@a𠀀b c', 2), { start: 0, end: 5, query: 'a' })
    assert.deepEqual(at('@𠀀'), { start: 0, end: 3, query: '𠀀' })
  })
  it('ends at whitespace, so a path with a space is not one query', () => {
    assert.equal(at('@my file'), null)
    assert.deepEqual(mentionQueryAt('@my file', 3), { start: 0, end: 3, query: 'my' })
  })
  it('is not a mention when other punctuation sits before the caret', () => {
    assert.equal(at('@foo,'), null)
    assert.equal(at('@foo!'), null)
  })
  it('stops the token at punctuation after the caret', () => {
    assert.deepEqual(mentionQueryAt('@foo, bar', 2), { start: 0, end: 4, query: 'f' })
  })
  it('accepts another single-character trigger and rejects an invalid one', () => {
    assert.deepEqual(mentionQueryAt('fix #12', 7, '#'), { start: 4, end: 7, query: '12' })
    assert.equal(mentionQueryAt('fix @12', 7, '#'), null)
    assert.equal(mentionQueryAt('@a', 2, ''), null)
    assert.equal(mentionQueryAt('@a', 2, '@@'), null)
    assert.equal(mentionQueryAt(' a', 2, ' '), null)
  })
})

describe('applyMention', () => {
  it('replaces the token at the very end and adds one space', () => {
    assert.deepEqual(applyMention('hi @sr', { start: 3, end: 6 }, '@src/app.ts'), { text: 'hi @src/app.ts ', caret: 15 })
  })
  it('does not double a space that already follows, and puts the caret after it', () => {
    assert.deepEqual(applyMention('hi @sr there', { start: 3, end: 6 }, '@src'), { text: 'hi @src there', caret: 8 })
  })
  it('adds a space before a newline instead of jumping over it', () => {
    assert.deepEqual(applyMention('@sr\nnext', { start: 0, end: 3 }, '@src'), { text: '@src \nnext', caret: 5 })
  })
  it('replaces the rest of the token after the caret too', () => {
    const text = 'see @src/ap.ts now'
    const token = mentionQueryAt(text, 8)
    assert.ok(token)
    assert.deepEqual(applyMention(text, token, '@src/app.ts'), { text: 'see @src/app.ts now', caret: 16 })
  })
  it('removes the token without leaving a double space when the insert is empty', () => {
    assert.deepEqual(applyMention('hi @sr there', { start: 3, end: 6 }, ''), { text: 'hi there', caret: 3 })
    assert.deepEqual(applyMention('@sr', { start: 0, end: 3 }, ''), { text: '', caret: 0 })
    assert.deepEqual(applyMention('@sr there', { start: 0, end: 3 }, ''), { text: 'there', caret: 0 })
    assert.deepEqual(applyMention('hi @sr', { start: 3, end: 6 }, ''), { text: 'hi ', caret: 3 })
  })
  it('clamps an out-of-range range and orders a reversed one', () => {
    assert.deepEqual(applyMention('@a', { start: 0, end: 99 }, '@abc'), { text: '@abc ', caret: 5 })
    assert.deepEqual(applyMention('@a', { start: 2, end: 0 }, '@abc'), { text: '@abc ', caret: 5 })
  })
  it('works on unicode text', () => {
    assert.deepEqual(applyMention('zobacz @żó', { start: 7, end: 10 }, '@żółw.md'), { text: 'zobacz @żółw.md ', caret: 16 })
  })
})

const item = (id: string, label: string, group = 'Files', extra: Partial<MentionItem> = {}): MentionItem => ({ id, label, group, ...extra })
const labels = (items: readonly MentionItem[]) => items.map((i) => i.label)

describe('groupMentions: ranking', () => {
  const items = [
    item('1', 'lib/mybutton.ts'),
    item('2', 'button/index.ts'),
    item('3', 'src/components/Button.tsx'),
    item('4', 'docs/readme.md', 'Files', { detail: 'about the button' }),
    item('5', 'docs/button-guide.md'),
    item('6', 'src/other.ts'),
  ]
  it('prefers the last path segment, then the label prefix, then a substring, then detail/keywords', () => {
    const [files] = groupMentions(items, 'button')
    assert.deepEqual(labels(files.matches.map((m) => m.item)), [
      'src/components/Button.tsx',
      'docs/button-guide.md',
      'button/index.ts',
      'lib/mybutton.ts',
      'docs/readme.md',
    ])
    assert.deepEqual(
      files.matches.map((m) => m.rank),
      [0, 0, 1, 2, 3],
    )
  })
  it('keeps the input order within a rank', () => {
    const [files] = groupMentions([item('a', 'b/app.ts'), item('b', 'a/app.ts')], 'app')
    assert.deepEqual(labels(files.matches.map((m) => m.item)), ['b/app.ts', 'a/app.ts'])
  })
  it('matches keywords, case-insensitively', () => {
    const [g] = groupMentions([item('k', 'Terminal', 'Open tabs', { keywords: ['SHELL', 'zsh'] })], 'shell')
    assert.equal(g.matches[0].rank, 3)
  })
  it('folds case and diacritics on both sides', () => {
    const list = [item('z', 'notes/Zażółć gęślą.md'), item('w', 'żółw.ts')]
    assert.deepEqual(labels(flatMentions(groupMentions(list, 'zazolc'))), ['notes/Zażółć gęślą.md'])
    assert.deepEqual(labels(flatMentions(groupMentions(list, 'ŻÓŁ'))), ['żółw.ts', 'notes/Zażółć gęślą.md'])
    assert.deepEqual(labels(flatMentions(groupMentions(list, 'gesla'))), ['notes/Zażółć gęślą.md'])
  })
  it('returns nothing when nothing matches', () => {
    assert.deepEqual(groupMentions(items, 'zzz'), [])
    assert.deepEqual(groupMentions([], ''), [])
  })
})

describe('groupMentions: groups', () => {
  const items = [
    item('t1', 'App.tsx', 'Open tabs'),
    item('f1', 'src/app.ts'),
    item('s1', 'AppState', 'Symbols'),
    item('f2', 'src/apple.ts'),
    item('t2', 'readme.md', 'Open tabs'),
  ]
  it('orders groups by first appearance and drops empty ones', () => {
    const groups = groupMentions(items, 'app')
    assert.deepEqual(
      groups.map((g) => g.name),
      ['Open tabs', 'Files', 'Symbols'],
    )
    assert.deepEqual(
      groupMentions(items, 'readme').map((g) => g.name),
      ['Open tabs'],
    )
  })
  it('limits each group and gives each its flat offset', () => {
    const groups = groupMentions(items, 'app', 1)
    assert.deepEqual(
      groups.map((g) => [g.name, g.offset, g.matches.length]),
      [
        ['Open tabs', 0, 1],
        ['Files', 1, 1],
        ['Symbols', 2, 1],
      ],
    )
  })
  it('treats a zero, negative or fractional limit sensibly', () => {
    assert.deepEqual(groupMentions(items, '', 0), [])
    assert.deepEqual(groupMentions(items, '', -3), [])
    assert.equal(flatMentions(groupMentions(items, '', 1.9)).length, 3)
  })
  it('lists everything in input order for an empty query', () => {
    assert.deepEqual(labels(flatMentions(groupMentions(items, ''))), ['App.tsx', 'readme.md', 'src/app.ts', 'src/apple.ts', 'AppState'])
  })
  it('keeps every item in order when ranking is off (results already filtered by a search)', () => {
    const groups = groupMentions([item('x', 'zzz'), item('y', 'src/app.ts')], 'app', Infinity, { rank: false })
    assert.deepEqual(labels(flatMentions(groups)), ['zzz', 'src/app.ts'])
    assert.equal(groups[0].matches[0].hit, null)
    assert.deepEqual(groups[0].matches[1].hit, { start: 4, end: 7 })
  })
  it('mentionAt finds an item by its flat index across groups', () => {
    const groups = groupMentions(items, 'app')
    assert.equal(mentionAt(groups, 0)?.id, 't1')
    assert.equal(mentionAt(groups, 2)?.id, 'f2')
    assert.equal(mentionAt(groups, 3)?.id, 's1')
    assert.equal(mentionAt(groups, 4), undefined)
    assert.equal(mentionAt(groups, -1), undefined)
  })
})

describe('highlightRange', () => {
  it('marks the last segment for a segment match, the start for a prefix, else the first occurrence', () => {
    assert.deepEqual(highlightRange('src/app/app.ts', 'app'), { start: 8, end: 11 })
    assert.deepEqual(highlightRange('button/index.ts', 'butt'), { start: 0, end: 4 })
    assert.deepEqual(highlightRange('lib/mybutton.ts', 'butt'), { start: 6, end: 10 })
  })
  it('maps folded matches back to the original characters', () => {
    assert.deepEqual(highlightRange('Zażółć.md', 'zol'), { start: 2, end: 5 })
    const nfd = 'zólw.ts'
    assert.deepEqual(highlightRange(nfd, 'ol'), { start: 1, end: 4 })
  })
  it('is null for an empty query or no match in the label', () => {
    assert.equal(highlightRange('abc', ''), null)
    assert.equal(highlightRange('abc', 'x'), null)
  })
})

describe('wrapIndex', () => {
  it('wraps at both ends', () => {
    assert.equal(wrapIndex(0, -1, 3), 2)
    assert.equal(wrapIndex(2, 1, 3), 0)
    assert.equal(wrapIndex(1, 1, 3), 2)
  })
  it('is 0 for an empty list and recovers from an out-of-range index', () => {
    assert.equal(wrapIndex(4, 1, 0), 0)
    assert.equal(wrapIndex(7, 1, 3), 0)
    assert.equal(wrapIndex(-5, 0, 3), 0)
  })
})

describe('mentionKeyAction', () => {
  const key = (k: string, mods: Partial<Parameters<typeof mentionKeyAction>[0]> = {}) => ({ key: k, ...mods })
  it('maps the arrows (and Ctrl+N/P) to moves, Enter and Tab to pick, Escape to close', () => {
    assert.equal(mentionKeyAction(key('ArrowDown'), true), 'next')
    assert.equal(mentionKeyAction(key('ArrowUp'), true), 'prev')
    assert.equal(mentionKeyAction(key('n', { ctrlKey: true }), true), 'next')
    assert.equal(mentionKeyAction(key('p', { ctrlKey: true }), true), 'prev')
    assert.equal(mentionKeyAction(key('Enter'), true), 'pick')
    assert.equal(mentionKeyAction(key('Tab'), true), 'pick')
    assert.equal(mentionKeyAction(key('Escape'), true), 'close')
  })
  it('leaves modified Enter/Tab/arrows alone (newline, focus back, word moves)', () => {
    assert.equal(mentionKeyAction(key('Enter', { shiftKey: true }), true), null)
    assert.equal(mentionKeyAction(key('Enter', { metaKey: true }), true), null)
    assert.equal(mentionKeyAction(key('Tab', { shiftKey: true }), true), null)
    assert.equal(mentionKeyAction(key('ArrowDown', { altKey: true }), true), null)
    assert.equal(mentionKeyAction(key('ArrowUp', { metaKey: true }), true), null)
  })
  it('lets moves and picks through when there is nothing to pick, but still closes on Escape', () => {
    assert.equal(mentionKeyAction(key('Enter'), false), null)
    assert.equal(mentionKeyAction(key('Tab'), false), null)
    assert.equal(mentionKeyAction(key('ArrowDown'), false), null)
    assert.equal(mentionKeyAction(key('Escape'), false), 'close')
  })
  it('ignores every key of an IME composition, including keyCode 229 from WebKit', () => {
    assert.equal(mentionKeyAction(key('Enter', { isComposing: true }), true), null)
    assert.equal(mentionKeyAction(key('Enter', { keyCode: 229 }), true), null)
    assert.equal(mentionKeyAction(key('Escape', { isComposing: true }), true), null)
  })
  it('ignores other keys', () => {
    assert.equal(mentionKeyAction(key('a'), true), null)
    assert.equal(mentionKeyAction(key('ArrowLeft'), true), null)
  })
})

describe('mentionKeyAction while results are pending (a search running, or rows of an older query)', () => {
  const key = (k: string, mods: Partial<Parameters<typeof mentionKeyAction>[0]> = {}) => ({ key: k, ...mods })
  it('holds Enter, Tab and the arrows so nothing is sent or inserted yet', () => {
    for (const k of ['Enter', 'Tab', 'ArrowDown', 'ArrowUp']) assert.equal(mentionKeyAction(key(k), true, true), 'wait', k)
    assert.equal(mentionKeyAction(key('Enter'), false, true), 'wait')
    assert.equal(mentionKeyAction(key('n', { ctrlKey: true }), false, true), 'wait')
  })
  it('still closes on Escape and leaves modified keys and IME keys alone', () => {
    assert.equal(mentionKeyAction(key('Escape'), false, true), 'close')
    assert.equal(mentionKeyAction(key('Enter', { shiftKey: true }), false, true), null)
    assert.equal(mentionKeyAction(key('Enter', { keyCode: 229 }), false, true), null)
    assert.equal(mentionKeyAction(key('a'), false, true), null)
  })
})

describe('isImeKey', () => {
  it('is true during a composition and for the commit key WebKit sends after compositionend', () => {
    assert.equal(isImeKey({ key: 'Enter', isComposing: true }), true)
    assert.equal(isImeKey({ key: 'Enter', isComposing: false, keyCode: 229 }), true)
  })
  it('is false for an ordinary key', () => {
    assert.equal(isImeKey({ key: 'Enter', isComposing: false, keyCode: 13 }), false)
    assert.equal(isImeKey({ key: 'a' }), false)
  })
})

describe('groupMentions on a large list', () => {
  const groupNames = ['Files', 'Symbols', 'Open tabs']
  const many: readonly MentionItem[] = Array.from({ length: 20_000 }, (_, i) => item(`i${i}`, `dir${i % 50}/file${i}.ts`, groupNames[i % 3]))
  it('keeps every item, grouped in first-appearance order, for an empty query', () => {
    const groups = groupMentions(many, '')
    assert.deepEqual(
      groups.map((g) => [g.name, g.offset, g.matches.length]),
      [
        ['Files', 0, 6667],
        ['Symbols', 6667, 6667],
        ['Open tabs', 13334, 6666],
      ],
    )
    assert.equal(groups[1].matches[0].item.id, 'i1')
    assert.equal(groups[1].matches[6666].item.id, 'i19999')
  })
  it('filters, ranks and limits correctly', () => {
    const groups = groupMentions(many, 'file1999', 4)
    // i1999 and i19990–i19999 all match on the last segment; Symbols has five of them, cut to four.
    assert.deepEqual(
      flatMentions(groups).map((i) => i.id),
      ['i19992', 'i19995', 'i19998', 'i1999', 'i19990', 'i19993', 'i19996', 'i19991', 'i19994', 'i19997'],
    )
  })
})
