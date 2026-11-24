// The @-mention picker's state, wired to any <textarea>:
//
//   const mention = useMentionPicker({ value, onChange: setValue, items })
//   <textarea {...mention.textareaProps} />
//   <MentionPicker picker={mention} anchor={mention.ref} />
//
// With a keydown handler of your own, call the picker's first, and skip IME keys
// (WebKit sends the composition's commit Enter after compositionend, with
// isComposing false and keyCode 229, so checking isComposing alone sends it):
//   onKeyDown={(e) => {
//     if (mention.onKeyDown(e)) return
//     if (e.key === 'Enter' && !e.shiftKey && !isImeKey({ key: e.key, keyCode: e.keyCode, isComposing: e.nativeEvent.isComposing })) send()
//   }}
import {
  type ChangeEvent,
  type KeyboardEvent,
  type RefObject,
  type SyntheticEvent,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  applyMention,
  flatMentions,
  groupMentions,
  type MentionGroup,
  type MentionItem,
  type MentionKey,
  mentionKeyAction,
  mentionQueryAt,
  type MentionRange,
  type MentionToken,
  wrapIndex,
} from './mention'

/** An async source: called (debounced) with the query; abort `signal` when it is stale. */
export type MentionSearch<T extends MentionItem> = (query: string, signal: AbortSignal) => Promise<readonly T[]>

/**
 * What `onSelect` returns:
 * - `undefined`/`true`: insert the default text (`item.insert`, else trigger + label);
 * - a string: insert that instead; `''` removes the typed token (attach a chip instead);
 * - `false`: leave the text as it is and close the picker for this token.
 */
export type MentionSelectResult = void | boolean | string

export type UseMentionPickerOptions<T extends MentionItem> = {
  /** The textarea's text (controlled). */
  readonly value: string
  readonly onChange: (value: string) => void
  /** A synchronous list, filtered and ranked by the picker. */
  readonly items?: readonly T[]
  /**
   * An async source instead of `items`; its results are grouped in the order given,
   * not re-ranked. It is read through a ref: a new function (or one that now reads
   * another source) is used from the next query on, so the rows on screen stay the
   * old source's until the query changes.
   */
  readonly search?: MentionSearch<T>
  /** Called before the text changes; see MentionSelectResult. */
  readonly onSelect?: (item: T, token: MentionToken) => MentionSelectResult
  /** One character, '@' by default. */
  readonly trigger?: string
  /** Rows per group, 8 by default. */
  readonly limitPerGroup?: number
  /** Delay before `search` runs, 120 ms by default. */
  readonly debounceMs?: number
  /** The textarea's ref, when the caller already has one. */
  readonly ref?: RefObject<HTMLTextAreaElement | null>
}

type TextareaHandler = (e: SyntheticEvent<HTMLTextAreaElement>) => void

/** Props to spread on the textarea (ChatComposer passes them through). */
export type MentionTextareaProps = {
  readonly ref: RefObject<HTMLTextAreaElement | null>
  readonly value: string
  readonly onChange: (e: ChangeEvent<HTMLTextAreaElement>) => void
  readonly onKeyDown: (e: KeyboardEvent<HTMLTextAreaElement>) => boolean
  readonly onSelect: TextareaHandler
  readonly onClick: TextareaHandler
  readonly onKeyUp: TextareaHandler
  readonly onFocus: TextareaHandler
  readonly onBlur: TextareaHandler
  readonly onCompositionStart: TextareaHandler
  readonly onCompositionEnd: TextareaHandler
  readonly role: 'combobox'
  readonly 'aria-expanded': boolean
  readonly 'aria-haspopup': 'listbox'
  readonly 'aria-autocomplete': 'list'
  readonly 'aria-controls': string | undefined
  readonly 'aria-activedescendant': string | undefined
}

