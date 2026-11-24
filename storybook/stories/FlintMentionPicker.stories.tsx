import type { Meta, StoryObj } from '@storybook/react-vite'
import { type KeyboardEvent, useState } from 'react'
import {
  Badge,
  Button,
  ChatComposer,
  isImeKey,
  MentionPicker,
  type MentionItem,
  type MentionSearch,
  type MentionSelectResult,
  Textarea,
  useMentionPicker,
} from '@flint-ide/ui'
import { AppWindow, Braces, FileCode, FileText, Plus } from '@flint-ide/ui/icons'

/** The @-mention picker (ui/src/chat): type "@" in the field. ↑ ↓ move, ↵ or Tab insert, Esc closes. */
const meta = { title: 'Flint/Mention Picker' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const file = (path: string): MentionItem => {
  const slash = path.lastIndexOf('/')
  return {
    id: `file:${path}`,
    label: path.slice(slash + 1),
    detail: slash > 0 ? path.slice(0, slash) : undefined,
    group: 'Files',
    icon: path.endsWith('.md') ? <FileText /> : <FileCode />,
    insert: `@${path}`,
  }
}

const ITEMS: readonly MentionItem[] = [
  { id: 'tab:App.tsx', label: 'App.tsx', detail: 'frontend/src', group: 'Open tabs', icon: <AppWindow />, insert: '@frontend/src/App.tsx' },
  { id: 'tab:load.go', label: 'load.go', detail: 'internal/config', group: 'Open tabs', icon: <AppWindow />, insert: '@internal/config/load.go' },
  ...[
    'frontend/src/App.tsx',
    'frontend/src/boot.ts',
    'frontend/src/components/agent/AgentChat.tsx',
    'frontend/src/components/agent/PromptPicker.tsx',
    'internal/config/load.go',
    'internal/config/load_test.go',
    'docs/DEVELOPMENT.md',
    'docs/notatki/zażółć gęślą jaźń.md',
    'ui/src/chat/ChatComposer.tsx',
    'ui/src/chat/mention.ts',
  ].map(file),
  { id: 'sym:useMentionPicker', label: 'useMentionPicker', detail: 'ui/src/chat/useMentionPicker.ts', group: 'Symbols', icon: <Braces /> },
  { id: 'sym:applyMention', label: 'applyMention', detail: 'ui/src/chat/mention.ts', group: 'Symbols', icon: <Braces />, keywords: ['replace', 'insert'] },
  { id: 'sym:Load', label: 'Load', detail: 'internal/config/load.go', group: 'Symbols', icon: <Braces /> },
]

const FRAME = { maxWidth: 560, paddingTop: 340 }

type FieldProps = {
  readonly items?: readonly MentionItem[]
  readonly search?: MentionSearch<MentionItem>
  readonly initial?: string
}

/** A bare textarea: the popup spans the textarea itself (anchorClosest=""). */
function Field({ items, search, initial = 'Look at @' }: FieldProps) {
  const [value, setValue] = useState(initial)
  const mention = useMentionPicker({ value, onChange: setValue, items, search })
  return (
    <div style={FRAME}>
      <Textarea {...mention.textareaProps} aria-label="Message" placeholder="Type @ to mention a file, a symbol or an open tab" rows={4} spellCheck={false} />
      <MentionPicker picker={mention} anchor={mention.ref} anchorClosest="" />
    </div>
  )
}

export const StaticList: Story = {
  name: 'Static grouped list',
  render: () => <Field items={ITEMS} />,
}

const wait = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    const onAbort = () => {
      clearTimeout(timer)
      reject(new DOMException('Aborted', 'AbortError'))
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    signal.addEventListener('abort', onAbort, { once: true })
  })

/** Like a ripgrep file search: slow, filtered on the "server"; "fail" in the query makes it reject. */
const slowSearch: MentionSearch<MentionItem> = async (query, signal) => {
  await wait(600, signal)
  if (query.includes('fail')) throw new Error('rg exited with status 2')
  const q = query.toLowerCase()
  return ITEMS.filter((i) => i.group === 'Files' && `${i.detail ?? ''}/${i.label}`.toLowerCase().includes(q))
}

export const AsyncSearch: Story = {
  name: 'Async search (600 ms, type "fail" for an error)',
  render: () => <Field search={slowSearch} initial="Fix @" />,
}

export const NoResults: Story = {
  name: 'No results',
  render: () => <Field items={ITEMS} initial="Where is @nothing-matches-this" />,
}

/** onSelect attaches the item as a chip and returns '' so the typed "@que" disappears from the text. */
function ChipsField() {
  const [value, setValue] = useState('Compare @')
  const [chips, setChips] = useState<readonly MentionItem[]>([])
  const onSelect = (item: MentionItem): MentionSelectResult => {
    setChips((c) => (c.some((x) => x.id === item.id) ? c : [...c, item]))
    return ''
  }
  const mention = useMentionPicker({ value, onChange: setValue, items: ITEMS, onSelect })
  return (
    <div style={FRAME}>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, minHeight: 22, marginBottom: 6 }}>
        {chips.map((c) => (
          <Badge key={c.id} color="grey" badgeStyle="light_border" size="sm" onRemove={() => setChips((all) => all.filter((x) => x.id !== c.id))} removeLabel="Remove">
            {c.label}
          </Badge>
        ))}
      </div>
      <Textarea {...mention.textareaProps} aria-label="Message" rows={4} spellCheck={false} />
      <MentionPicker picker={mention} anchor={mention.ref} anchorClosest="" />
    </div>
  )
}

export const ChipsInsteadOfText: Story = {
  name: 'onSelect adds chips',
  render: () => <ChipsField />,
}

/** The real ChatComposer: the hook's textarea props pass straight through; Enter sends only while the popup is closed. */
function Composer() {
  const [value, setValue] = useState('')
  const [sent, setSent] = useState<readonly string[]>([])
  const mention = useMentionPicker({ value, onChange: setValue, items: ITEMS })
  const send = () => {
    if (!value.trim()) return
    setSent((s) => [...s, value])
    setValue('')
  }
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (mention.onKeyDown(e)) return
    // isImeKey, not only isComposing: WebKit sends the IME's commit Enter after compositionend with keyCode 229.
    const ime = isImeKey({ key: e.key, keyCode: e.keyCode, isComposing: e.nativeEvent.isComposing })
    if (e.key === 'Enter' && !e.shiftKey && !ime) {
      e.preventDefault()
      send()
    }
  }
  return (
    <div style={{ ...FRAME, border: '1px solid var(--border)', borderRadius: 8, paddingTop: 0, marginTop: 340 }}>
      <div style={{ padding: 8, fontSize: 12, color: 'var(--fg-muted)' }}>{sent.length === 0 ? 'Nothing sent yet.' : sent.map((s, i) => <div key={i}>Sent: {s}</div>)}</div>
      <ChatComposer
        {...mention.textareaProps}
        onKeyDown={onKeyDown}
        placeholder="Message Claude (Enter to send, @ to mention)"
        spellCheck={false}
        above={<MentionPicker picker={mention} anchor={mention.ref} />}
        toolbar={
          <Button variant="ghost" size="sm">
            <Plus data-icon="inline-start" />
            current file
          </Button>
        }
        actions={
          <Button variant="primary" size="sm" disabled={!value.trim()} onClick={send}>
            Send
          </Button>
        }
      />
    </div>
  )
}

export const WithChatComposer: Story = {
  name: 'In the ChatComposer',
  render: () => <Composer />,
}
