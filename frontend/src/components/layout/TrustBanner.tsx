import { useTrust, trustFolder, dismissTrustBanner } from '../../store/trust'
import { useWorkspace } from '../../store/workspace'
import { displayPath } from '../../lib/workspaceRoots'

/**
 * Restricted Mode notice for open folders Flint hasn't been told to trust:
 * until then nothing runs the folder's code on its own.
 */
export function TrustBanner() {
  const roots = useWorkspace((s) => s.roots)
  const { trusted, dismissed } = useTrust()
  const restricted = roots.filter((r) => trusted[r] === false && !dismissed.has(r))
  if (restricted.length === 0) return null
  const root = restricted[0]
  return (
    <div className="trust-banner" role="status">
      <span className="trust-icon">⚠</span>
      <span className="trust-text">
        <strong>Restricted Mode</strong> in {displayPath(roots, root)}: Flint doesn't run this folder's code on its own — no automatic terminal,
        no Rust or Python language servers, and agents ignore the project's hooks and settings. Trust it only if you trust its authors.
      </span>
      <button className="primary" onClick={() => void trustFolder(root)}>Trust Folder</button>
      <button className="icon" title="Hide for now (the folder stays restricted)" onClick={() => dismissTrustBanner(root)}>✕</button>
    </div>
  )
}
