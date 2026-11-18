import { useDialog } from '../../store/dialog'
import { useGit } from '../../store/git'
import { PullProjectPopup } from '../git/PullProjectPopup'
import { Modal } from './Modal'
import { AgentPeek } from '../agents/AgentPeek'
import { ContextMenu } from '../ui/ContextMenu'
import { closeSignInDialog, useAccounts } from '../../store/accounts'
import { SignInWithGitHub } from '../accounts/SignInWithGitHub'

/** Global confirm dialog, Sign in with GitHub, the Update Project picker, context menu and toast notification. */
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
      {toast && <div className={`toast ${toast.kind}`} role="status">{toast.message}</div>}
    </>
  )
}