/** The popup's state, what MentionPicker renders. */
export type MentionPickerState<T extends MentionItem = MentionItem> = {
  readonly open: boolean
  readonly query: string
  readonly groups: readonly MentionGroup<T>[]
  /** The rows in display order; activeIndex points into it (-1 when empty). */
  readonly items: readonly T[]
  readonly activeIndex: number
  readonly setActiveIndex: (index: number) => void
  readonly select: (item: T) => void
  /** Closes the popup until the token under the caret changes or the textarea is focused or clicked again. */
  readonly close: () => void
  /** A search is pending: debouncing, running, or its rows are for an older query. */
  readonly loading: boolean
  /** The rows shown belong to an older query (a search is pending); they cannot be picked. */
  readonly stale: boolean
  readonly error: Error | null
  readonly listId: string
  readonly optionId: (index: number) => string
  readonly ref: RefObject<HTMLTextAreaElement | null>
}

export type UseMentionPickerResult<T extends MentionItem = MentionItem> = MentionPickerState<T> & {
  readonly textareaProps: MentionTextareaProps
  /** The textarea's keydown: true when the picker used the key (it is then preventDefault-ed). */
  readonly onKeyDown: (e: KeyboardEvent<HTMLTextAreaElement>) => boolean
}

type SearchState<T> = { readonly query: string | null; readonly items: readonly T[]; readonly loading: boolean; readonly error: Error | null }

const IDLE: SearchState<never> = { query: null, items: [], loading: false, error: null }
const NO_ITEMS: readonly never[] = []

function useLatest<V>(value: V): RefObject<V> {
  const ref = useRef(value)
  useLayoutEffect(() => {
    ref.current = value
  })
  return ref
}

function toError(reason: unknown): Error {
  return reason instanceof Error ? reason : new Error(String(reason))
}

async function runSearch<T extends MentionItem>(search: MentionSearch<T>, query: string, signal: AbortSignal): Promise<readonly T[]> {
  const found = await search(query, signal)
  if (!Array.isArray(found)) throw new TypeError('mention search did not return an array')
  return found
}

