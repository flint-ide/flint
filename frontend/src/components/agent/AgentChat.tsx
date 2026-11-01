import { memo, useEffect, useMemo, useRef, useState } from 'react'
import { useAgentChat, type ChatSession } from '../../store/agentChat'
import { editedFiles } from '../../lib/agentReducer'
import { totalsOf } from '../../lib/usage'
import { focusSoon } from '../../lib/focusSoon'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { rootOf } from '../../lib/workspaceRoots'
import { FileLink, MessageText, PermissionCard, PlanView, ToolCallRow, UsageLine, ChatUsage } from './ChatItems'
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
  const scroller = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLTextAreaElement>(null)
  const stick = useRef(true)
  const frame = useRef(0)
  const picker = usePromptPicker(session, input)
  const { chat } = session

  // Follow the stream unless the user scrolled up to read. Scrolling (which reads the
  // layout) happens at most once per frame however often the transcript changes.
  useEffect(() => {
    if (!stick.current || frame.current) return
    frame.current = requestAnimationFrame(() => {
      frame.current = 0
      const el = scroller.current
      if (el && stick.current) el.scrollTop = el.scrollHeight
    })
  }, [chat.items])
  useEffect(() => () => cancelAnimationFrame(frame.current), [])
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
    <div className={`agent-chat${chat.busy ? ' busy' : ''}`}>
      <div className="chat-header">
        <span className="agent-name">{session.agent}</span>
        {session.model && <span className="muted">{session.model}</span>}
        <ChatUsage totals={totalsOf(chat)} />
        <span className="spacer" />
        <label className="check muted" title="Show the agent's reasoning">
          <input type="checkbox" checked={showThoughts} onChange={(e) => setShowThoughts(e.target.checked)} /> thinking
        </label>
        <PolicyToggle />
      </div>
      <BudgetWarning chatId={session.id} />
      <PlanView plan={chat.plan} />
      <div
        className="chat-log"
        ref={scroller}
        onScroll={(e) => {
          const el = e.currentTarget
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
        }}
      >
        {chat.items.length === 0 && (
          <div className="placeholder muted">
            Ask {session.agent} anything about {root.split('/').pop()}. Edits are applied directly; review them in the Commit panel ({formatKeys('Mod+K')}).
          </div>
        )}
        {chat.items.map((it, i) => {
          switch (it.type) {
            case 'user':
              return (
                <div key={i} className="msg user">
                  <MessageText text={it.text} />
                  {it.attachments.length > 0 && (
                    <div className="msg-attachments">{it.attachments.map((a, j) => <span key={j} className="chip">{label(a, root)}</span>)}</div>
                  )}
                </div>
              )
            case 'assistant':
              return <div key={i} className="msg assistant"><MessageText text={it.text} /></div>
            case 'thought':
              return showThoughts ? <div key={i} className="msg thought"><MessageText text={it.text} /></div> : null
            case 'tool':
              return <ToolCallRow key={it.call.id} call={it.call} root={root} />
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
              return <div key={i} className="msg error selectable">{it.text}</div>
            case 'end':
              return <UsageLine key={i} usage={it.usage} stopReason={it.stopReason} />
          }
          return null
        })}
        {session.dormant && chat.items.length > 0 && (
          <div className="restored-note muted">Restored after restart — {session.agent} picks the conversation up with your next message.</div>
        )}
        {chat.busy && <div className="working muted">● working…</div>}
      </div>
      {edited.length > 0 && !chat.busy && (
        <div className="chat-edited">
          <span className="muted">Changed:</span>
          {edited.slice(0, 6).map((p) => <FileLink key={p} path={p} root={root} />)}
          {edited.length > 6 && <span className="muted">+{edited.length - 6}</span>}
        </div>
      )}
      <div className="composer">
        {session.attachments.length > 0 && (
          <div className="composer-attachments">
            {session.attachments.map((a, i) => (
              <span key={i} className="chip">
                {label(a, root)}
                <button className="icon" onClick={() => detach(session.id, i)} aria-label="Remove attachment">✕</button>
              </span>
            ))}
          </div>
        )}
        {picker.element}
        <textarea
          ref={input}
          value={session.draft}
          placeholder={chat.busy ? `${session.agent} is working… (Esc to stop)` : `Message ${session.agent} (Enter to send, ${isMac() ? '⇧Enter' : 'Shift+Enter'} newline, / for prompts)`}
          spellCheck={false}
          onChange={(e) => setDraft(session.id, e.target.value)}
          onKeyDown={(e) => {
            if (picker.onKeyDown(e)) return
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void send(session.id)
            } else if (e.key === 'Escape' && chat.busy) {
              e.preventDefault()
              cancel(session.id)
            }
          }}
        />
        <div className="composer-actions">
          <button className="link" onClick={attachActiveFile} title={`Attach the file open in the editor (${formatKeys('Mod+L')} attaches the selection)`}>
            ＋ current file
          </button>
          <span className="spacer" />
          {chat.busy ? (
            <button className="danger" onClick={() => cancel(session.id)}>■ Stop</button>
          ) : (
            <button className="primary" disabled={!session.draft.trim()} onClick={() => void send(session.id)}>Send</button>
          )}
        </div>
      </div>
    </div>
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
      label: 'Edit files in the project',
      checked: policy.autoApproveEdits,
      onSelect: () => void setPolicy({ ...policy, autoApproveEdits: !policy.autoApproveEdits }),
    },
    {
      label: 'Read and search the project',
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
        <button {...t} className="link" title="What the agent may do without asking">{summary}</button>
      )}
    />
  )
}
