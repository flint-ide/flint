import { Alert, Button, IconButton } from '@flint-ide/ui'
import { X } from '@flint-ide/ui/icons'
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
    <Alert variant="warning" size="small" className="trust-banner" role="status">
      <span className="trust-text">
        <strong>Restricted Mode</strong> in {displayPath(roots, root)}: Flint doesn't run this folder's code on its own — no automatic terminal,
        no Rust or Python language servers, and agents ignore the folder's hooks and settings. Trust it only if you trust its authors.
      </span>
      <Button variant="primary" size="sm" onClick={() => void trustFolder(root)}>Trust Folder</Button>
      <IconButton
        variant="ghost"
        size="sm"
        aria-label="Hide for now (the folder stays restricted)"
        title="Hide for now (the folder stays restricted)"
        onClick={() => dismissTrustBanner(root)}
      >
        <X />
      </IconButton>
    </Alert>
  )
}
