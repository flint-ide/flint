import { Suspense, useEffect } from 'react'
import { lazyPreload } from '../../lib/lazyPreload'
import { chatTitle } from '../../lib/agentList'
import { useAgentChat, type ChatSession } from '../../store/agentChat'
import { closePeek, keepPeekAsTab, useAgents } from '../../store/agents'
import { useEditor } from '../../store/editor'
import { ErrorBoundary } from '../layout/ErrorBoundary'
import { Modal } from '../layout/Modal'

const AgentChat = lazyPreload(() => import('../agent/AgentChat').then((m) => ({ default: m.AgentChat })))

/**
 * One agent's chat over the current tab, which stays the active one: read it, answer
 * a request or type a prompt, then go back. The chat view is the same component as in
 * the tab, and it holds no subscription of its own (agent events land in the chat
 * store), so a chat shown both in its tab and here receives each event once; the
 * draft is in the store too, so the two inputs always agree.
 */
export function AgentPeek() {
  const id = useAgents((s) => s.peek)
  const session = useAgentChat((s) => (id ? s.sessions[id] : undefined))
  if (!id || !session) return null
  return <PeekDialog key={id} session={session} />
}

function PeekDialog({ session }: { session: ChatSession }) {
  // The page was in front of the overlay: give focus back there on close, unless
  // "Keep as Tab" moved it to the chat's own tab.
  useEffect(() => {
    const before = document.activeElement
    const tab = useEditor.getState().activeId
    return () => {
      if (useEditor.getState().activeId === tab && before instanceof HTMLElement && before.isConnected) before.focus()
    }
  }, [])

  return (
    <Modal title={`${session.agent} · ${chatTitle(session.chat)}`} onClose={closePeek} className="peek">
      <div className="peek-actions">
        <button className="link" onClick={keepPeekAsTab} title="Close the peek and switch to the chat's tab">Keep as Tab</button>
        <span className="spacer" />
        <button className="icon" onClick={closePeek} aria-label="Close peek" title="Close (Esc)">✕</button>
      </div>
      <div className="peek-chat">
        <ErrorBoundary label="Peek">
          <Suspense fallback={null}>
            <AgentChat session={session} active />
          </Suspense>
        </ErrorBoundary>
      </div>
    </Modal>
  )
}
