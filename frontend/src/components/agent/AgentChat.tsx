import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Badge,
  Button,
  ChatChangedFiles,
  ChatComposer,
  ChatHeader,
  ChatMarkdown,
  ChatMessage,
  ChatNotice,
  ChatPlan,
  ChatTranscript,
  ChatView,
  ChatWorking,
  Checkbox,
  ContextMeter,
  isImeKey,
  MentionPicker,
  type MentionItem,
  useMentionPicker,
} from '@flint-ide/ui'
import { Plus, Square } from '@flint-ide/ui/icons'
import { Workspace } from '../../api/backend'
import { useAgentChat, type ChatSession } from '../../store/agentChat'
import { editedFiles } from '../../lib/agentReducer'
import { totalsOf } from '../../lib/usage'
import { FILE_MENTION_LIMIT, fileMentionItems } from '../../lib/fileMentions'
import { FileIcon } from '../icons/FileIcon'
import { focusSoon } from '../../lib/focusSoon'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { rootOf } from '../../lib/workspaceRoots'
import { FileLink, PermissionCard, ToolCallRow, UsageLine, ChatUsage } from './ChatItems'
import { Dropdown, type DropdownItem } from '../ui/Dropdown'
import { BudgetWarning } from './BudgetWarning'
import { usePromptPicker } from './PromptPicker'
import { formatKeys } from '../../lib/commands'
import { isMac } from '../../lib/platform'

/** Conversation with a coding agent: streamed replies, tool calls, plan and questions. */
// memo: chunks for one session must not re-render the others (actions are stable).
export const AgentChat = memo(function AgentChat({ session, active }: { session: ChatSession; active: boolean }) {
  const send = useAgentChat((s) => s.send)
  const cancel = useAgentChat((s) => s.cancel)
  const respond = useAgentChat((s) => s.respond)
  const setDraft = useAgentChat((s) => s.setDraft)
  const detach = useAgentChat((s) => s.detach)
  const attach = useAgentChat((s) => s.attach)
  // Paths are shown relative to the chat's own workspace folder.
  const root = useWorkspace((s) => rootOf(s.roots, session.cwd)) ?? session.cwd
  const [showThoughts, setShowThoughts] = useState(false)
  const input = useRef<HTMLTextAreaElement>(null)
  const picker = usePromptPicker(session, input)
  // "@" lists the folder's files; picking one attaches it and takes the typed "@query" out of the message.
  const findFiles = useCallback(
    async (query: string, signal: AbortSignal): Promise<readonly MentionItem[]> => {
      const found = await Workspace.FindFiles([root], query, FILE_MENTION_LIMIT)
      if (signal.aborted) return []
      return fileMentionItems(found.matches ?? [], root).map((item) => ({ ...item, icon: <FileIcon name={item.label} /> }))
    },
    [root],
  )
  const mention = useMentionPicker({
    value: session.draft,
    onChange: (text) => setDraft(session.id, text),
    search: findFiles,
    onSelect: (item) => {
      attach(session.id, { path: item.id })
      return ''
    },
    ref: input,
  })
  const { chat } = session
  const totals = totalsOf(chat)
  const folder = root.split('/').pop()

  useEffect(() => {
    if (active) return focusSoon(() => input.current?.focus())
  }, [active])

  const attachActiveFile = () => {
    const { tabs, activeId } = useEditor.getState()
    const tab = tabs.find((t) => t.id === activeId)
    if (tab?.kind === 'file') attach(session.id, { path: tab.path })
  }

  // Shown only between turns: no need to rescan the transcript for every streamed token.
  const idleChat = chat.busy ? null : chat
  const edited = useMemo(() => (idleChat ? editedFiles(idleChat) : NONE), [idleChat])

  return (
    <ChatView>
      <ChatHeader
        title={session.agent}
        subtitle={session.model}
        meta={<ChatUsage totals={totals} />}
        actions={
          <>
            <Checkbox
              wrapperClassName="fui-muted"
              label="thinking"
              title="Show the agent's reasoning"
              checked={showThoughts}
              onCheckedChange={setShowThoughts}
            />
            <PolicyToggle />
          </>
        }
      />
      <BudgetWarning chatId={session.id} />
      <ChatPlan entries={chat.plan} />
      <ChatTranscript follow={chat.items} busy={chat.busy} aria-label={`Conversation with ${session.agent}`}>
        {chat.items.length === 0 && session.dormant && (
          <ChatNotice variant="placeholder">
            An earlier conversation with {session.agent} in {folder}: its messages are not shown here, but {session.agent} remembers them and picks the conversation up with your next message.
          </ChatNotice>
        )}
        {chat.items.length === 0 && !session.dormant && (
          <ChatNotice variant="placeholder">
            Ask {session.agent} anything about {folder}. Edits are applied directly; review them in the Commit panel ({formatKeys('Mod+K')}).
          </ChatNotice>
        )}
        {chat.items.map((it, i) => {
          switch (it.type) {
            case 'user':
              return (
                <ChatMessage
                  key={i}
                  role="user"
                  from={it.from}
                  attachments={
                    it.attachments.length > 0 &&
                    it.attachments.map((a, j) => (
                      <Badge key={j} color="grey" badgeStyle="light_border" size="sm">
                        {label(a, root)}
                      </Badge>
                    ))
                  }
                >
                  <ChatMarkdown text={it.text} />
                </ChatMessage>
              )
            case 'assistant':
              return (
                <ChatMessage key={i} role="assistant">
                  <ChatMarkdown text={it.text} />
                </ChatMessage>
              )
            case 'thought':
              return showThoughts ? (
                <ChatMessage key={i} role="thought">
                  <ChatMarkdown text={it.text} />
                </ChatMessage>
              ) : null
            case 'tool':
              return <ToolCallRow key={it.call.id} call={it.call} root={root} live={chat.busy} />
            case 'permission':
              return (
                <PermissionCard
                  key={`p${it.request.id}`}
                  request={it.request}
                  answer={it.answer}
                  settled={it.settled}
                  onAnswer={(optionId, name) => void respond(session.id, it.request.id, optionId, name)}
                />
              )
            case 'error':
              return (
                <ChatMessage key={i} role="error">
                  {it.text}
                </ChatMessage>
              )
            case 'end':
              return <UsageLine key={i} usage={it.usage} stopReason={it.stopReason} />
          }
          return null
        })}
        {session.dormant && chat.items.length > 0 && (
          <ChatNotice variant="divider">Restored after restart — {session.agent} picks the conversation up with your next message.</ChatNotice>
        )}
        {chat.busy && <ChatWorking />}
      </ChatTranscript>
      {edited.length > 0 && !chat.busy && (
        <ChatChangedFiles more={edited.length - 6}>
          {edited.slice(0, 6).map((p) => (
            <FileLink key={p} path={p} root={root} />
          ))}
        </ChatChangedFiles>
      )}
      <ChatComposer
        attachments={
          session.attachments.length > 0 &&
          session.attachments.map((a, i) => (
            <Badge key={i} color="grey" badgeStyle="light_border" size="sm" onRemove={() => detach(session.id, i)} removeLabel="Remove attachment">
              {label(a, root)}
            </Badge>
          ))
        }
        {...mention.textareaProps}
        above={
          <>
            {picker.element}
            <MentionPicker picker={mention} anchor={input} emptyText="No matching files" />
          </>
        }
        placeholder={chat.busy ? `${session.agent} is working… (Esc to stop)` : `Message ${session.agent} (Enter to send, ${isMac() ? '⇧Enter' : 'Shift+Enter'} newline, / for prompts, @ for files)`}
        spellCheck={false}
        onKeyDown={(e) => {
          if (mention.onKeyDown(e)) return
          if (picker.onKeyDown(e)) return
          const ime = isImeKey({ key: e.key, keyCode: e.keyCode, isComposing: e.nativeEvent.isComposing })
          if (e.key === 'Enter' && !e.shiftKey && !ime) {
            e.preventDefault()
            void send(session.id)
          } else if (e.key === 'Escape' && chat.busy) {
            e.preventDefault()
            cancel(session.id)
          }
        }}
        toolbar={
          <>
            <Button variant="ghost" size="sm" onClick={attachActiveFile} title={`Attach the file open in the editor (${formatKeys('Mod+L')} attaches the selection)`}>
              <Plus data-icon="inline-start" />
              current file
            </Button>
            <ContextMeter used={totals.contextTokens} limit={totals.contextWindow} />
          </>
        }
        actions={
          chat.busy ? (
            <Button variant="destructive" size="sm" onClick={() => cancel(session.id)}>
              <Square data-icon="inline-start" />
              Stop
            </Button>
          ) : (
            <Button variant="primary" size="sm" disabled={!session.draft.trim()} onClick={() => void send(session.id)}>
              Send
            </Button>
          )
        }
      />
    </ChatView>
  )
})

