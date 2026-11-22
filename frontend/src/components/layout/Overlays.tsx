import { Button, FormActions, IconButton } from '@flint-ide/ui'
import { X } from '@flint-ide/ui/icons'
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
import { ConfirmDialog } from './ConfirmDialog'
import { Toasts } from './Toasts'

/** Global confirm dialog, Sign in with GitHub, the Update Repository picker, context menu, toasts and the agent attention notice. */
export function Overlays() {
  const signIn = useAccounts((s) => s.dialog)
  const pullPicker = useGit((s) => s.pullPicker)

  return (
    <>
      <ConfirmDialog />
      {signIn && (
        <Modal title="Sign in to GitHub" onClose={closeSignInDialog} className="signin-dialog">
          <SignInWithGitHub intro="This needs a GitHub account. Flint keeps its token in the system keyring and sends it only to GitHub's API." />
          <FormActions>
            <Button variant="plain" onClick={closeSignInDialog}>Close</Button>
          </FormActions>
        </Modal>
      )}
      {pullPicker && <PullProjectPopup />}
      <AgentPeek />
      <ContextMenu />
      <Toasts />
      <div className="toast-stack">
        <AttentionNotice />
      </div>
    </>
  )
}

/**
 * An agent waits for an answer in a tab that is not the active one: a notice that stays until the
 * request is answered, shown or closed (passing toasts stack above it and never replace it). From the
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
      <Button variant="primary" size="sm" aria-label={`Show: ${need.inApp}`} onClick={() => showNeed(need)}>Show</Button>
      <IconButton
        variant="ghost"
        size="sm"
        aria-label="Close this notice (the request stays open)"
        title="Close this notice (the request stays open)"
        onClick={() => dismissNotice(need.key)}
      >
        <X />
      </IconButton>
    </div>
  )
}
