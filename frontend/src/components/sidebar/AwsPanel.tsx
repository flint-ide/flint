import { useEffect, useMemo, useState } from 'react'
import { ClipboardSetText } from '../../wails/runtime'
import { cancelRefresh, checkIdentity, dismissRefresh, loginProfile, openProfileTerminal, refreshAws, refreshProfile, setAwsQuery, useAws } from '../../store/aws'
import { openContextMenu } from '../../store/contextMenu'
import { showToast } from '../../store/dialog'
import { kindLabel, profileStatus, shellCommand, type AwsProfile } from '../../lib/aws'
import type { DropdownItem } from '../ui/Dropdown'

/** While shown, the list is re-read this often (new logins) and expiry counts down. */
const REFRESH_MS = 30_000

/** AWS CLI profiles with the state of their credentials; terminals and logins per profile. */
export function AwsPanel() {
  const { profiles, loaded, loading, error, query, identities, refreshes } = useAws()
  const [now, setNow] = useState(Date.now)

  useEffect(() => {
    void refreshAws()
    const tick = setInterval(() => {
      setNow(Date.now())
      void refreshAws()
    }, REFRESH_MS)
    // Back from the browser after `aws sso login`.
    const onFocus = () => {
      setNow(Date.now())
      void refreshAws()
    }
    window.addEventListener('focus', onFocus)
    return () => {
      clearInterval(tick)
      window.removeEventListener('focus', onFocus)
    }
  }, [])

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? profiles.filter((p) => `${p.name} ${p.account ?? ''} ${p.role ?? ''} ${p.region ?? ''}`.toLowerCase().includes(q)) : profiles
  }, [profiles, query])

  const menu = (p: AwsProfile): DropdownItem[] => [
    { label: 'Open Terminal', hint: `AWS_PROFILE=${p.name}`, onSelect: () => openProfileTerminal(p) },
    ...(p.refreshable ? [{ label: 'Refresh Credentials', disabled: refreshes[p.name] === 'running', onSelect: () => void refreshProfile(p) }] : []),
    ...(p.login?.length ? [{ label: 'Log In', hint: shellCommand(p.login), onSelect: () => loginProfile(p) }] : []),
    { label: 'Check Identity', hint: 'sts get-caller-identity', onSelect: () => void checkIdentity(p.name) },
    { type: 'separator' },
    { label: 'Copy Profile Name', onSelect: () => void ClipboardSetText(p.name).then((ok) => ok && showToast(`Copied ${p.name}`)) },
    { label: 'Copy export AWS_PROFILE', onSelect: () => void ClipboardSetText(`export AWS_PROFILE=${p.name}`).then((ok) => ok && showToast('Copied')) },
  ]

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">AWS</span>
        <button className="icon" title="Refresh" disabled={loading} onClick={() => (setNow(Date.now()), void refreshAws())}>↻</button>
      </div>
      <div className="search-form">
        <div className="search-input">
          <input value={query} placeholder="Filter profiles" spellCheck={false} onChange={(e) => setAwsQuery(e.target.value)} />
        </div>
      </div>
      <div className="tree scroll">
        {error && <div className="placeholder error">{error}</div>}
        {shown.map((p) => {
          const status = profileStatus(p, now)
          const id = identities[p.name]
          const refresh = refreshes[p.name]
          const running = refresh === 'running'
          return (
            <div key={p.name} className="aws-profile">
              <div
                className="tree-row static aws-row"
                title={[p.name, kindLabel(p.kind), p.account && `account ${p.account}`, p.role && `role ${p.role}`, p.region && `region ${p.region}`, status.title].filter(Boolean).join('\n')}
                onDoubleClick={() => openProfileTerminal(p)}
                onContextMenu={(e) => {
                  e.preventDefault()
                  openContextMenu(e.clientX, e.clientY, menu(p))
                }}
              >
                <span className="aws-icon">☁</span>
                <span className="file-name">{p.name}</span>
                <span className="muted path-hint">{[kindLabel(p.kind), p.account, p.region].filter(Boolean).join(' · ')}</span>
                <span className={`aws-status ${running ? 'warn' : status.tone}`}>{running ? (p.kind === 'sso' ? 'browser…' : 'refreshing…') : status.text}</span>
                <span className={`aws-actions${running ? ' busy' : ''}`}>
                  {running ? (
                    <button className="icon" title="Stop refreshing" onClick={(e) => (e.stopPropagation(), cancelRefresh(p.name))}>■</button>
                  ) : p.refreshable ? (
                    <button className="icon" title="Refresh credentials (no terminal)" onClick={(e) => (e.stopPropagation(), void refreshProfile(p))}>↻</button>
                  ) : null}
                  {p.login?.length ? (
                    <button className="icon" title={`Log in: ${shellCommand(p.login)}`} onClick={(e) => (e.stopPropagation(), loginProfile(p))}>⇥</button>
                  ) : null}
                  <button className="icon" title={`Terminal as ${p.name}`} onClick={(e) => (e.stopPropagation(), openProfileTerminal(p))}>❯_</button>
                </span>
              </div>
              {refresh && refresh !== 'running' && (
                <div className={`aws-identity${refresh.ok ? '' : ' error'}`}>
                  {refresh.ok ? refresh.message : refresh.error}
                  {!refresh.ok && refresh.needsInput && p.login?.length ? (
                    <button className="link" onClick={() => (dismissRefresh(p.name), loginProfile(p))}>Log In</button>
                  ) : null}
                  {!refresh.ok && <button className="link" title="Dismiss" onClick={() => dismissRefresh(p.name)}>✕</button>}
                </div>
              )}
              {running && p.kind === 'sso' && <div className="aws-identity">Approve the login in your browser…</div>}
              {id && (
                <div className={`aws-identity${typeof id === 'object' && 'error' in id ? ' error' : ''}`}>
                  {id === 'checking' ? 'Checking…' : 'error' in id ? id.error : `${id.account} · ${id.arn}`}
                </div>
              )}
            </div>
          )
        })}
        {loaded && !error && profiles.length === 0 && <div className="placeholder muted">No profiles in ~/.aws/config or ~/.aws/credentials.</div>}
      </div>
    </div>
  )
}
