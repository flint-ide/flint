import { useEffect, useState } from 'react'
import { cancelSignIn, importGitHubCLI, loadAccounts, openSignInPage, signIn, useAccounts, useProvider } from '../../store/accounts'

/**
 * "Sign in with GitHub" where a feature needs an account: the device flow's
 * code and GitHub's page, or the GitHub CLI's token. Used inline (Pull
 * Requests view, Settings ▸ Accounts) and in the sign-in dialog.
 */
export function SignInWithGitHub({ intro }: { intro?: string }) {
  const provider = useProvider()
  const loaded = useAccounts((s) => s.loaded)
  const state = useAccounts((s) => s.signIn)
  const error = useAccounts((s) => s.error)
  const busy = useAccounts((s) => s.busy)
  useEffect(() => {
    if (!loaded) void loadAccounts()
  }, [loaded])

  if (state?.phase === 'code' && state.code) return <EnterCode host={provider?.host ?? 'GitHub'} code={state.code.userCode} expiresIn={state.code.expiresIn} />

  const unavailable = provider?.signIn ?? ''
  return (
    <div className="signin">
      {intro && <span className="muted">{intro}</span>}
      <div className="signin-actions">
        <button className="primary" disabled={!!unavailable || state?.phase === 'starting'} title={unavailable || undefined} onClick={() => void signIn()}>
          {state?.phase === 'starting' ? 'Asking GitHub…' : 'Sign in with GitHub'}
        </button>
        <button disabled={busy} onClick={() => void importGitHubCLI()} title="Runs `gh auth token` once (after you confirm) and keeps the token in the system keyring">
          Use the GitHub CLI's Token
        </button>
      </div>
      {unavailable && <span className="muted signin-note">{unavailable}</span>}
      {error && <span className="error selectable">{error}</span>}
    </div>
  )
}

function EnterCode({ host, code, expiresIn }: { host: string; code: string; expiresIn: number }) {
  const [left, setLeft] = useState(expiresIn)
  useEffect(() => {
    const started = Date.now()
    const timer = setInterval(() => setLeft(Math.max(0, expiresIn - Math.round((Date.now() - started) / 1000))), 1000)
    return () => clearInterval(timer)
  }, [expiresIn])
  return (
    <div className="signin">
      <span className="muted">Enter this code on {host} to let Flint use your account:</span>
      <code className="signin-code selectable">{code}</code>
      <div className="signin-actions">
        <button className="primary" autoFocus onClick={() => void openSignInPage()}>Copy Code &amp; Open GitHub</button>
        <button onClick={cancelSignIn}>Cancel</button>
      </div>
      <span className="muted signin-note">
        Waiting for GitHub… the code expires in {Math.ceil(left / 60)} min. The token goes straight to the system keyring.
      </span>
    </div>
  )
}