/** Debounced, abortable search; results of an older query never replace newer ones. */
function useSearch<T extends MentionItem>(query: string | null, search: MentionSearch<T> | undefined, debounceMs: number): SearchState<T> {
  const [state, setState] = useState<SearchState<T>>(IDLE)
  const latest = useLatest(search)
  const seq = useRef(0)
  const enabled = search !== undefined
  useEffect(() => {
    const id = ++seq.current
    if (query === null || !enabled) {
      setState((s) => (s === IDLE ? s : IDLE))
      return undefined
    }
    const controller = new AbortController()
    const current = () => id === seq.current && !controller.signal.aborted
    setState((s) => (s.loading && s.error === null ? s : { ...s, loading: true, error: null }))
    const timer = setTimeout(() => {
      const fn = latest.current
      if (!fn) return
      runSearch(fn, query, controller.signal).then(
        (items) => current() && setState({ query, items, loading: false, error: null }),
        // A failure settles the query too: no rows, so Enter goes back to the textarea.
        (reason: unknown) => current() && setState({ query, items: [], loading: false, error: toError(reason) }),
      )
    }, Math.max(0, debounceMs))
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [query, enabled, debounceMs, latest])
  return state
}

/** The caret as last read from the textarea, with the text it was read from. */
type Caret = MentionRange & { readonly text: string }

function sameCaret(a: Caret | null, b: Caret): boolean {
  return a !== null && a.start === b.start && a.end === b.end && a.text === b.text
}

/** The caret/selection, focus and IME state of the textarea. */
function useTextareaState(onChange: RefObject<(value: string) => void>, reopen: () => void) {
  const [selection, setSelection] = useState<Caret | null>(null)
  const [focused, setFocused] = useState(false)
  const [composing, setComposing] = useState(false)
  const track = useCallback((e: SyntheticEvent<HTMLTextAreaElement>) => {
    const el = e.currentTarget
    const next = { start: el.selectionStart, end: el.selectionEnd, text: el.value }
    setSelection((s) => (sameCaret(s, next) ? s : next))
  }, [])
  const handlers = useMemo(
    () => ({
      onChange: (e: ChangeEvent<HTMLTextAreaElement>) => {
        track(e)
        onChange.current(e.currentTarget.value)
      },
      onSelect: track,
      onClick: (e: SyntheticEvent<HTMLTextAreaElement>) => {
        reopen()
        track(e)
      },
      onKeyUp: track,
      onFocus: (e: SyntheticEvent<HTMLTextAreaElement>) => {
        reopen()
        setFocused(true)
        track(e)
      },
      onBlur: () => setFocused(false),
      onCompositionStart: () => setComposing(true),
      onCompositionEnd: (e: SyntheticEvent<HTMLTextAreaElement>) => {
        setComposing(false)
        track(e)
      },
    }),
    [track, onChange, reopen],
  )
  return { selection, setSelection, focused, composing, handlers }
}

type Pending = { readonly from: string; readonly text: string; readonly caret: number }

/** Puts the caret after an inserted mention once the new value has rendered. */
function useCaretAfterInsert(ref: RefObject<HTMLTextAreaElement | null>, value: string) {
  const pending = useRef<Pending | null>(null)
  useLayoutEffect(() => {
    const p = pending.current
    if (!p) return
    if (value !== p.text && value !== p.from) pending.current = null
    const el = ref.current
    if (value !== p.text || !el || el.value !== p.text) return
    pending.current = null
    el.setSelectionRange(p.caret, p.caret)
  })
  return pending
}

function keyOf(e: KeyboardEvent<HTMLTextAreaElement>): MentionKey {
  const { key, shiftKey, ctrlKey, altKey, metaKey, keyCode } = e
  return { key, shiftKey, ctrlKey, altKey, metaKey, keyCode, isComposing: e.nativeEvent.isComposing }
}

function tokenKey(token: MentionToken | null): string | null {
  return token ? `${token.start}\u0000${token.query}` : null
}

/** The active row, reset to the first whenever the results change. */
function useActive(resultsKey: string, total: number) {
  const [active, setActive] = useState<{ readonly key: string; readonly index: number }>({ key: '', index: 0 })
  const index = total === 0 ? -1 : active.key === resultsKey && active.index < total ? active.index : 0
  const set = useCallback((i: number) => setActive({ key: resultsKey, index: i }), [resultsKey])
  return [index, set] as const
}

type InsertDeps<T extends MentionItem> = {
  readonly value: string
  readonly token: MentionToken | null
  readonly trigger: string
  readonly onSelect: RefObject<UseMentionPickerOptions<T>['onSelect']>
  readonly onChange: RefObject<(value: string) => void>
  readonly pending: RefObject<Pending | null>
  readonly setCaret: (caret: Caret) => void
  readonly dismiss: () => void
}

/** Runs onSelect, then replaces the token as its result says (see MentionSelectResult). */
function insertMention<T extends MentionItem>(item: T, d: InsertDeps<T>): void {
  if (!d.token) return
  const result = d.onSelect.current?.(item, d.token)
  if (result === false) return d.dismiss()
  const insert = typeof result === 'string' ? result : (item.insert ?? d.trigger + item.label)
  const next = applyMention(d.value, d.token, insert)
  d.pending.current = { from: d.value, ...next }
  d.setCaret({ start: next.caret, end: next.caret, text: next.text })
  d.onChange.current(next.text)
}

function comboboxAria(open: boolean, listId: string, activeId: string | undefined) {
  return {
    role: 'combobox',
    'aria-expanded': open,
    'aria-haspopup': 'listbox',
    'aria-autocomplete': 'list',
    'aria-controls': open ? listId : undefined,
    'aria-activedescendant': open ? activeId : undefined,
  } as const
}

/** Esc (or a press outside) closes the popup for one token; any other token, or a new focus or click, opens it again. */
function useDismissal() {
  const [dismissed, setDismissed] = useState<string | null>(null)
  const reopen = useCallback(() => setDismissed(null), [])
  return { dismissed, setDismissed, reopen }
}

/** The token under the caret and whether the popup shows for it. */
function useToken(value: string, trigger: string, ta: ReturnType<typeof useTextareaState>, d: ReturnType<typeof useDismissal>) {
  // A caret read from another text (the value changed from outside, e.g. cleared after send) means nothing.
  const token = ta.selection && ta.selection.text === value ? mentionQueryAt(value, ta.selection, trigger) : null
  const key = tokenKey(token)
  // Forget a dismissal as soon as the caret is in another token (or none): React re-renders at once.
  if (d.dismissed !== null && d.dismissed !== key) d.setDismissed(null)
  const open = token !== null && ta.focused && !ta.composing && d.dismissed !== key
  const { setDismissed } = d
  const close = useCallback(() => setDismissed(key), [setDismissed, key])
  return { token, key, open, close }
}

/**
 * State and textarea wiring for an @-mention popup. It opens while the caret is
 * in a mention token of a focused textarea (never during an IME composition),
 * and only then uses keys: up/down (and Ctrl+N/P) move with wrap-around, Enter
 * and Tab insert the active row, Esc closes until the token changes. Those keys
 * are preventDefault-ed. While a search is pending (debouncing, running, or its
 * rows are an older query's) they are swallowed without effect, so a fast Enter
 * neither sends the text nor picks a stale row; once a search has settled with no
 * rows, Enter and Tab pass through to the textarea again.
 */
export function useMentionPicker<T extends MentionItem>(options: UseMentionPickerOptions<T>): UseMentionPickerResult<T> {
  const { value, items, search, trigger = '@', limitPerGroup = 8, debounceMs = 120 } = options
  const ownRef = useRef<HTMLTextAreaElement | null>(null)
  const ref = options.ref ?? ownRef
  const onChange = useLatest(options.onChange)
  const onSelect = useLatest(options.onSelect)
  const dismissal = useDismissal()
  const ta = useTextareaState(onChange, dismissal.reopen)
  const pending = useCaretAfterInsert(ref, value)
  const { token, key, open, close } = useToken(value, trigger, ta, dismissal)
  const query = token?.query ?? ''
  const found = useSearch(open ? query : null, search, debounceMs)
  const waiting = open && search !== undefined && (found.loading || found.query !== query)
  const source = search ? found.items : (items ?? NO_ITEMS)
  const groups = useMemo(() => (open ? groupMentions(source, query, limitPerGroup, { rank: !search }) : []), [open, source, query, limitPerGroup, search])
  const flat = useMemo(() => flatMentions(groups), [groups])
  const [activeIndex, setActiveIndex] = useActive(`${key}\u0000${search ? found.query : ''}`, flat.length)
  const listId = `fui-mention-${useId()}`
  const optionId = useCallback((i: number) => `${listId}-${i}`, [listId])
  // Stale rows (an older query's, a search pending) cannot be picked, by key or by click.
  const select = (item: T) => {
    if (!waiting) insertMention(item, { value, token, trigger, onSelect, onChange, pending, setCaret: ta.setSelection, dismiss: close })
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    if (!open || e.defaultPrevented) return false
    const action = mentionKeyAction(keyOf(e), flat.length > 0, waiting)
    if (action === null) return false
    e.preventDefault()
    const item = flat[activeIndex]
    if (action === 'wait') return true
    if (action === 'close') close()
    else if (action === 'pick') {
      if (item) select(item)
    } else setActiveIndex(wrapIndex(activeIndex, action === 'next' ? 1 : -1, flat.length))
    return true
  }

  const aria = comboboxAria(open, listId, activeIndex >= 0 ? optionId(activeIndex) : undefined)
  const textareaProps: MentionTextareaProps = { ...ta.handlers, ...aria, ref, value, onKeyDown }
  const state = { open, query, groups, items: flat, activeIndex, setActiveIndex, select, close, listId, optionId, ref }
  const status = { loading: waiting, stale: waiting && flat.length > 0, error: open && !waiting ? found.error : null }
  return { ...state, ...status, textareaProps, onKeyDown }
}
