import { useDialog } from '../../store/dialog'
import { useGit } from '../../store/git'
import { PullProjectPopup } from '../git/PullProjectPopup'
import { Modal } from './Modal'
import { AgentPeek } from '../agents/AgentPeek'
import { ContextMenu } from '../ui/ContextMenu'
import { closeSignInDialog, useAccounts } from '../../store/accounts'
import { SignInWithGitHub } from '../accounts/SignInWithGitHub'
import { dismissNotice, showNeed, useAlerts } from '../../store/alerts'
import { useEditor } from '../../store/editor'
import { inAppNotice } from '../../lib/attention'

/** Global confirm dialog, Sign in with GitHub, the Update Repository picker, context menu and toast notification. */
export function Overlays() {
  const confirm = useDialog((s) => s.confirm)
  const toast = useDialog((s) => s.toast)
  const signIn = useAccounts((s) => s.dialog)
  const pullPicker = useGit((s) => s.pullPicker)

  return (
    <>
      {confirm && (
        <Modal onClose={() => confirm.resolve(false)} className="confirm">
          <p>{confirm.message}</p>
          <div className="form-actions">
            <span className="spacer" />
            <button onClick={() => confirm.resolve(false)}>Cancel</button>
            {confirm.alternateLabel && <button onClick={() => confirm.resolve(false, true)}>{confirm.alternateLabel}</button>}
            <button className="danger" autoFocus onClick={() => confirm.resolve(true)}>
              {confirm.confirmLabel}
            </button>
          </div>
        </Modal>
      )}
      {signIn && (
        <Modal title="Sign in to GitHub" onClose={closeSignInDialog} className="signin-dialog">
          <SignInWithGitHub intro="This needs a GitHub account. Flint keeps its token in the system keyring and sends it only to GitHub's API." />
          <div className="form-actions">
            <span className="spacer" />
            <button onClick={closeSignInDialog}>Close</button>
          </div>
        </Modal>
      )}
      {pullPicker && <PullProjectPopup />}
      <AgentPeek />
      <ContextMenu />
      <div className="toast-stack">
        {toast && <div className={`toast ${toast.kind}`} role="status">{toast.message}</div>}
        <AttentionNotice />
      </div>
    </>
  )
}

/**
 * An agent waits for an answer in a tab that is not the active one: a notice that stays until the
 * request is answered, shown or closed (the passing toast above it never replaces it). From the
 * keyboard: the command "Show Agent Waiting for You" does what Show does.
 */
function AttentionNotice() {
  const attention = useAlerts((s) => s.attention)
  const dismissed = useAlerts((s) => s.dismissed)
  const active = useEditor((s) => s.activeId)
  const need = inAppNotice(attention, active, dismissed)
  if (!need) return null
  return (
    <div className="toast attention" role="alert" title="From the keyboard: Show Agent Waiting for You in the command palette">
      <span>{need.inApp}</span>
      <span className="spacer" />
      <button className="link" aria-label={`Show: ${need.inApp}`} onClick={() => showNeed(need)}>Show</button>
      <button className="icon" aria-label="Close this notice (the request stays open)" title="Close this notice (the request stays open)" onClick={() => dismissNotice(need.key)}>✕</button>
    </div>
  )
}