const NONE: string[] = []

function label(a: { path: string; startLine?: number; endLine?: number }, root: string): string {
  const rel = a.path.startsWith(root + '/') ? a.path.slice(root.length + 1) : a.path
  return a.startLine ? `${rel}:${a.startLine}${a.endLine && a.endLine !== a.startLine ? `-${a.endLine}` : ''}` : rel
}

/** What the agent may do without asking (edits by default — review them via git). */
function PolicyToggle() {
  const policy = useAgentChat((s) => s.policy)
  const setPolicy = useAgentChat((s) => s.setPolicy)
  if (!policy) return null
  const summary = policy.autoApproveAll ? 'auto: all' : policy.autoApproveEdits ? 'auto: edits' : 'ask: all'
  const items: DropdownItem[] = [
    { type: 'section', label: 'Without asking, the agent may…' },
    {
      label: 'Edit files in the folder',
      checked: policy.autoApproveEdits,
      onSelect: () => void setPolicy({ ...policy, autoApproveEdits: !policy.autoApproveEdits }),
    },
    {
      label: 'Read and search the folder',
      checked: policy.autoApproveReads,
      onSelect: () => void setPolicy({ ...policy, autoApproveReads: !policy.autoApproveReads }),
    },
    { type: 'separator' },
    {
      label: 'Do everything (commands too)',
      danger: true,
      checked: policy.autoApproveAll,
      onSelect: () => void setPolicy({ ...policy, autoApproveAll: !policy.autoApproveAll }),
    },
  ]
  return (
    <Dropdown
      align="end"
      minWidth={260}
      items={items}
      trigger={(t) => (
        <Button {...t} variant="ghost" size="sm" title="What the agent may do without asking">{summary}</Button>
      )}
    />
  )
}
