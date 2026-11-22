import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Badge,
  Button,
  ChatBanner,
  ChatChangedFiles,
  ChatComposer,
  ChatContextUsage,
  ChatFileLink,
  ChatHeader,
  ChatMarkdown,
  ChatMessage,
  ChatNotice,
  ChatPermission,
  ChatPlan,
  ChatTranscript,
  ChatView,
  ChatWorking,
  Checkbox,
  DiffStat,
  ToolCall,
  ToolCallOutput,
  ToolDiff,
} from '@flint-ide/ui'
import { Plus, Square } from '@flint-ide/ui/icons'

/** The agent chat from @flint-ide/ui (ui/src/chat): every transcript item kind, the plan, a budget banner and the composer. */
const meta = { title: 'Flint/Chat' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const REPLY = `I found the problem in **parseConfig**: the \`timeout\` field is read as seconds.

## Fix
- convert it with \`time.Duration\`
- add a test for the zero value

\`\`\`go
timeout := time.Duration(cfg.Timeout) * time.Millisecond
\`\`\`

1. Run the tests
2. Review the diff in the Commit panel`

const PLAN = [
  { content: 'Read the config loader', status: 'completed' },
  { content: 'Fix the timeout unit', status: 'in_progress' },
  { content: 'Add a regression test', status: 'pending' },
]

const OPTIONS = [
  { id: 'once', name: 'Allow once', kind: 'allow_once' },
  { id: 'always', name: 'Always allow', kind: 'allow_always' },
  { id: 'no', name: 'Reject', kind: 'reject_once' },
]

const file = (p: string) => <ChatFileLink key={p} label={p} path={`/work/flint/${p}`} onOpen={() => undefined} />

function Transcript({ busy }: { busy: boolean }) {
  const [answer, setAnswer] = useState<string | null>(null)
  return (
    <ChatTranscript follow={answer} busy={busy} aria-label="Conversation with Claude">
      <ChatNotice variant="placeholder">Ask Claude anything about flint. Edits are applied directly; review them in the Commit panel (⌘K).</ChatNotice>
      <ChatMessage
        role="user"
        from="Review"
        attachments={
          <Badge color="grey" badgeStyle="light_border" size="sm">
            internal/config/load.go:40-52
          </Badge>
        }
      >
        <ChatMarkdown text="Why does the request time out after **3 ms**?" />
      </ChatMessage>
      <ChatMessage role="thought">
        <ChatMarkdown text="The loader multiplies by `time.Millisecond`…" />
      </ChatMessage>
      <ToolCall kind="read" title="internal/config/load.go" status="completed" paths={file('internal/config/load.go')} />
      <ToolCall kind="search" title='rg "Timeout" internal/' status="completed">
        <ToolCallOutput>rg "Timeout" internal/</ToolCallOutput>
        <ToolCallOutput muted>{'internal/config/load.go:44: Timeout int\ninternal/http/client.go:12: c.Timeout = cfg.Timeout'}</ToolCallOutput>
      </ToolCall>
      <ToolCall kind="edit" title="Edit internal/config/load.go" status="completed" meta={<DiffStat added={2} removed={1} />} paths={file('internal/config/load.go')} defaultOpen>
        <ToolDiff
          header={file('internal/config/load.go')}
          lines={[
            { kind: 'del', text: 'timeout := cfg.Timeout * time.Millisecond' },
            { kind: 'add', text: 'timeout := time.Duration(cfg.Timeout) * time.Second' },
            { kind: 'add', text: '// seconds, as documented' },
            { kind: 'sep', text: '⋯' },
          ]}
        />
      </ToolCall>
      <ToolCall kind="execute" title="go test ./internal/config/..." status="in_progress" live={busy}>
        <ToolCallOutput>go test ./internal/config/...</ToolCallOutput>
      </ToolCall>
      <ToolCall kind="fetch" title="https://pkg.go.dev/time" status="failed" />
      <ToolCall kind="delete" title="Delete tmp/cache" status="cancelled" />
      <ChatPermission
        kind="execute"
        title="Run go mod tidy"
        input="go mod tidy"
        options={OPTIONS}
        settled={answer !== null}
        answer={answer}
        onAnswer={(_, name) => setAnswer(name)}
      />
      <ChatPermission kind="edit" title="Edit go.sum" options={OPTIONS} settled answer={null} onAnswer={() => undefined} />
      <ChatMessage role="assistant">
        <ChatMarkdown text={REPLY} />
      </ChatMessage>
      <ChatMessage role="error">agent exited: signal: killed (exit status 137)</ChatMessage>
      <ChatNotice>max tokens · 18240 tokens · $0.041</ChatNotice>
      <ChatNotice variant="divider">Restored after restart — Claude picks the conversation up with your next message.</ChatNotice>
      {busy && <ChatWorking />}
    </ChatTranscript>
  )
}

function FullChat() {
  const [busy, setBusy] = useState(true)
  const [draft, setDraft] = useState('')
  const [thoughts, setThoughts] = useState(true)
  return (
    <div style={{ height: 900, maxWidth: 560, border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
      <ChatView>
        <ChatHeader
          title="Claude"
          subtitle="opus"
          meta={
            <ChatContextUsage percent={74} title="Context: 148k of 200k tokens">
              <span>412k in / 38k out</span>
              <span>$1.92</span>
            </ChatContextUsage>
          }
          actions={
            <>
              <Checkbox wrapperClassName="fui-muted" label="thinking" checked={thoughts} onCheckedChange={setThoughts} />
              <Button variant="ghost" size="sm">
                auto: edits
              </Button>
            </>
          }
        />
        <ChatBanner
          onDismiss={() => undefined}
          actions={
            <Button variant="ghost" size="sm">
              Usage
            </Button>
          }
        >
          Today's budget of $5.00 is 80 % spent
        </ChatBanner>
        <ChatPlan entries={PLAN} />
        <Transcript busy={busy} />
        <ChatChangedFiles more={3}>{['internal/config/load.go', 'internal/config/load_test.go'].map(file)}</ChatChangedFiles>
        <ChatComposer
          attachments={
            <Badge color="grey" badgeStyle="light_border" size="sm" onRemove={() => undefined} removeLabel="Remove attachment">
              frontend/src/boot.ts
            </Badge>
          }
          value={draft}
          placeholder={busy ? 'Claude is working… (Esc to stop)' : 'Message Claude (Enter to send, ⇧Enter newline, / for prompts)'}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setBusy(false)
          }}
          toolbar={
            <Button variant="ghost" size="sm">
              <Plus data-icon="inline-start" />
              current file
            </Button>
          }
          actions={
            busy ? (
              <Button variant="destructive" size="sm" onClick={() => setBusy(false)}>
                <Square data-icon="inline-start" />
                Stop
              </Button>
            ) : (
              <Button variant="primary" size="sm" disabled={!draft.trim()} onClick={() => setBusy(true)}>
                Send
              </Button>
            )
          }
        />
      </ChatView>
    </div>
  )
}

export const Dark: Story = {
  render: () => <FullChat />,
  globals: { theme: 'dark' },
}

export const Light: Story = {
  render: () => <FullChat />,
  globals: { theme: 'light' },
}

export const PausedBudget: Story = {
  name: 'Paused budget banner',
  render: () => (
    <div style={{ maxWidth: 560, border: '1px solid var(--border)' }}>
      <ChatBanner
        tone="negative"
        actions={
          <>
            <Button variant="plain" size="sm">
              Continue anyway
            </Button>
            <Button variant="ghost" size="sm">
              Usage
            </Button>
          </>
        }
      >
        This task's budget of $2.00 is spent — new turns are paused.
      </ChatBanner>
    </div>
  ),
}
