import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { loadAccounts, onAccountsChanged, openAuthorizedApps, signOut, useProvider } from '../../store/accounts'
import { DEFAULT_GITHUB, GITHUB_RETRY_WAIT, parseScopes, validClientId, validGitHubHost, type GitHubSettings } from '../../lib/githubSettings'
import { keyringName } from '../../lib/platform'
import { openInBrowser } from '../../store/pullRequests'
import { SignInWithGitHub } from '../accounts/SignInWithGitHub'
import { NumberField, Row, TextField } from './fields'

const OAUTH_APPS_DOCS = 'https://docs.github.com/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app'

/** Settings ▸ Accounts: the GitHub account Flint uses, and how it signs in. */
export function AccountsSection() {
  const github = useSettings((s) => s.settings.github)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<GitHubSettings>) => update((s) => ({ github: { ...s.github, ...change } }))
  useEffect(() => {
    void loadAccounts()
    return onAccountsChanged(() => void loadAccounts())
  }, [])
  // The client id and host decide whether Sign in can start: ask Go again after a change.
  useEffect(() => void loadAccounts(), [github.host, github.clientId])
  return (
    <section>
      <h2>Accounts</h2>
      <p className="muted settings-note">
        Pull requests, reviews and CI talk to GitHub's API with the token of the account signed in here. Flint keeps the token in the {keyringName()},
        sends it only to the API of the host below, and never shows it.
      </p>
      <GitHubAccount />
      <h3>GitHub</h3>
      <Row label="Host" hint="github.com, or your GitHub Enterprise Server (ghe.example.com). Each host has its own account.">
        <TextField value={github.host} valid={(v) => validGitHubHost(v.trim().toLowerCase())} onCommit={(v) => set({ host: v.trim().toLowerCase() })} />
      </Row>
      <Row label="OAuth App client ID" hint="The app Sign in with GitHub uses. Empty: sign in with the GitHub CLI's token instead.">
        <TextField value={github.clientId} valid={(v) => validClientId(v.trim())} onCommit={(v) => set({ clientId: v.trim() })} />
      </Row>
      <ScopesRow scopes={github.scopes} onChange={(scopes) => set({ scopes })} />
      <Row label="Wait for GitHub's retry-after up to" hint="When GitHub asks Flint to slow down, a read waits this long and tries once more; longer waits are reported. 0 never waits.">
        <NumberField value={github.maxRetryWaitSeconds} min={GITHUB_RETRY_WAIT.min} max={GITHUB_RETRY_WAIT.max} step={1} suffix="s" onCommit={(v) => set({ maxRetryWaitSeconds: Math.round(v) })} />
      </Row>
      <div className="settings-actions">
        <button
          disabled={github.host === DEFAULT_GITHUB.host && github.clientId === '' && github.scopes.join() === DEFAULT_GITHUB.scopes.join() && github.maxRetryWaitSeconds === DEFAULT_GITHUB.maxRetryWaitSeconds}
          onClick={() => update({ github: { ...DEFAULT_GITHUB, scopes: [...DEFAULT_GITHUB.scopes] } })}
        >
          Reset to Defaults
        </button>
      </div>
      <h3>Registering an OAuth App</h3>
      <p className="muted settings-note">
        Sign in with GitHub uses GitHub's device flow, which needs an OAuth App but no secret and no server. On GitHub (or your Enterprise
        server) open Settings ▸ Developer settings ▸ OAuth Apps ▸ New OAuth App, give it any homepage and callback URL (the device flow doesn't use
        them), create it, tick <b>Enable Device Flow</b>, and paste its client ID above. An organization that restricts OAuth Apps must approve it.{' '}
        <button className="link" onClick={() => openInBrowser(OAUTH_APPS_DOCS)}>GitHub's guide</button>
      </p>
    </section>
  )
}

function GitHubAccount() {
  const provider = useProvider()
  if (!provider) return null
  const account = provider.account
  if (!account) {
    return (
      <div className="account-card">
        {provider.envToken ? (
          <span className="muted">
            Using the token in <code>{provider.envToken}</code> from Flint's environment for {provider.host} (read as it is, never stored). Signing in replaces it.
          </span>
        ) : (
          <span className="muted">Not signed in to {provider.host}.</span>
        )}
        <SignInWithGitHub />
      </div>
    )
  }
  return (
    <div className="account-card">
      <div className="account-head">
        {account.avatarUrl && <img className="account-avatar" src={account.avatarUrl} alt="" referrerPolicy="no-referrer" />}
        <div className="account-who">
          <b>{account.login}</b>
          {account.name && <span className="muted">{account.name}</span>}
          <span className="muted">
            {account.host} · {account.source === 'cli' ? "the GitHub CLI's token" : 'signed in with GitHub'}
          </span>
        </div>
        <span className="spacer" />
        <button onClick={() => void signOut()}>Sign Out</button>
      </div>
      <span className="muted">Scopes: {account.scopes.length ? account.scopes.join(', ') : 'none reported (a fine-grained or app token)'}</span>
      <span className="muted">
        Sign Out deletes the token from the {keyringName()}. Revoking it at GitHub needs the OAuth App's secret, which Flint doesn't have: do it on{' '}
        <button className="link" onClick={openAuthorizedApps}>GitHub's authorized apps page</button>.
      </span>
    </div>
  )
}

function ScopesRow({ scopes, onChange }: { scopes: string[]; onChange: (scopes: string[]) => void }) {
  const saved = scopes.join(', ')
  const [draft, setDraft] = useState(saved)
  useEffect(() => setDraft(saved), [saved])
  const { scopes: parsed, invalid } = parseScopes(draft)
  const commit = () => {
    if (invalid.length === 0 && parsed.join(', ') !== saved) onChange(parsed)
    else if (invalid.length === 0) setDraft(saved)
  }
  return (
    <Row label="Scopes" hint="Asked for when signing in: repo (pull requests, reviews, checks, re-runs), read:org (organization repositories), workflow.">
      <div className="settings-control settings-control-stack">
        <input
          className={`settings-text${invalid.length ? ' invalid' : ''}`}
          value={draft}
          spellCheck={false}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => e.key === 'Enter' && commit()}
        />
        {invalid.length > 0 && <span className="error">Not a scope: {invalid.join(', ')}</span>}
      </div>
    </Row>
  )
}
